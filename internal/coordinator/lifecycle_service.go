package coordinator

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// This file contains the core service functions for agent lifecycle management:
// spawnAgentService, restartAgentService, and stopAgentService.
// These were extracted from lifecycle.go to reduce file size.

// spawnAgentService contains the core business logic for spawning a new agent session.
func (s *Server) spawnAgentService(spaceName, agentName string, req spawnRequest, spawnerName string) (sessionID, backendName, canonical string, retErr error) {
	// Serialize concurrent spawn requests for the same agent to eliminate the
	// TOCTOU race between SessionExists() and CreateSession(). A sync.Map entry
	// is held for the duration of this call; a second concurrent request for the
	// same agent receives an immediate 409 Conflict rather than a silent race.
	spawnKey := strings.ToLower(spaceName + "/" + agentName)
	if _, loaded := s.spawnInProgress.LoadOrStore(spawnKey, struct{}{}); loaded {
		return "", "", "", &lifecycleErr{
			StatusCode: http.StatusConflict,
			Msg:        fmt.Sprintf("spawn for agent %q is already in progress", agentName),
		}
	}
	defer s.spawnInProgress.Delete(spawnKey)

	// Apply AgentConfig defaults. The command is intentionally NOT read from
	// req.Command — callers cannot specify an arbitrary command to execute.
	// The only valid command sources are: stored AgentConfig.Command (set by
	// admins via the config API) and the server-side allowSkipPermissions toggle.
	var spawnCommand string
	var spawnWorkDir string
	var spawnRepos []SessionRepo
	var spawnInitialPrompt string
	var spawnPersonas []PersonaRef
	if existingKS, hasKS := s.getSpace(spaceName); hasKS {
		s.mu.RLock()
		cfgCanonical := resolveAgentName(existingKS, agentName)
		if cfg := existingKS.agentConfig(cfgCanonical); cfg != nil {
			if req.Backend == "" && cfg.Backend != "" {
				req.Backend = cfg.Backend
			}
			if cfg.Command != "" {
				spawnCommand = cfg.Command
			}
			spawnWorkDir = cfg.WorkDir
			spawnRepos = cfg.Repos
			spawnInitialPrompt = cfg.InitialPrompt
			spawnPersonas = cfg.Personas
		}
		// Inherit WorkDir from spawner if the child has no WorkDir configured.
		if spawnWorkDir == "" && spawnerName != "" {
			spawnerCanonical := resolveAgentName(existingKS, spawnerName)
			if spawnerCfg := existingKS.agentConfig(spawnerCanonical); spawnerCfg != nil {
				spawnWorkDir = spawnerCfg.WorkDir
			}
		}
		s.mu.RUnlock()
	}
	_ = spawnPersonas // personas are embedded in buildIgnitionText

	backend, err := s.backendByName(req.Backend)
	if err != nil {
		return "", "", "", &lifecycleErr{StatusCode: http.StatusBadRequest, Msg: err.Error()}
	}
	sessionName := req.SessionID
	if sessionName == "" {
		sessionName = tmuxDefaultSession(spaceName, agentName)
	}

	// If the agent already exists with a non-session registration, reject the spawn.
	if existingKS, ok := s.getSpace(spaceName); ok {
		s.mu.RLock()
		can := resolveAgentName(existingKS, agentName)
		existingAgent := existingKS.agentStatus(can)
		s.mu.RUnlock()
		if isNonSessionAgent(existingAgent) {
			return "", "", "", &lifecycleErr{
				StatusCode: http.StatusUnprocessableEntity, JSONBody: true,
				Msg: fmt.Sprintf("lifecycle management via session backend is not available for agent_type %q; manage your agent process externally", existingAgent.Registration.AgentType),
			}
		}
	}

	// For tmux, check if session already exists. Ambient generates its own IDs.
	if backend.Name() == "tmux" && backend.SessionExists(sessionName) {
		return "", "", "", &lifecycleErr{StatusCode: http.StatusConflict, Msg: fmt.Sprintf("session %q already exists", sessionName)}
	}

	ctx := context.Background()
	if backend.Name() == "tmux" && s.allowSkipPermissions && spawnCommand == "" {
		spawnCommand = "claude --dangerously-skip-permissions"
	}
	var createOpts SessionCreateOpts
	if backend.Name() == "ambient" {
		createOpts = SessionCreateOpts{
			SessionID: sessionName,
			Command:   spawnCommand,
			BackendOpts: AmbientCreateOpts{
				DisplayName: agentName,
				Repos:       spawnRepos,
				SpaceName:   spaceName,
				EnvVars: func() map[string]string {
					if s.apiToken == "" {
						return nil
					}
					return map[string]string{"ODIS_API_TOKEN": s.apiToken}
				}(),
			},
		}
	} else {
		createOpts = SessionCreateOpts{
			SessionID: sessionName,
			Command:   spawnCommand,
			BackendOpts: TmuxCreateOpts{
				Width:                req.Width,
				Height:               req.Height,
				WorkDir:              spawnWorkDir,
				MCPServerURL:         s.localURL(),
				MCPServerName:        s.mcpServerName(),
				AgentToken:           s.generateAgentToken(spaceName, agentName),
				AllowSkipPermissions: s.allowSkipPermissions,
			},
		}
	}

	sessionID, retErr = backend.CreateSession(ctx, createOpts)
	if retErr != nil {
		return "", "", "", &lifecycleErr{StatusCode: http.StatusInternalServerError, Msg: fmt.Sprintf("create session: %v", retErr)}
	}
	if sessionID == "" {
		return "", "", "", &lifecycleErr{StatusCode: http.StatusInternalServerError, Msg: fmt.Sprintf("backend returned empty session ID for agent %s", agentName)}
	}

	// Register session on the agent record.
	ks := s.getOrCreateSpace(spaceName)
	s.mu.Lock()
	canonical = resolveAgentName(ks, agentName)
	agent := ks.agentStatus(canonical)
	if agent == nil {
		agent = &AgentUpdate{
			Status:    StatusIdle,
			Summary:   fmt.Sprintf("%s: spawned", agentName),
			UpdatedAt: time.Now().UTC(),
		}
		ks.setAgentStatus(canonical, agent)
	}
	agent.SessionID = sessionID
	agent.BackendType = backend.Name()

	// Set Parent from spawner identity, if not already set.
	if spawnerName != "" && !strings.EqualFold(spawnerName, agentName) && agent.Parent == "" {
		agent.Parent = resolveAgentName(ks, spawnerName)
		rebuildChildren(ks)
	}

	if saveErr := s.saveSpace(ks); saveErr != nil {
		s.mu.Unlock()
		s.emit(DomainEvent{Level: LevelError, EventType: EventServerError, Space: spaceName, Agent: agentName,
			Msg: fmt.Sprintf("spawn: save failed: %v", saveErr)})
	} else {
		s.mu.Unlock()
	}

	backendName = backend.Name()
	s.emit(DomainEvent{Level: LevelInfo, EventType: EventAgentSpawned, Space: spaceName, Agent: agentName,
		Msg:    fmt.Sprintf("spawned in session \"%s\" (backend: %s)", sessionID, backendName),
		Fields: map[string]string{"session_id": sessionID, "backend": backendName}})
	spawnedPayload, _ := json.Marshal(map[string]string{"space": spaceName, "agent": agentName})
	s.broadcastSSE(spaceName, agentName, "agent_spawned", string(spawnedPayload))

	initialMsg := req.InitialMessage
	cfgInitialPrompt := spawnInitialPrompt
	spawnerIdentity := spawnerName
	if spawnerIdentity == "" {
		spawnerIdentity = "boss"
	}

	if req.TaskID != "" {
		caller := spawnerName
		if caller == "" {
			caller = "boss"
		}
		s.assignTaskToAgent(spaceName, req.TaskID, canonical, caller)
	}

	go func() {
		if ab, ok := backend.(*AmbientSessionBackend); ok {
			pollCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			if err := ab.waitForRunning(pollCtx, sessionID, 60*time.Second); err != nil {
				s.logEvent(fmt.Sprintf("[%s/%s] spawn: session did not reach running state: %v", spaceName, agentName, err))
				return
			}
		} else {
			// Poll for Claude Code's idle prompt instead of a fixed sleep.
			// A 5-second sleep is unreliable: startup time varies with MCP
			// registration and first-run config. Text sent before the prompt
			// appears goes to the shell and is silently dropped.
			if err := waitForIdle(sessionID, 60*time.Second); err != nil {
				s.emit(DomainEvent{Level: LevelWarn, EventType: EventAgentSpawned, Space: spaceName, Agent: agentName,
					Msg: fmt.Sprintf("spawn: timed out waiting for idle before ignite: %v — sending anyway", err)})
			}
		}
		s.mu.RLock()
		ignitePrompt := s.buildIgnitionText(spaceName, agentName, sessionID)
		s.mu.RUnlock()
		if err := backend.SendInput(sessionID, ignitePrompt); err != nil {
			s.emit(DomainEvent{Level: LevelWarn, EventType: EventAgentSpawned, Space: spaceName, Agent: agentName,
				Msg: fmt.Sprintf("spawn: ignite send failed: %v (fetch manually: curl %s/spaces/%s/ignition/%s)", err, s.localURL(), spaceName, agentName)})
		}
		if initialMsg != "" {
			s.deliverInternalMessage(spaceName, agentName, spawnerIdentity, initialMsg)
		}
		if cfgInitialPrompt != "" {
			s.deliverInternalMessage(spaceName, agentName, "boss", cfgInitialPrompt)
		}
	}()

	return sessionID, backendName, canonical, nil
}

