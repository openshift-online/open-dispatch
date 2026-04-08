package checkin

import (
	"fmt"
	"log"
	"time"

	"github.com/ambient/platform/components/boss/internal/coordinator/db"
)

// ResponseTracker monitors agent status updates and correlates them with pending check-ins.
type ResponseTracker struct {
	repo    *db.Repository
	metrics *Metrics
}

// NewResponseTracker creates a new response tracker instance.
func NewResponseTracker(repo *db.Repository, metrics *Metrics) *ResponseTracker {
	return &ResponseTracker{
		repo:    repo,
		metrics: metrics,
	}
}

// CheckPendingEvents checks for pending check-in events and validates responses.
// Should be called periodically (e.g., every 30 seconds).
func (rt *ResponseTracker) CheckPendingEvents() error {
	// Get all pending check-ins from the last 10 minutes
	events, err := rt.repo.GetPendingCheckInEvents(10)
	if err != nil {
		return fmt.Errorf("get pending events: %w", err)
	}

	now := time.Now().UTC()

	for _, event := range events {
		// Get current agent status
		agent, err := rt.repo.GetAgent(event.SpaceName, event.AgentName)
		if err != nil {
			log.Printf("[response tracker] error getting agent %s/%s: %v",
				event.SpaceName, event.AgentName, err)
			continue
		}
		if agent == nil {
			log.Printf("[response tracker] agent %s/%s not found for event %s",
				event.SpaceName, event.AgentName, event.ID)
			continue
		}

		// Check if agent has updated their status since the check-in was triggered
		// Agent's UpdatedAt timestamp should be after the check-in was triggered
		if agent.UpdatedAt.After(event.TriggeredAt) {
			// Agent has posted a status update - consider this a response
			event.ResponseReceived = true
			event.ResponseAt.Time = agent.UpdatedAt
			event.ResponseAt.Valid = true

			// Calculate response latency
			latencyMs := agent.UpdatedAt.Sub(event.TriggeredAt).Milliseconds()
			event.ResponseLatencyMs.Int64 = latencyMs
			event.ResponseLatencyMs.Valid = true

			event.StatusAfterCheckIn = agent.Status

			if err := rt.repo.UpdateCheckInEvent(event); err != nil {
				log.Printf("[response tracker] error updating event %s: %v", event.ID, err)
				continue
			}

			log.Printf("[response tracker] check-in response received for %s/%s (latency: %dms)",
				event.SpaceName, event.AgentName, latencyMs)

			// Track metrics
			rt.metrics.CheckInsSucceeded.Inc()
			rt.metrics.ResponseLatency.Observe(float64(latencyMs) / 1000.0) // convert to seconds

			// Update the config's last_check_in_at timestamp
			if err := rt.updateLastCheckIn(event.SpaceName, event.AgentName); err != nil {
				log.Printf("[response tracker] error updating last check-in time: %v", err)
			}
		} else {
			// Check if timeout has been exceeded
			cfg, err := rt.repo.GetCheckInConfig(event.SpaceName, event.AgentName)
			if err != nil {
				log.Printf("[response tracker] error getting config for %s/%s: %v",
					event.SpaceName, event.AgentName, err)
				continue
			}
			if cfg == nil {
				continue
			}

			timeoutDuration := time.Duration(cfg.TimeoutSeconds) * time.Second
			if now.Sub(event.TriggeredAt) > timeoutDuration {
				// Timeout exceeded - check if we should retry
				if event.RetryCount < cfg.RetryAttempts {
					// Calculate retry delay with exponential backoff: 1x, 2x, 4x
					retryDelay := time.Duration(cfg.RetryDelaySeconds) * time.Second
					backoffMultiplier := 1 << event.RetryCount // 2^retryCount
					retryDelay = retryDelay * time.Duration(backoffMultiplier)

					scheduledFor := now.Add(retryDelay)

					log.Printf("[response tracker] check-in timeout for %s/%s, scheduling retry %d/%d (delay: %s, scheduled for: %s)",
						event.SpaceName, event.AgentName, event.RetryCount+1, cfg.RetryAttempts, retryDelay, scheduledFor.Format(time.RFC3339))

					// Mark original event as timed out
					event.ErrorMessage = fmt.Sprintf("timeout after %s, retry scheduled", timeoutDuration)
					event.ResponseReceived = false

					if err := rt.repo.UpdateCheckInEvent(event); err != nil {
						log.Printf("[response tracker] error updating event %s: %v", event.ID, err)
					}

					// Create a new check-in event for the retry
					retryEvent := &db.CheckInEvent{
						ID:               fmt.Sprintf("%s-retry-%d", event.ID, event.RetryCount+1),
						SpaceName:        event.SpaceName,
						AgentName:        event.AgentName,
						ScheduledAt:      scheduledFor,
						TriggeredAt:      time.Time{}, // Will be set when actually triggered
						AgentStatus:      event.AgentStatus,
						MessageSent:      false,
						ResponseReceived: false,
						RetryCount:       event.RetryCount + 1,
					}

					if err := rt.repo.CreateCheckInEvent(retryEvent); err != nil {
						log.Printf("[response tracker] error creating retry event: %v", err)
						rt.metrics.MessageDeliveryFailures.Inc()
					} else {
						rt.metrics.CheckInRetries.Inc()
						log.Printf("[response tracker] retry event created: %s (scheduled for %s)",
							retryEvent.ID, scheduledFor.Format(time.RFC3339))
					}
				} else {
					// Max retries exceeded - mark as failed
					event.ResponseReceived = false
					event.ErrorMessage = fmt.Sprintf("max retries (%d) exceeded, no response after %s",
						cfg.RetryAttempts, timeoutDuration)

					// Track metrics
					rt.metrics.MaxRetriesExceeded.Inc()
					rt.metrics.CheckInsFailed.Inc()

					if err := rt.repo.UpdateCheckInEvent(event); err != nil {
						log.Printf("[response tracker] error updating event %s: %v", event.ID, err)
					}

					log.Printf("[response tracker] check-in failed for %s/%s: max retries exceeded",
						event.SpaceName, event.AgentName)
				}
			}
		}
	}

	return nil
}

// updateLastCheckIn updates the last_check_in_at timestamp for a config.
func (rt *ResponseTracker) updateLastCheckIn(spaceName, agentName string) error {
	cfg, err := rt.repo.GetCheckInConfig(spaceName, agentName)
	if err != nil {
		return err
	}
	if cfg == nil {
		return fmt.Errorf("config not found")
	}

	cfg.LastCheckInAt.Time = time.Now().UTC()
	cfg.LastCheckInAt.Valid = true

	return rt.repo.UpsertCheckInConfig(cfg)
}

// ValidateResponse checks if an agent's status update counts as a valid check-in response.
func (rt *ResponseTracker) ValidateResponse(spaceName, agentName string, updateTime time.Time) (bool, error) {
	// Get pending check-ins for this agent
	events, err := rt.repo.GetPendingCheckInEvents(10)
	if err != nil {
		return false, fmt.Errorf("get pending events: %w", err)
	}

	for _, event := range events {
		if event.SpaceName == spaceName && event.AgentName == agentName {
			// Check if this update is after the check-in was triggered
			if updateTime.After(event.TriggeredAt) {
				return true, nil
			}
		}
	}

	return false, nil
}
