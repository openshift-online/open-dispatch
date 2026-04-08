// Package checkin implements the agent check-in scheduler and coordinator.
package checkin

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/ambient/platform/components/boss/internal/coordinator/db"
	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
)

const (
	// Leader election lock duration (30 seconds as per spec)
	lockDuration = 30 * time.Second
	// Lock renewal interval (15 seconds - renew at half the lock duration)
	renewInterval = 15 * time.Second
	// Failover detection window (40 seconds max as per spec)
	pollInterval = 10 * time.Second
)

// Scheduler manages check-in schedules and coordinates execution across instances.
type Scheduler struct {
	repo       *db.Repository
	cron       *cron.Cron
	instanceID string
	isLeader   bool
	mu         sync.RWMutex
	ctx        context.Context
	cancel     context.CancelFunc

	// Message sender function injected for testing
	sendMessage func(spaceName, agentName, message string) error

	// Metrics for observability
	metrics       *Metrics
	leaderSince   time.Time
}

// New creates a new check-in scheduler instance.
func New(repo *db.Repository) *Scheduler {
	ctx, cancel := context.WithCancel(context.Background())
	return &Scheduler{
		repo:       repo,
		cron:       cron.New(cron.WithSeconds()),
		instanceID: uuid.New().String(),
		ctx:        ctx,
		cancel:     cancel,
		metrics:    NewMetrics(),
	}
}

// SetMessageSender injects the message delivery function.
func (s *Scheduler) SetMessageSender(fn func(spaceName, agentName, message string) error) {
	s.sendMessage = fn
}

// Start begins the scheduler's leader election and job execution loop.
func (s *Scheduler) Start() error {
	log.Printf("[check-in scheduler] starting instance %s", s.instanceID)

	// Start leader election loop
	go s.leaderElectionLoop()

	return nil
}

// Stop gracefully shuts down the scheduler.
func (s *Scheduler) Stop() error {
	log.Printf("[check-in scheduler] stopping instance %s", s.instanceID)
	s.cancel()

	// Release leader lock if held
	if s.isLeader {
		if err := s.repo.ReleaseSchedulerLock(s.instanceID); err != nil {
			log.Printf("[check-in scheduler] error releasing lock: %v", err)
		}
	}

	// Stop cron scheduler
	ctx := s.cron.Stop()
	<-ctx.Done()

	return nil
}

// leaderElectionLoop continuously tries to acquire/maintain leader status.
func (s *Scheduler) leaderElectionLoop() {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.tryBecomeLeader()
		}
	}
}

// tryBecomeLeader attempts to acquire the leader lock.
func (s *Scheduler) tryBecomeLeader() {
	acquired, err := s.repo.AcquireSchedulerLock(s.instanceID, lockDuration)
	if err != nil {
		log.Printf("[check-in scheduler] error acquiring lock: %v", err)
		return
	}

	s.mu.Lock()
	wasLeader := s.isLeader
	s.isLeader = acquired
	s.mu.Unlock()

	if acquired && !wasLeader {
		log.Printf("[check-in scheduler] instance %s became leader", s.instanceID)
		s.leaderSince = time.Now()
		s.metrics.LeaderElections.Inc()
		go s.leaderLoop()
	}
}

// leaderLoop runs while this instance is the leader.
func (s *Scheduler) leaderLoop() {
	// Load all enabled check-in configs and schedule them
	if err := s.reloadSchedules(); err != nil {
		log.Printf("[check-in scheduler] error loading schedules: %v", err)
	}

	// Start cron scheduler
	s.cron.Start()

	// Start retry queue processor
	retryTicker := time.NewTicker(30 * time.Second)
	defer retryTicker.Stop()

	// Lock renewal loop
	renewTicker := time.NewTicker(renewInterval)
	defer renewTicker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-retryTicker.C:
			// Process retry queue every 30 seconds
			if err := s.processRetryQueue(); err != nil {
				log.Printf("[check-in scheduler] error processing retry queue: %v", err)
			}
		case <-renewTicker.C:
			if err := s.repo.RenewSchedulerLock(s.instanceID, lockDuration); err != nil {
				log.Printf("[check-in scheduler] lost leader lock: %v", err)
				s.metrics.LockFailures.Inc()
				s.metrics.LeadershipDuration.Set(0)
				s.mu.Lock()
				s.isLeader = false
				s.mu.Unlock()
				s.cron.Stop()
				return
			}
			s.metrics.LockRenewals.Inc()
			s.metrics.LeadershipDuration.Set(time.Since(s.leaderSince).Seconds())
		}
	}
}

// reloadSchedules loads all enabled check-in configs and schedules cron jobs.
func (s *Scheduler) reloadSchedules() error {
	configs, err := s.repo.ListCheckInConfigs(true)
	if err != nil {
		return fmt.Errorf("list enabled configs: %w", err)
	}

	// Clear existing entries
	for _, entry := range s.cron.Entries() {
		s.cron.Remove(entry.ID)
	}

	// Add new entries
	for _, cfg := range configs {
		if err := s.addSchedule(cfg); err != nil {
			log.Printf("[check-in scheduler] error adding schedule for %s/%s: %v",
				cfg.SpaceName, cfg.AgentName, err)
			continue
		}
	}

	log.Printf("[check-in scheduler] loaded %d check-in schedules", len(configs))
	s.metrics.ActiveConfigs.Set(float64(len(configs)))
	return nil
}

