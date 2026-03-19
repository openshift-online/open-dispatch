package coordinator

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"strings"
)

// --- ACP backend API response types (K8s CR shape) ---

type backendSessionCR struct {
	Metadata struct {
		Name             string            `json:"name"`
		Labels           map[string]string `json:"labels,omitempty"`
		CreationTimestamp string            `json:"creationTimestamp,omitempty"`
	} `json:"metadata"`
	Spec struct {
		DisplayName string `json:"displayName,omitempty"`
	} `json:"spec"`
	Status struct {
		Phase string `json:"phase,omitempty"`
	} `json:"status"`
}

func (cr *backendSessionCR) displayName() string {
	if cr.Spec.DisplayName != "" {
		return cr.Spec.DisplayName
	}
	return cr.Metadata.Name
}

func (cr *backendSessionCR) phase() string {
	return strings.ToLower(cr.Status.Phase)
}

type ambientExportMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ambientExportResponse struct {
	LegacyMessages []ambientExportMessage `json:"legacyMessages"`
	AguiEvents     []json.RawMessage      `json:"aguiEvents"`
}

type aguiEvent struct {
	Type     string                `json:"type"`
	Messages []ambientExportMessage `json:"messages,omitempty"`
}

type backendSessionList struct {
	Items []backendSessionCR `json:"items"`
}

// --- Helpers ---

func (b *AmbientSessionBackend) sessionsPath() string {
	return "/api/projects/" + b.project + "/agentic-sessions"
}

func (b *AmbientSessionBackend) sessionPath(sessionID string) string {
	return b.sessionsPath() + "/" + sessionID
}

func generateMsgID() string {
	buf := make([]byte, 16)
	rand.Read(buf)
	return fmt.Sprintf("%x", buf)
}



// sanitizeLabelValue converts an arbitrary string into a valid Kubernetes label
// value. Spaces are replaced with '-'; any character that is not alphanumeric,
// '-', '_', or '.' is dropped. Leading/trailing non-alphanumeric characters are
// trimmed, and the result is truncated to 63 characters.
func sanitizeLabelValue(s string) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	for _, c := range s {
		switch {
		case (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.':
			b.WriteRune(c)
		case c == ' ':
			b.WriteByte('-')
		}
	}
	v := strings.Trim(b.String(), "-_.")
	if len(v) > 63 {
		v = strings.TrimRight(v[:63], "-_.")
	}
	return v
}

// lastMessagesSnapshot finds the last MESSAGES_SNAPSHOT event in aguiEvents
// and returns its messages. Returns nil if none found.
func lastMessagesSnapshot(events []json.RawMessage) []ambientExportMessage {
	var lastMsgs []ambientExportMessage
	for _, raw := range events {
		var ev aguiEvent
		if err := json.Unmarshal(raw, &ev); err != nil {
			continue
		}
		if ev.Type == "MESSAGES_SNAPSHOT" && len(ev.Messages) > 0 {
			lastMsgs = ev.Messages
		}
	}
	return lastMsgs
}
