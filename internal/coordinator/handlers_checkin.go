package coordinator

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/ambient/platform/components/boss/internal/coordinator/db"
	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
)

// CheckInConfigRequest is the JSON request body for creating/updating check-in config.
type CheckInConfigRequest struct {
	CheckInEnabled       bool     `json:"check_in_enabled"`
	CronSchedule         string   `json:"cron_schedule"`
	IdleOnly             bool     `json:"idle_only"`
	TimeoutSeconds       int      `json:"timeout_seconds,omitempty"`
	RetryAttempts        int      `json:"retry_attempts,omitempty"`
	RetryDelaySeconds    int      `json:"retry_delay_seconds,omitempty"`
	NotificationChannels []string `json:"notification_channels,omitempty"`
}

// CheckInConfigResponse is the JSON response for check-in configuration.
type CheckInConfigResponse struct {
	AgentName            string    `json:"agent_name"`
	SpaceName            string    `json:"space_name"`
	CheckInEnabled       bool      `json:"check_in_enabled"`
	CronSchedule         string    `json:"cron_schedule"`
	IdleOnly             bool      `json:"idle_only"`
	TimeoutSeconds       int       `json:"timeout_seconds"`
	RetryAttempts        int       `json:"retry_attempts"`
	RetryDelaySeconds    int       `json:"retry_delay_seconds"`
	NotificationChannels []string  `json:"notification_channels"`
	LastCheckInAt        *time.Time `json:"last_check_in_at,omitempty"`
	EnabledBy            string    `json:"enabled_by,omitempty"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

// CheckInEventResponse is the JSON response for check-in events.
type CheckInEventResponse struct {
	ID                 string     `json:"id"`
	AgentName          string     `json:"agent_name"`
	SpaceName          string     `json:"space_name"`
	ScheduledAt        time.Time  `json:"scheduled_at"`
	TriggeredAt        time.Time  `json:"triggered_at"`
	AgentStatus        string     `json:"agent_status"`
	MessageSent        bool       `json:"message_sent"`
	MessageID          string     `json:"message_id,omitempty"`
	ResponseReceived   bool       `json:"response_received"`
	ResponseAt         *time.Time `json:"response_at,omitempty"`
	ResponseLatencyMs  *int64     `json:"response_latency_ms,omitempty"`
	StatusAfterCheckIn string     `json:"status_after_check_in,omitempty"`
	ErrorMessage       string     `json:"error_message,omitempty"`
	RetryCount         int        `json:"retry_count"`
}

// handleAgentCheckInConfig handles GET/POST/PATCH/DELETE /spaces/{space}/agent/{agent}/check-in/config
func (s *Server) handleAgentCheckInConfig(w http.ResponseWriter, r *http.Request, spaceName, agentName string) {
	switch r.Method {
	case http.MethodGet:
		s.getAgentCheckInConfig(w, r, spaceName, agentName)
	case http.MethodPost:
		s.createAgentCheckInConfig(w, r, spaceName, agentName)
	case http.MethodPatch:
		s.updateAgentCheckInConfig(w, r, spaceName, agentName)
	case http.MethodDelete:
		s.deleteAgentCheckInConfig(w, r, spaceName, agentName)
	default:
		writeJSONError(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// getAgentCheckInConfig retrieves the check-in configuration for an agent.
func (s *Server) getAgentCheckInConfig(w http.ResponseWriter, r *http.Request, spaceName, agentName string) {
	cfg, err := s.repo.GetCheckInConfig(spaceName, agentName)
	if err != nil {
		writeJSONError(w, fmt.Sprintf("database error: %v", err), http.StatusInternalServerError)
		return
	}
	if cfg == nil {
		writeJSONError(w, "check-in configuration not found", http.StatusNotFound)
		return
	}

	resp := configToResponse(cfg)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// createAgentCheckInConfig creates a new check-in configuration for an agent.
func (s *Server) createAgentCheckInConfig(w http.ResponseWriter, r *http.Request, spaceName, agentName string) {
	var req CheckInConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, fmt.Sprintf("invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	// Validate cron schedule
	if req.CronSchedule != "" {
		parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
		if _, err := parser.Parse(req.CronSchedule); err != nil {
			writeJSONError(w, fmt.Sprintf("invalid cron schedule: %v", err), http.StatusBadRequest)
			return
		}
	}

	// Validate timeout/retry values
	if req.TimeoutSeconds < 0 || req.RetryAttempts < 0 || req.RetryDelaySeconds < 0 {
		writeJSONError(w, "timeout, retry_attempts, and retry_delay_seconds must be non-negative", http.StatusBadRequest)
		return
	}

	// Set defaults
	if req.TimeoutSeconds == 0 {
		req.TimeoutSeconds = 300
	}
	if req.RetryAttempts == 0 {
		req.RetryAttempts = 3
	}
	if req.RetryDelaySeconds == 0 {
		req.RetryDelaySeconds = 60
	}

	// Get caller name for audit
	callerName := r.Header.Get("X-Agent-Name")
	if callerName == "" {
		callerName = "unknown"
	}

	// Marshal notification channels to JSON
	notifJSON, _ := json.Marshal(req.NotificationChannels)

	cfg := &db.AgentCheckInConfig{
		SpaceName:            spaceName,
		AgentName:            agentName,
		CheckInEnabled:       req.CheckInEnabled,
		CronSchedule:         req.CronSchedule,
		IdleOnly:             req.IdleOnly,
		TimeoutSeconds:       req.TimeoutSeconds,
		RetryAttempts:        req.RetryAttempts,
		RetryDelaySeconds:    req.RetryDelaySeconds,
		NotificationChannels: string(notifJSON),
		EnabledBy:            callerName,
		CreatedAt:            time.Now().UTC(),
		UpdatedAt:            time.Now().UTC(),
	}

	if err := s.repo.UpsertCheckInConfig(cfg); err != nil {
		writeJSONError(w, fmt.Sprintf("database error: %v", err), http.StatusInternalServerError)
		return
	}

	// Reload scheduler to pick up new config
	if s.checkInScheduler != nil {
		s.checkInScheduler.ReloadSchedules()
	}

	resp := configToResponse(cfg)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(resp)
}

// updateAgentCheckInConfig updates an existing check-in configuration.
func (s *Server) updateAgentCheckInConfig(w http.ResponseWriter, r *http.Request, spaceName, agentName string) {
	// Fetch existing config
	cfg, err := s.repo.GetCheckInConfig(spaceName, agentName)
	if err != nil {
		writeJSONError(w, fmt.Sprintf("database error: %v", err), http.StatusInternalServerError)
		return
	}
	if cfg == nil {
		writeJSONError(w, "check-in configuration not found", http.StatusNotFound)
		return
	}

	var req CheckInConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, fmt.Sprintf("invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	// Validate cron schedule if provided
	if req.CronSchedule != "" {
		parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
		if _, err := parser.Parse(req.CronSchedule); err != nil {
			writeJSONError(w, fmt.Sprintf("invalid cron schedule: %v", err), http.StatusBadRequest)
			return
		}
		cfg.CronSchedule = req.CronSchedule
	}

	// Update fields (only non-zero values)
	cfg.CheckInEnabled = req.CheckInEnabled
	if req.TimeoutSeconds > 0 {
		cfg.TimeoutSeconds = req.TimeoutSeconds
	}
	if req.RetryAttempts >= 0 {
		cfg.RetryAttempts = req.RetryAttempts
	}
	if req.RetryDelaySeconds > 0 {
		cfg.RetryDelaySeconds = req.RetryDelaySeconds
	}
	if req.NotificationChannels != nil {
		notifJSON, _ := json.Marshal(req.NotificationChannels)
		cfg.NotificationChannels = string(notifJSON)
	}
	cfg.IdleOnly = req.IdleOnly
	cfg.UpdatedAt = time.Now().UTC()

	if err := s.repo.UpsertCheckInConfig(cfg); err != nil {
		writeJSONError(w, fmt.Sprintf("database error: %v", err), http.StatusInternalServerError)
		return
	}

	// Reload scheduler to pick up updated config
	if s.checkInScheduler != nil {
		s.checkInScheduler.ReloadSchedules()
	}

	resp := configToResponse(cfg)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// deleteAgentCheckInConfig disables check-ins for an agent.
func (s *Server) deleteAgentCheckInConfig(w http.ResponseWriter, r *http.Request, spaceName, agentName string) {
	if err := s.repo.DeleteCheckInConfig(spaceName, agentName); err != nil {
		writeJSONError(w, fmt.Sprintf("database error: %v", err), http.StatusInternalServerError)
		return
	}

	// Reload scheduler to remove deleted config
	if s.checkInScheduler != nil {
		s.checkInScheduler.ReloadSchedules()
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleCheckInConfigsList handles GET /spaces/{space}/check-ins/configs
func (s *Server) handleCheckInConfigsList(w http.ResponseWriter, r *http.Request, spaceName string) {
	enabledOnly := r.URL.Query().Get("enabled") == "true"

	configs, err := s.repo.ListCheckInConfigs(enabledOnly)
	if err != nil {
		writeJSONError(w, fmt.Sprintf("database error: %v", err), http.StatusInternalServerError)
		return
	}

	// Filter by space
	var filtered []*db.AgentCheckInConfig
	for _, cfg := range configs {
		if cfg.SpaceName == spaceName {
			filtered = append(filtered, cfg)
		}
	}

	responses := make([]CheckInConfigResponse, 0, len(filtered))
	for _, cfg := range filtered {
		responses = append(responses, configToResponse(cfg))
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(responses)
}

// handleAgentCheckInHistory handles GET /spaces/{space}/agent/{agent}/check-in/history
func (s *Server) handleAgentCheckInHistory(w http.ResponseWriter, r *http.Request, spaceName, agentName string) {
	limitStr := r.URL.Query().Get("limit")
	limit := 50 // default
	if limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	events, err := s.repo.ListCheckInEvents(spaceName, agentName, limit)
	if err != nil {
		writeJSONError(w, fmt.Sprintf("database error: %v", err), http.StatusInternalServerError)
		return
	}

	responses := make([]CheckInEventResponse, 0, len(events))
	for _, evt := range events {
		responses = append(responses, eventToResponse(evt))
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(responses)
}

// configToResponse converts a DB model to an API response.
func configToResponse(cfg *db.AgentCheckInConfig) CheckInConfigResponse {
	var channels []string
	if cfg.NotificationChannels != "" {
		json.Unmarshal([]byte(cfg.NotificationChannels), &channels)
	}

	var lastCheckIn *time.Time
	if cfg.LastCheckInAt.Valid {
		lastCheckIn = &cfg.LastCheckInAt.Time
	}

	return CheckInConfigResponse{
		AgentName:            cfg.AgentName,
		SpaceName:            cfg.SpaceName,
		CheckInEnabled:       cfg.CheckInEnabled,
		CronSchedule:         cfg.CronSchedule,
		IdleOnly:             cfg.IdleOnly,
		TimeoutSeconds:       cfg.TimeoutSeconds,
		RetryAttempts:        cfg.RetryAttempts,
		RetryDelaySeconds:    cfg.RetryDelaySeconds,
		NotificationChannels: channels,
		LastCheckInAt:        lastCheckIn,
		EnabledBy:            cfg.EnabledBy,
		CreatedAt:            cfg.CreatedAt,
		UpdatedAt:            cfg.UpdatedAt,
	}
}

// eventToResponse converts a DB event model to an API response.
func eventToResponse(evt *db.CheckInEvent) CheckInEventResponse {
	var responseAt *time.Time
	if evt.ResponseAt.Valid {
		responseAt = &evt.ResponseAt.Time
	}

	var latency *int64
	if evt.ResponseLatencyMs.Valid {
		latency = &evt.ResponseLatencyMs.Int64
	}

	return CheckInEventResponse{
		ID:                 evt.ID,
		AgentName:          evt.AgentName,
		SpaceName:          evt.SpaceName,
		ScheduledAt:        evt.ScheduledAt,
		TriggeredAt:        evt.TriggeredAt,
		AgentStatus:        evt.AgentStatus,
		MessageSent:        evt.MessageSent,
		MessageID:          evt.MessageID,
		ResponseReceived:   evt.ResponseReceived,
		ResponseAt:         responseAt,
		ResponseLatencyMs:  latency,
		StatusAfterCheckIn: evt.StatusAfterCheckIn,
		ErrorMessage:       evt.ErrorMessage,
		RetryCount:         evt.RetryCount,
	}
}

// CreateCheckInEventForAgent creates a check-in event and sends a message to the agent.
// This is called by the scheduler.
func (s *Server) CreateCheckInEventForAgent(spaceName, agentName string) error {
	// Get agent status
	agent, err := s.repo.GetAgent(spaceName, agentName)
	if err != nil {
		return fmt.Errorf("get agent: %w", err)
	}
	if agent == nil {
		return fmt.Errorf("agent not found")
	}

	// Create event
	event := &db.CheckInEvent{
		ID:           uuid.New().String(),
		SpaceName:    spaceName,
		AgentName:    agentName,
		ScheduledAt:  time.Now().UTC(),
		TriggeredAt:  time.Now().UTC(),
		AgentStatus:  agent.Status,
		MessageSent:  false,
		ResponseReceived: false,
	}

	if err := s.repo.CreateCheckInEvent(event); err != nil {
		return fmt.Errorf("create event: %w", err)
	}

	// Send check-in message via the existing message system
	// TODO: Integrate with odis-mcp send_message when scheduler is implemented

	return nil
}
