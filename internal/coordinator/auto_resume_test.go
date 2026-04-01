package coordinator

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

// mockAmbientBackend is a test backend that simulates an Ambient session lifecycle
// with support for session state transitions (missing → created).
type mockAmbientBackend struct {
	mu            sync.Mutex
	sessions      map[string]bool // sessionID -> exists
	restartCalled bool
	createCount   int
}

func newMockAmbientBackend() *mockAmbientBackend {
	return &mockAmbientBackend{
		sessions: make(map[string]bool),
	}
}

func (b *mockAmbientBackend) Name() string { return "ambient" }
func (b *mockAmbientBackend) Available() bool { return true }
func (b *mockAmbientBackend) SupportsAutoResume() bool { return true }

func (b *mockAmbientBackend) CreateSession(_ context.Context, opts SessionCreateOpts) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.createCount++
	sessionID := fmt.Sprintf("mock-ambient-session-%d", b.createCount)
	b.sessions[sessionID] = true
	return sessionID, nil
}

func (b *mockAmbientBackend) KillSession(_ context.Context, sessionID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.sessions, sessionID)
	return nil
}

func (b *mockAmbientBackend) SessionExists(sessionID string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.sessions[sessionID]
}

func (b *mockAmbientBackend) ListSessions() ([]string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	var list []string
	for sid := range b.sessions {
		list = append(list, sid)
	}
	return list, nil
}

func (b *mockAmbientBackend) GetStatus(_ context.Context, sessionID string) (SessionStatus, error) {
	if b.SessionExists(sessionID) {
		return SessionStatusRunning, nil
	}
	return SessionStatusMissing, nil
}

func (b *mockAmbientBackend) IsIdle(_ string) bool { return true }
func (b *mockAmbientBackend) CaptureOutput(_ string, _ int) ([]string, error) { return nil, nil }
func (b *mockAmbientBackend) CheckApproval(_ string) ApprovalInfo { return ApprovalInfo{} }
func (b *mockAmbientBackend) SendInput(_ string, _ string) error { return nil }
func (b *mockAmbientBackend) Approve(_ string) error { return nil }
func (b *mockAmbientBackend) AlwaysAllow(_ string) error { return nil }
func (b *mockAmbientBackend) Interrupt(_ context.Context, _ string) error { return nil }
func (b *mockAmbientBackend) DiscoverSessions() (map[string]string, error) { return nil, nil }

// TestAutoResumeAmbientSession verifies that when an Ambient session is stopped
// (SessionExists returns false), the restartAgentService call correctly creates a new
// session. This test directly exercises restartAgentService rather than the full
// SingleAgentCheckIn flow to avoid test timeouts.
func TestAutoResumeAmbientSession(t *testing.T) {
	srv, cleanup := mustStartServer(t)
	defer cleanup()

	space := "TestAutoResume"
	agentName := "test-agent"

	// Install mock ambient backend
	mockBackend := newMockAmbientBackend()
	srv.backends = map[string]SessionBackend{"ambient": mockBackend}
	srv.defaultBackend = "ambient"

	// Create an agent with an initial session
	initialSessionID := "initial-session"
	mockBackend.mu.Lock()
	mockBackend.sessions[initialSessionID] = true
	mockBackend.mu.Unlock()

	srv.mu.Lock()
	ks := srv.getOrCreateSpaceLocked(space)
	ks.setAgentStatus(agentName, &AgentUpdate{
		Status:      StatusIdle,
		Summary:     agentName + ": ready",
		SessionID:   initialSessionID,
		BackendType: "ambient",
	})
	if _, ok := ks.Agents[agentName]; !ok {
		ks.Agents[agentName] = &AgentRecord{}
	}
	ks.Agents[agentName].Config = &AgentConfig{
		WorkDir: "/workspace",
	}
	srv.mu.Unlock()

	// Simulate the session being stopped (e.g., due to inactivity timeout)
	mockBackend.mu.Lock()
	delete(mockBackend.sessions, initialSessionID)
	mockBackend.mu.Unlock()

	// Verify the session is gone
	if mockBackend.SessionExists(initialSessionID) {
		t.Fatal("expected initial session to be stopped")
	}

	// Test auto-resume through the production code path
	newSessionID, resumed, err := srv.maybeAutoResumeAgent(space, agentName, initialSessionID, mockBackend)
	if err != nil {
		t.Fatalf("maybeAutoResumeAgent failed: %v", err)
	}
	if !resumed {
		t.Fatal("expected auto-resume to occur")
	}

	// Verify a new session was created
	if mockBackend.createCount != 1 {
		t.Errorf("expected 1 session creation, got %d", mockBackend.createCount)
	}

	if newSessionID == initialSessionID {
		t.Error("expected new session ID after auto-resume")
	}
	if newSessionID == "" {
		t.Error("expected non-empty session ID after auto-resume")
	}

	// Verify the new session exists
	if !mockBackend.SessionExists(newSessionID) {
		t.Errorf("new session %q does not exist", newSessionID)
	}

	// Verify the agent status was updated with the new session
	srv.mu.RLock()
	agent, ok := ks.agentStatusOk(agentName)
	srv.mu.RUnlock()
	if !ok {
		t.Fatal("agent not found after auto-resume")
	}
	if agent.SessionID != newSessionID {
		t.Errorf("agent session ID = %q, want %q", agent.SessionID, newSessionID)
	}
	if agent.BackendType != "ambient" {
		t.Errorf("agent backend type = %q, want %q", agent.BackendType, "ambient")
	}
}

// TestAutoResumeOnlyForAmbient verifies that auto-resume currently only applies to
// backends that report SupportsAutoResume() == true (currently Ambient), not tmux sessions
// (which should skip). This behavior is enforced via the backend capability interface.
func TestAutoResumeOnlyForAmbient(t *testing.T) {
	srv, cleanup := mustStartServer(t)
	defer cleanup()

	space := "TestTmuxNoResume"
	agentName := "tmux-agent"

	// Install mock tmux backend
	mockBackend := newSpawnCapturingBackend()
	srv.backends = map[string]SessionBackend{"tmux": mockBackend}
	srv.defaultBackend = "tmux"

	// Create an agent with a tmux session that doesn't exist
	srv.mu.Lock()
	ks := srv.getOrCreateSpaceLocked(space)
	ks.setAgentStatus(agentName, &AgentUpdate{
		Status:      StatusIdle,
		Summary:     agentName + ": ready",
		SessionID:   "missing-tmux-session",
		BackendType: "tmux",
	})
	srv.mu.Unlock()

	// Call SingleAgentCheckIn — should skip, not auto-resume
	result := srv.SingleAgentCheckIn(space, agentName, "", "")

	// Verify it was skipped
	if len(result.Skipped) != 1 {
		t.Errorf("expected 1 skipped, got %d: %v", len(result.Skipped), result.Skipped)
	}

	// Verify no session was created
	select {
	case <-mockBackend.captured:
		t.Error("expected no session creation for tmux backend")
	default:
		// Expected: no session created
	}
}

// TestSingleAgentCheckInNonexistentAgent verifies that calling SingleAgentCheckIn
// on a nonexistent agent returns an appropriate error.
func TestSingleAgentCheckInNonexistentAgent(t *testing.T) {
	srv, cleanup := mustStartServer(t)
	defer cleanup()

	space := "TestNonexistent"

	// Call SingleAgentCheckIn on non-existent agent
	result := srv.SingleAgentCheckIn(space, "nonexistent", "", "")

	// Should get an error
	if len(result.Errors) != 1 {
		t.Errorf("expected 1 error for nonexistent agent, got %d: %v", len(result.Errors), result.Errors)
	}
}
