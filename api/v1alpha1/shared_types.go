package v1alpha1

// LocalObjectRef is a reference to an object in the same namespace.
type LocalObjectRef struct {
	Name string `json:"name"`
}

// ClusterObjectRef is a reference to a cluster-scoped object.
type ClusterObjectRef struct {
	Name string `json:"name"`
}

// ProbeSpec holds optional startup, readiness, and liveness probes.
// The most specific definition in the inheritance chain wins:
// ImpVMClass → ImpVMTemplate → ImpVM.
type ProbeSpec struct {
	StartupProbe   *Probe `json:"startupProbe,omitempty"`
	ReadinessProbe *Probe `json:"readinessProbe,omitempty"`
	LivenessProbe  *Probe `json:"livenessProbe,omitempty"`
	// HTTPCheck configures an optional operator-driven HTTP health check (opt-in).
	// +optional
	HTTPCheck *HTTPCheckSpec `json:"httpCheck,omitempty"`
}

// Probe defines a single probe. Only one of Exec or HTTP may be set.
type Probe struct {
	// +optional
	Exec *ExecAction `json:"exec,omitempty"`
	// +optional
	HTTP *HTTPGetAction `json:"http,omitempty"`

	// +optional
	// +kubebuilder:default=0
	InitialDelaySeconds int32 `json:"initialDelaySeconds,omitempty"`
	// +optional
	// +kubebuilder:default=10
	PeriodSeconds int32 `json:"periodSeconds,omitempty"`
	// +optional
	// +kubebuilder:default=3
	FailureThreshold int32 `json:"failureThreshold,omitempty"`
}

// ExecAction runs a command inside the VM via VSOCK guest agent.
type ExecAction struct {
	// +kubebuilder:validation:MinItems=1
	Command []string `json:"command"`
}

// HTTPGetAction performs an HTTP GET against the VM's guest agent.
type HTTPGetAction struct {
	Path string `json:"path"`
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	Port int32 `json:"port"`
}

// VMLifecycle controls what happens when an ImpVM finishes.
// +kubebuilder:validation:Enum=ephemeral;persistent
type VMLifecycle string

const (
	// VMLifecycleEphemeral deletes the VM once its workload exits.
	VMLifecycleEphemeral VMLifecycle = "ephemeral"
	// VMLifecyclePersistent keeps the VM running until explicitly deleted.
	VMLifecyclePersistent VMLifecycle = "persistent"
)

// VMPhase is the current lifecycle phase of an ImpVM.
// +kubebuilder:validation:Enum=Pending;Scheduled;Starting;Running;Terminating;Succeeded;Failed
type VMPhase string

const (
	VMPhasePending     VMPhase = "Pending"
	VMPhaseScheduled   VMPhase = "Scheduled"
	VMPhaseStarting    VMPhase = "Starting"
	VMPhaseRunning     VMPhase = "Running"
	VMPhaseTerminating VMPhase = "Terminating"
	VMPhaseSucceeded   VMPhase = "Succeeded"
	VMPhaseFailed      VMPhase = "Failed"
)

// Arch is the CPU architecture for a VM class.
// +kubebuilder:validation:Enum=amd64;arm64;multi
type Arch string

const (
	ArchAMD64 Arch = "amd64"
	ArchARM64 Arch = "arm64"
	ArchMulti Arch = "multi"
)

// HTTPCheckSpec configures the operator-side HTTP health check (opt-in).
// Enabled per-VM via spec.probes.httpCheck or cluster-wide via ClusterImpConfig.spec.defaultHttpCheck.
type HTTPCheckSpec struct {
	// Enabled turns on the operator HTTP health check.
	// +optional
	Enabled bool `json:"enabled,omitempty"`
	// Path is the HTTP path to GET. Defaults to /healthz.
	// +optional
	// +kubebuilder:default=/healthz
	Path string `json:"path,omitempty"`
	// Port is the TCP port to check.
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	Port int32 `json:"port,omitempty"`
	// IntervalSeconds is how often the operator checks the endpoint.
	// +optional
	// +kubebuilder:default=10
	IntervalSeconds int32 `json:"intervalSeconds,omitempty"`
	// FailureThreshold is the number of consecutive failures before marking Ready=False.
	// +optional
	// +kubebuilder:default=3
	FailureThreshold int32 `json:"failureThreshold,omitempty"`
}