// stopAgentService contains the core business logic for stopping an agent session.
func (s *Server) stopAgentService(spaceName, agentName string) (canonical string, retErr error) {
	ks, ok := s.getSpace(spaceName)
	if !ok {
		return "", &lifecycleErr{StatusCode: http.StatusNotFound, Msg: fmt.Sprintf("space %q not found", spaceName)}
	}

	s.mu.RLock()
	canonical = resolveAgentName(ks, agentName)
	agent, exists := ks.agentStatusOk(canonical)
	var sessionName string
	if exists {
		sessionName = agent.SessionID
	}
	s.mu.RUnlock()

	if !exists {
		return "", &lifecycleErr{StatusCode: http.StatusNotFound, Msg: fmt.Sprintf("agent %q not found", agentName)}
	}
	if isNonSessionAgent(agent) {
		return "", &lifecycleErr{StatusCode: http.StatusUnprocessableEntity, JSONBody: true,
			Msg: fmt.Sprintf("lifecycle management via session backend is not available for agent_type %q; manage your agent process externally", agent.Registration.AgentType)}
	}
	if sessionName == "" {
		return "", &lifecycleErr{StatusCode: http.StatusBadRequest, Msg: fmt.Sprintf("agent %q has no registered session", canonical)}
	}

	backend := s.backendFor(agent)
	if !backend.SessionExists(sessionName) {
		return "", &lifecycleErr{StatusCode: http.StatusNotFound, Msg: fmt.Sprintf("session %q not found", sessionName)}
	}

	ctx, cancel := context.WithTimeout(context.Background(), tmuxCmdTimeout)
	defer cancel()
	if err := backend.KillSession(ctx, sessionName); err != nil {
		return "", &lifecycleErr{StatusCode: http.StatusInternalServerError, Msg: fmt.Sprintf("kill session: %v", err)}
	}

	s.mu.Lock()
	agent.Status = StatusDone
	agent.Summary = fmt.Sprintf("%s: stopped", canonical)
	agent.SessionID = ""
	agent.UpdatedAt = time.Now().UTC()
	s.saveSpace(ks)
	s.mu.Unlock()

	s.emit(DomainEvent{Level: LevelInfo, EventType: EventAgentStopped, Space: spaceName, Agent: canonical,
		Msg:    fmt.Sprintf("stopped (session %q killed)", sessionName),
		Fields: map[string]string{"session_id": sessionName}})
	s.broadcastSSE(spaceName, canonical, "agent_stopped", canonical)

	return canonical, nil
}