// addSchedule adds a cron job for a check-in configuration.
func (s *Scheduler) addSchedule(cfg *db.AgentCheckInConfig) error {
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	schedule, err := parser.Parse(cfg.CronSchedule)
	if err != nil {
		return fmt.Errorf("parse cron schedule %q: %w", cfg.CronSchedule, err)
	}

	// Create job that triggers check-in
	job := func() {
		if err := s.triggerCheckIn(cfg); err != nil {
			log.Printf("[check-in scheduler] error triggering check-in for %s/%s: %v",
				cfg.SpaceName, cfg.AgentName, err)
		}
	}

	s.cron.Schedule(schedule, cron.FuncJob(job))
	return nil
}

// triggerCheckIn executes a check-in for an agent.
func (s *Scheduler) triggerCheckIn(cfg *db.AgentCheckInConfig) error {
	// Get current agent status
	agent, err := s.repo.GetAgent(cfg.SpaceName, cfg.AgentName)
	if err != nil {
		return fmt.Errorf("get agent: %w", err)
	}
	if agent == nil {
		return fmt.Errorf("agent not found")
	}

	// 3-check idle validation (if idle_only is enabled)
	if cfg.IdleOnly {
		// Check 1: Agent status must be "idle"
		if agent.Status != "idle" {
			log.Printf("[check-in scheduler] skipping %s/%s (status=%s, idle_only=true)",
				cfg.SpaceName, cfg.AgentName, agent.Status)
			s.metrics.IdleOnlySkipped.Inc()
			return nil
		}

		// Check 2: Agent must be idle for at least 5 minutes (300 seconds)
		idleDuration := time.Since(agent.UpdatedAt)
		if idleDuration < 300*time.Second {
			log.Printf("[check-in scheduler] skipping %s/%s (idle for %v, required 5m)",
				cfg.SpaceName, cfg.AgentName, idleDuration)
			s.metrics.IdleOnlySkipped.Inc()
			return nil
		}

		// Check 3: No pending check-ins within 10-minute window
		pending, err := s.repo.GetPendingCheckInEvents(10)
		if err != nil {
			log.Printf("[check-in scheduler] error checking pending events: %v", err)
			// Continue anyway - don't block on this check
		} else {
			for _, evt := range pending {
				if evt.AgentName == cfg.AgentName && evt.SpaceName == cfg.SpaceName {
					log.Printf("[check-in scheduler] skipping %s/%s (pending check-in exists)",
						cfg.SpaceName, cfg.AgentName)
					s.metrics.DuplicateSkipped.Inc()
					return nil
				}
			}
		}

		// 100ms debounce before message send
		time.Sleep(100 * time.Millisecond)

		// Re-validate all 3 checks after debounce
		agent, err = s.repo.GetAgent(cfg.SpaceName, cfg.AgentName)
		if err != nil {
			return fmt.Errorf("re-validate agent: %w", err)
		}
		if agent == nil {
			return fmt.Errorf("agent not found on re-validation")
		}

		// Re-check 1: Status still idle
		if agent.Status != "idle" {
			log.Printf("[check-in scheduler] skipping %s/%s (status changed to %s after debounce)",
				cfg.SpaceName, cfg.AgentName, agent.Status)
			s.metrics.IdleOnlySkipped.Inc()
			return nil
		}

		// Re-check 2: Still idle for at least 5 minutes
		idleDuration = time.Since(agent.UpdatedAt)
		if idleDuration < 300*time.Second {
			log.Printf("[check-in scheduler] skipping %s/%s (idle duration %v after debounce)",
				cfg.SpaceName, cfg.AgentName, idleDuration)
			s.metrics.IdleOnlySkipped.Inc()
			return nil
		}

		// Re-check 3: No new pending check-ins appeared
		pending, err = s.repo.GetPendingCheckInEvents(10)
		if err != nil {
			log.Printf("[check-in scheduler] error re-checking pending events: %v", err)
		} else {
			for _, evt := range pending {
				if evt.AgentName == cfg.AgentName && evt.SpaceName == cfg.SpaceName {
					log.Printf("[check-in scheduler] skipping %s/%s (pending check-in appeared after debounce)",
						cfg.SpaceName, cfg.AgentName)
					s.metrics.DuplicateSkipped.Inc()
					return nil
				}
			}
		}
	} else {
		// When idle_only is false, still check for duplicate pending check-ins
		pending, err := s.repo.GetPendingCheckInEvents(10)
		if err != nil {
			log.Printf("[check-in scheduler] error checking pending events: %v", err)
			// Continue anyway - don't block on this check
		} else {
			for _, evt := range pending {
				if evt.AgentName == cfg.AgentName && evt.SpaceName == cfg.SpaceName {
					log.Printf("[check-in scheduler] skipping %s/%s (pending check-in exists)",
						cfg.SpaceName, cfg.AgentName)
					s.metrics.DuplicateSkipped.Inc()
					return nil
				}
			}
		}
	}

	// Create check-in event
	now := time.Now().UTC()
	event := &db.CheckInEvent{
		ID:           uuid.New().String(),
		SpaceName:    cfg.SpaceName,
		AgentName:    cfg.AgentName,
		ScheduledAt:  now,
		TriggeredAt:  now,
		AgentStatus:  agent.Status,
		MessageSent:  false,
		ResponseReceived: false,
		RetryCount:   0,
	}

	if err := s.repo.CreateCheckInEvent(event); err != nil {
		return fmt.Errorf("create check-in event: %w", err)
	}

	// Send check-in message
	if s.sendMessage != nil {
		message := fmt.Sprintf("🔔 Scheduled check-in. Please confirm you're operational by posting a status update.")
		if err := s.sendMessage(cfg.SpaceName, cfg.AgentName, message); err != nil {
			log.Printf("[check-in scheduler] failed to send message to %s/%s: %v",
				cfg.SpaceName, cfg.AgentName, err)
			event.ErrorMessage = err.Error()
			event.MessageSent = false
			s.metrics.MessageDeliveryFailures.Inc()
		} else {
			event.MessageSent = true
			event.MessageID = uuid.New().String() // TODO: get actual message ID from send_message
		}
	} else {
		event.ErrorMessage = "message sender not configured"
		s.metrics.MessageDeliveryFailures.Inc()
	}

	// Update event with message status
	if err := s.repo.UpdateCheckInEvent(event); err != nil {
		log.Printf("[check-in scheduler] error updating event: %v", err)
	}

	log.Printf("[check-in scheduler] triggered check-in for %s/%s (event %s)",
		cfg.SpaceName, cfg.AgentName, event.ID)
	s.metrics.CheckInsTriggered.Inc()

	return nil
}

