package controller

// Event reason constants emitted on ImpVM objects.
const (
	EventReasonScheduled            = "Scheduled"
	EventReasonUnschedulable        = "Unschedulable"
	EventReasonNodeLost             = "NodeLost"
	EventReasonRescheduling         = "Rescheduling"
	EventReasonTerminating          = "Terminating"
	EventReasonTerminationTimeout   = "TerminationTimeout"
	EventReasonHealthCheckFailed    = "HealthCheckFailed"
	EventReasonHealthCheckRecovered = "HealthCheckRecovered"
)

// Condition type constants.
const (
	ConditionScheduled   = "Scheduled"
	ConditionReady       = "Ready"
	ConditionNodeHealthy = "NodeHealthy"
)
