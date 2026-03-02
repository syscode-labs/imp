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
	EventReasonCNIDetected          = "CNIDetected"
	EventReasonCNIAmbiguous         = "CNIAmbiguous"
	EventReasonBridgeReady          = "BridgeReady"
	EventReasonIPAllocated          = "IPAllocated"
	EventReasonNATRulesApplied      = "NATRulesApplied"
	EventReasonCiliumConfigMissing  = "CiliumConfigMissing"
)

// ImpVM condition type constants.
const (
	ConditionScheduled   = "Scheduled"
	ConditionReady       = "Ready"
	ConditionNodeHealthy = "NodeHealthy"
)

// ImpNetwork condition type constants.
const (
	ConditionNetworkReady = "Ready"
)
