package controller

// Event reason constants emitted on ImpVM objects.
const (
	EventReasonScheduled            = "Scheduled"
	EventReasonUnschedulable        = "Unschedulable"
	EventReasonNodeLost             = "NodeLost"
	EventReasonRescheduling         = "Rescheduling"
	EventReasonTerminating          = "Terminating"
	EventReasonTerminationTimeout   = "TerminationTimeout"
	EventReasonExpired              = "Expired"
	EventReasonHealthCheckFailed    = "HealthCheckFailed"
	EventReasonHealthCheckRecovered = "HealthCheckRecovered"
	EventReasonSpecInvalid          = "SpecInvalid"
	EventReasonCNIDetected          = "CNIDetected"
	EventReasonCNIAmbiguous         = "CNIAmbiguous"
	EventReasonBridgeReady          = "BridgeReady"
	EventReasonIPAllocated          = "IPAllocated"
	EventReasonNATRulesApplied      = "NATRulesApplied"
	EventReasonCiliumConfigMissing  = "CiliumConfigMissing"
	EventReasonGroupCIDRError       = "GroupCIDRError"
)
