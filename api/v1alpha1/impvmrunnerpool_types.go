package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// ImpVMRunnerPoolSpec defines a pool of ephemeral CI runner VMs.
type ImpVMRunnerPoolSpec struct {
	// TemplateName references an ImpVMTemplate in the same namespace.
	TemplateName string `json:"templateName"`

	// Platform configures the CI platform integration.
	Platform RunnerPlatformSpec `json:"platform"`

	// RunnerLayer is the OCI image containing the runner binary.
	// When set, it is composited on top of the template image at boot time.
	// Omit if the runner binary is already baked into the template image.
	// +optional
	RunnerLayer string `json:"runnerLayer,omitempty"`

	// Labels are applied to runner registrations on the CI platform.
	// +optional
	Labels []string `json:"labels,omitempty"`

	// Scaling controls how many runner VMs are maintained.
	// +optional
	Scaling *RunnerScalingSpec `json:"scaling,omitempty"`

	// JobDetection configures how the operator discovers queued jobs.
	// +optional
	JobDetection *RunnerJobDetectionSpec `json:"jobDetection,omitempty"`

	// ExpireAfter sets VM expiration for runner VMs created by this pool.
	// 0 or unset disables automatic expiration. Minimum enabled value is 60s.
	// +optional
	// +kubebuilder:validation:XValidation:rule="duration(self) == duration('0s') || duration(self) >= duration('60s')",message="expireAfter must be 0 (disabled) or at least 60s"
	ExpireAfter *metav1.Duration `json:"expireAfter,omitempty"`
}

// RunnerPlatformSpec identifies the CI platform and credentials.
type RunnerPlatformSpec struct {
	// Type selects the CI platform driver.
	// +kubebuilder:validation:Enum=github-actions;gitlab;forgejo
	Type string `json:"type"`

	// ServerURL is required for GitLab and Forgejo. Leave empty for github.com.
	// +optional
	ServerURL string `json:"serverURL,omitempty"`

	// Scope configures the registration scope (org or repo).
	// +optional
	Scope *RunnerScopeSpec `json:"scope,omitempty"`

	// CredentialsSecret names a Secret containing the registration token or PAT.
	CredentialsSecret string `json:"credentialsSecret"`
}

// RunnerScopeSpec selects org-level or repo-level runner registration.
// Exactly one of Org or Repo must be set.
// +kubebuilder:validation:XValidation:rule="(size(self.org) > 0) != (size(self.repo) > 0)",message="set exactly one of org or repo"
type RunnerScopeSpec struct {
	// Org registers a runner for the entire organisation.
	// +optional
	Org string `json:"org,omitempty"`

	// Repo registers a runner for a single repository ("owner/repo").
	// +optional
	Repo string `json:"repo,omitempty"`
}

// RunnerScalingSpec controls pool size and concurrency.
type RunnerScalingSpec struct {
	// MinIdle is the number of pre-registered idle runner VMs to keep available.
	// 0 means pure on-demand — no idle VMs sit waiting.
	// +optional
	// +kubebuilder:default=0
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=3
	MinIdle int32 `json:"minIdle,omitempty"`

	// MaxConcurrent is the hard cap on simultaneous runner VMs.
	// +optional
	// +kubebuilder:default=10
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100
	MaxConcurrent int32 `json:"maxConcurrent,omitempty"`
}

// RunnerJobDetectionSpec configures job discovery.
type RunnerJobDetectionSpec struct {
	// Webhook enables platform-push job events.
	// +optional
	Webhook *RunnerWebhookSpec `json:"webhook,omitempty"`

	// Polling enables periodic API polling as a fallback.
	// +optional
	Polling *RunnerPollingSpec `json:"polling,omitempty"`
}

// RunnerWebhookSpec configures the inbound webhook listener.
type RunnerWebhookSpec struct {
	// Enabled turns on webhook-based job detection.
	// +optional
	Enabled bool `json:"enabled"`

	// SecretRef names a Secret containing the HMAC webhook secret.
	// +optional
	SecretRef string `json:"secretRef,omitempty"`
}

// RunnerPollingSpec configures periodic API polling.
type RunnerPollingSpec struct {
	// Enabled turns on polling-based job detection.
	// +optional
	Enabled bool `json:"enabled"`

	// IntervalSeconds is how often the operator polls the platform API.
	// +kubebuilder:default=30
	// +kubebuilder:validation:Minimum=10
	IntervalSeconds int32 `json:"intervalSeconds,omitempty"`
}

// ImpVMRunnerPoolStatus reflects the observed pool state.
type ImpVMRunnerPoolStatus struct {
	// IdleCount is the number of runner VMs currently registered and waiting for a job.
	// +optional
	IdleCount int32 `json:"idleCount,omitempty"`

	// ActiveCount is the number of runner VMs currently executing a job.
	// +optional
	ActiveCount int32 `json:"activeCount,omitempty"`

	// Conditions follow the standard k8s condition convention.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=imprp,categories=imp
// +kubebuilder:printcolumn:name="Platform",type=string,JSONPath=`.spec.platform.type`
// +kubebuilder:printcolumn:name="Template",type=string,JSONPath=`.spec.templateName`
// +kubebuilder:printcolumn:name="Idle",type=integer,JSONPath=`.status.idleCount`
// +kubebuilder:printcolumn:name="Active",type=integer,JSONPath=`.status.activeCount`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// ImpVMRunnerPool provisions ephemeral CI runner VMs that register with a CI
// platform, execute exactly one job, and then terminate.
type ImpVMRunnerPool struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ImpVMRunnerPoolSpec   `json:"spec,omitempty"`
	Status ImpVMRunnerPoolStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ImpVMRunnerPoolList contains a list of ImpVMRunnerPool.
type ImpVMRunnerPoolList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ImpVMRunnerPool `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ImpVMRunnerPool{}, &ImpVMRunnerPoolList{})
}