// restartAgentService contains the core business logic for restarting an agent.
func (s *Server) restartAgentService(spaceName, agentName string, req spawnRequest) (sessionID, canonical string, retErr error) {
	ks, ok := s.getSpace(spaceName)
	if !ok {
		return "", "", &lifecycleErr{StatusCode: http.StatusNotFound, Msg: fmt.Sprintf("space %q not found", spaceName)}
	}

	s.mu.RLock()
	canonical = resolveAgentName(ks, agentName)
	agent, exists := ks.agentStatusOk(canonical)
	var oldSession string
	if exists {
		oldSession = agent.SessionID
	}
	// Load AgentConfig to restore cwd, command, model, and initial_prompt on restart.
	var restartWorkDir string
	var restartInitialPrompt string
	var restartCommand string
	var restartModel string
	if cfg := ks.agentConfig(canonical); cfg != nil {
		restartWorkDir = cfg.WorkDir
		restartInitialPrompt = cfg.InitialPrompt
		restartCommand = cfg.Command
		restartModel = cfg.Model
	}
	s.mu.RUnlock()

	command := restartCommand
	if command == "" {
		if s.allowSkipPermissions {
			command = "claude --dangerously-skip-permissions"
		} else {
			command = "claude"
		}
	}

	if !exists {
		return "", "", &lifecycleErr{StatusCode: http.StatusNotFound, Msg: fmt.Sprintf("agent %q not found", agentName)}
	}
	if isNonSessionAgent(agent) {
		return "", "", &lifecycleErr{StatusCode: http.StatusUnprocessableEntity, JSONBody: true,
			Msg: fmt.Sprintf("lifecycle management via session backend is not available for agent_type %q; manage your agent process externally", agent.Registration.AgentType)}
	}
	if oldSession == "" {
		return "", "", &lifecycleErr{StatusCode: http.StatusBadRequest, Msg: fmt.Sprintf("agent %q has no registered session", canonical)}
	}

	backend := s.backendFor(agent)

	// Stop the existing session.
	if backend.SessionExists(oldSession) {
		ctx, cancel := context.WithTimeout(context.Background(), tmuxCmdTimeout)
		if err := backend.KillSession(ctx, oldSession); err != nil {
			cancel()
			return "", "", &lifecycleErr{StatusCode: http.StatusInternalServerError, Msg: fmt.Sprintf("kill existing session: %v", err)}
		}
		cancel()
		s.emit(DomainEvent{Level: LevelInfo, EventType: EventAgentRestarted, Space: spaceName, Agent: canonical,
			Msg: fmt.Sprintf("restart: killed old session %q", oldSession)})
		time.Sleep(1 * time.Second)
	}

	// Clear the session reference so spawn can proceed.
	s.mu.Lock()
	agent.SessionID = ""
	s.mu.Unlock()

	// Create new session.
	var createOpts SessionCreateOpts
	if backend.Name() == "ambient" {
		createOpts = SessionCreateOpts{
			Command: command,
			BackendOpts: AmbientCreateOpts{
				DisplayName: canonical,
				SpaceName:   spaceName,
				Model:       restartModel,
				EnvVars: func() map[string]string {
					if s.apiToken == "" {
						return nil
					}
					return map[string]string{"ODIS_API_TOKEN": s.apiToken}
				}(),
			},
		}
	} else {
		newSession := tmuxDefaultSession(spaceName, canonical)
		if backend.SessionExists(newSession) {
			newSession = newSession + "-new"
		}
		createOpts = SessionCreateOpts{
			SessionID: newSession,
			Command:   command,
			BackendOpts: TmuxCreateOpts{
				// Width/Height intentionally omitted — session_backend_tmux.go applies
				// the same 220×50 defaults as the spawn path when these are zero.
				WorkDir:              restartWorkDir,
				MCPServerURL:         s.localURL(),
				MCPServerName:        s.mcpServerName(),
				AgentToken:           s.generateAgentToken(spaceName, canonical),
				AllowSkipPermissions: s.allowSkipPermissions,
				Model:                restartModel,
			},
		}
	}

	ctx2 := context.Background()
	sessionID, retErr = backend.CreateSession(ctx2, createOpts)
	if retErr != nil {
		return "", "", &lifecycleErr{StatusCode: http.StatusInternalServerError, Msg: fmt.Sprintf("create new session: %v", retErr)}
	}

	s.mu.Lock()
	agent.SessionID = sessionID
	agent.Status = StatusIdle
	agent.Summary = fmt.Sprintf("%s: restarted", canonical)
	agent.UpdatedAt = time.Now().UTC()
	// Re-pin persona versions so the agent gets the latest prompts.
	if cfg := ks.agentConfig(canonical); cfg != nil && len(cfg.Personas) > 0 {
		cfg.Personas = s.resolvePersonaRefs(cfg.Personas)
	}
	s.saveSpace(ks)
	s.mu.Unlock()

	s.emit(DomainEvent{Level: LevelInfo, EventType: EventAgentRestarted, Space: spaceName, Agent: canonical,
		Msg:    fmt.Sprintf("restarted in new session %q", sessionID),
		Fields: map[string]string{"session_id": sessionID}})
	s.broadcastSSE(spaceName, canonical, "agent_restarted", canonical)

	// Handle task assignment if provided in spawn request
	if req.TaskID != "" {
		s.assignTaskToAgent(spaceName, req.TaskID, canonical, "boss")
	}

	go func() {
		if ab, ok := backend.(*AmbientSessionBackend); ok {
			pollCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			if err := ab.waitForRunning(pollCtx, sessionID, 60*time.Second); err != nil {
				s.logEvent(fmt.Sprintf("[%s/%s] restart: session did not reach running state: %v", spaceName, canonical, err))
				return
			}
		} else {
			if err := waitForIdle(sessionID, 60*time.Second); err != nil {
				s.emit(DomainEvent{Level: LevelWarn, EventType: EventAgentRestarted, Space: spaceName, Agent: canonical,
					Msg: fmt.Sprintf("restart: timed out waiting for idle before ignite: %v — sending anyway", err)})
			}
		}
		s.mu.RLock()
		igniteText := s.buildIgnitionText(spaceName, canonical, sessionID)
		s.mu.RUnlock()
		if err := backend.SendInput(sessionID, igniteText); err != nil {
			s.emit(DomainEvent{Level: LevelWarn, EventType: EventAgentRestarted, Space: spaceName, Agent: canonical,
				Msg: fmt.Sprintf("restart: ignite send failed: %v", err)})
		}
		// Deliver initial message if provided in spawn request (e.g., for auto-resume scenarios)
		if req.InitialMessage != "" {
			s.deliverInternalMessage(spaceName, canonical, "boss", req.InitialMessage)
		}
		// Also deliver configured initial prompt if set
		if restartInitialPrompt != "" {
			s.deliverInternalMessage(spaceName, canonical, "boss", restartInitialPrompt)
		}
	}()

	return sessionID, canonical, nil
}