// IsLeader returns true if this instance is currently the leader.
func (s *Scheduler) IsLeader() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.isLeader
}

// ReloadSchedules triggers a reload of all check-in schedules.
// Called when a configuration is added/updated/deleted.
func (s *Scheduler) ReloadSchedules() error {
	if !s.IsLeader() {
		return nil // Only leader reloads schedules
	}
	s.metrics.ConfigChanges.Inc()
	return s.reloadSchedules()
}

// GetMetrics returns the metrics instance for sharing with response tracker.
func (s *Scheduler) GetMetrics() *Metrics {
	return s.metrics
}

// processRetryQueue checks for scheduled check-in events that are ready to trigger
// and sends their messages. This handles retry events created by the response tracker.
func (s *Scheduler) processRetryQueue() error {
	events, err := s.repo.GetScheduledCheckInEvents()
	if err != nil {
		return fmt.Errorf("get scheduled events: %w", err)
	}

	if len(events) == 0 {
		return nil
	}

	log.Printf("[check-in scheduler] processing %d scheduled retry events", len(events))

	for _, event := range events {
		// Get the agent's current status
		agent, err := s.repo.GetAgent(event.SpaceName, event.AgentName)
		if err != nil {
			log.Printf("[check-in scheduler] error getting agent %s/%s for retry: %v",
				event.SpaceName, event.AgentName, err)
			continue
		}
		if agent == nil {
			log.Printf("[check-in scheduler] agent %s/%s not found for retry event %s",
				event.SpaceName, event.AgentName, event.ID)
			continue
		}

		// Update event with current agent status and triggered time
		event.AgentStatus = agent.Status
		event.TriggeredAt = time.Now().UTC()

		// Send check-in message
		if s.sendMessage != nil {
			message := fmt.Sprintf("🔔 Scheduled check-in (retry %d). Please confirm you're operational by posting a status update.", event.RetryCount)
			if err := s.sendMessage(event.SpaceName, event.AgentName, message); err != nil {
				log.Printf("[check-in scheduler] failed to send retry message to %s/%s: %v",
					event.SpaceName, event.AgentName, err)
				event.ErrorMessage = err.Error()
				event.MessageSent = false
				s.metrics.MessageDeliveryFailures.Inc()
			} else {
				event.MessageSent = true
				event.MessageID = fmt.Sprintf("retry-%s", event.ID)
			}
		} else {
			event.ErrorMessage = "message sender not configured"
			s.metrics.MessageDeliveryFailures.Inc()
		}

		// Update event with message status
		if err := s.repo.UpdateCheckInEvent(event); err != nil {
			log.Printf("[check-in scheduler] error updating retry event %s: %v", event.ID, err)
		}

		log.Printf("[check-in scheduler] triggered retry check-in for %s/%s (event %s, retry %d)",
			event.SpaceName, event.AgentName, event.ID, event.RetryCount)
	}

	return nil
}
