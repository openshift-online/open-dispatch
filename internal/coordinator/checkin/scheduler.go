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

	// Lock renewal loop
	renewTicker := time.NewTicker(renewInterval)
	defer renewTicker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-renewTicker.C:
			if err := s.repo.RenewSchedulerLock(s.instanceID, lockDuration); err != nil {
				log.Printf("[check-in scheduler] lost leader lock: %v", err)
				s.mu.Lock()
				s.isLeader = false
				s.mu.Unlock()
				s.cron.Stop()
				return
			}
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

	// Check if agent is idle (if idle_only is enabled)
	if cfg.IdleOnly && agent.Status != "idle" {
		log.Printf("[check-in scheduler] skipping %s/%s (status=%s, idle_only=true)",
			cfg.SpaceName, cfg.AgentName, agent.Status)
		return nil
	}

	// Check for pending check-ins (avoid duplicates within 10min window)
	pending, err := s.repo.GetPendingCheckInEvents(10)
	if err != nil {
		log.Printf("[check-in scheduler] error checking pending events: %v", err)
		// Continue anyway - don't block on this check
	} else {
		for _, evt := range pending {
			if evt.AgentName == cfg.AgentName && evt.SpaceName == cfg.SpaceName {
				log.Printf("[check-in scheduler] skipping %s/%s (pending check-in exists)",
					cfg.SpaceName, cfg.AgentName)
				return nil
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
		} else {
			event.MessageSent = true
			event.MessageID = uuid.New().String() // TODO: get actual message ID from send_message
		}
	} else {
		event.ErrorMessage = "message sender not configured"
	}

	// Update event with message status
	if err := s.repo.UpdateCheckInEvent(event); err != nil {
		log.Printf("[check-in scheduler] error updating event: %v", err)
	}

	log.Printf("[check-in scheduler] triggered check-in for %s/%s (event %s)",
		cfg.SpaceName, cfg.AgentName, event.ID)

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
	return s.reloadSchedules()
}