// maybeAutoResumeAgent checks if a session should be auto-resumed and restarts it if needed.
// Returns the (possibly new) sessionID, whether a restart occurred, and any error.
// Auto-resume only applies to backends that support it (checked via SupportsAutoResume()).
func (s *Server) maybeAutoResumeAgent(spaceName, canonical, sessionID string, backend SessionBackend) (string, bool, error) {
	// Only auto-resume if the backend supports it
	if !backend.SupportsAutoResume() {
		return sessionID, false, nil
	}

	// Check if session exists
	if backend.SessionExists(sessionID) {
		return sessionID, false, nil
	}

	// Session is missing and backend supports auto-resume — restart it
	s.logEvent(fmt.Sprintf("[%s/%s] auto-resume: session %s not found, attempting restart", spaceName, canonical, sessionID))

	newSessionID, _, err := s.restartAgentService(spaceName, canonical, spawnRequest{})
	if err != nil {
		// Don't return the stale sessionID on error; the session doesn't exist.
		// Caller should handle the error and skip work rather than proceeding with invalid sessionID.
		return "", false, fmt.Errorf("auto-resume failed: %w", err)
	}

	s.logEvent(fmt.Sprintf("[%s/%s] auto-resume: restarted in session %s", spaceName, canonical, newSessionID))
	return newSessionID, true, nil
}
