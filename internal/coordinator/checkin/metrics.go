package checkin

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics holds Prometheus metrics for the check-in system.
type Metrics struct {
	// Scheduler metrics
	CheckInsTriggered prometheus.Counter
	CheckInsFailed    prometheus.Counter
	CheckInsSucceeded prometheus.Counter
	CheckInsTimedOut  prometheus.Counter
	ResponseLatency   prometheus.Histogram

	// Retry metrics
	CheckInRetries prometheus.Counter
	MaxRetriesExceeded prometheus.Counter

	// Leader election metrics
	LeaderElections prometheus.Counter
	LeadershipDuration prometheus.Gauge
	LockRenewals prometheus.Counter
	LockFailures prometheus.Counter

	// Configuration metrics
	ActiveConfigs prometheus.Gauge
	ConfigChanges prometheus.Counter

	// Agent metrics
	IdleOnlySkipped prometheus.Counter
	DuplicateSkipped prometheus.Counter
	MessageDeliveryFailures prometheus.Counter
}

// NewMetrics creates and registers Prometheus metrics for check-ins.
func NewMetrics() *Metrics {
	return &Metrics{
		CheckInsTriggered: promauto.NewCounter(prometheus.CounterOpts{
			Name: "checkin_triggers_total",
			Help: "Total number of check-ins triggered",
		}),
		CheckInsFailed: promauto.NewCounter(prometheus.CounterOpts{
			Name: "checkin_failures_total",
			Help: "Total number of check-ins that failed (max retries exceeded)",
		}),
		CheckInsSucceeded: promauto.NewCounter(prometheus.CounterOpts{
			Name: "checkin_successes_total",
			Help: "Total number of successful check-ins (agent responded)",
		}),
		CheckInsTimedOut: promauto.NewCounter(prometheus.CounterOpts{
			Name: "checkin_timeouts_total",
			Help: "Total number of check-ins that timed out",
		}),
		ResponseLatency: promauto.NewHistogram(prometheus.HistogramOpts{
			Name: "checkin_response_latency_seconds",
			Help: "Check-in response latency in seconds",
			Buckets: []float64{1, 5, 10, 30, 60, 120, 300, 600}, // 1s to 10min
		}),
		CheckInRetries: promauto.NewCounter(prometheus.CounterOpts{
			Name: "checkin_retries_total",
			Help: "Total number of check-in retry attempts",
		}),
		MaxRetriesExceeded: promauto.NewCounter(prometheus.CounterOpts{
			Name: "checkin_max_retries_exceeded_total",
			Help: "Total number of check-ins that exceeded max retry attempts",
		}),
		LeaderElections: promauto.NewCounter(prometheus.CounterOpts{
			Name: "checkin_leader_elections_total",
			Help: "Total number of leader elections (instance became leader)",
		}),
		LeadershipDuration: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "checkin_leadership_seconds",
			Help: "Duration in seconds since this instance became leader (0 if not leader)",
		}),
		LockRenewals: promauto.NewCounter(prometheus.CounterOpts{
			Name: "checkin_lock_renewals_total",
			Help: "Total number of successful leader lock renewals",
		}),
		LockFailures: promauto.NewCounter(prometheus.CounterOpts{
			Name: "checkin_lock_failures_total",
			Help: "Total number of leader lock renewal failures",
		}),
		ActiveConfigs: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "checkin_active_configs",
			Help: "Number of currently active check-in configurations",
		}),
		ConfigChanges: promauto.NewCounter(prometheus.CounterOpts{
			Name: "checkin_config_changes_total",
			Help: "Total number of check-in configuration changes (create/update/delete)",
		}),
		IdleOnlySkipped: promauto.NewCounter(prometheus.CounterOpts{
			Name: "checkin_idle_only_skipped_total",
			Help: "Total number of check-ins skipped because agent was not idle",
		}),
		DuplicateSkipped: promauto.NewCounter(prometheus.CounterOpts{
			Name: "checkin_duplicate_skipped_total",
			Help: "Total number of check-ins skipped due to pending check-in exists",
		}),
		MessageDeliveryFailures: promauto.NewCounter(prometheus.CounterOpts{
			Name: "checkin_message_delivery_failures_total",
			Help: "Total number of check-in message delivery failures",
		}),
	}
}
