package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ImpVMSnapshotSpec defines the desired snapshot.
type ImpVMSnapshotSpec struct {
	// SourceVMName is the name of the running ImpVM to snapshot.
	SourceVMName string `json:"sourceVMName"`

	// SourceVMNamespace is the namespace of the source ImpVM.
	SourceVMNamespace string `json:"sourceVMNamespace"`

	// Schedule is an optional cron expression for recurring snapshots.
	// e.g. "0 2 * * *" — daily at 02:00 UTC.
	// +optional
	Schedule string `json:"schedule,omitempty"`

	// Retention is the number of completed snapshots to keep (oldest pruned first).
	// +optional
	// +kubebuilder:default=3
	Retention int32 `json:"retention,omitempty"`

	// Storage defines where the snapshot artifact is persisted.
	Storage SnapshotStorageSpec `json:"storage"`
}

// SnapshotStorageSpec configures snapshot artifact storage.
type SnapshotStorageSpec struct {
	// Type selects the storage backend.
	// +kubebuilder:validation:Enum=node-local;oci-registry
	Type string `json:"type"`

	// OCIRegistry configures an OCI registry destination.
	// Required when Type is "oci-registry".
	// +optional
	OCIRegistry *OCIRegistrySpec `json:"ociRegistry,omitempty"`
}

// OCIRegistrySpec configures OCI registry access for snapshot storage.
type OCIRegistrySpec struct {
	// Repository is the full image reference prefix
	// (e.g. "ghcr.io/org/imp-snapshots").
	Repository string `json:"repository"`

	// PullSecretRef references a Secret with registry credentials.
	// +optional
	PullSecretRef *corev1.LocalObjectReference `json:"pullSecretRef,omitempty"`
}

// ImpVMSnapshotStatus reflects the observed state.
type ImpVMSnapshotStatus struct {
	// Phase is the current snapshot phase.
	// +optional
	Phase string `json:"phase,omitempty"`

	// Digest is the OCI digest of the produced artifact (oci-registry only).
	// +optional
	Digest string `json:"digest,omitempty"`

	// SnapshotPath is the node-local path of the snapshot artifact (node-local only).
	// +optional
	SnapshotPath string `json:"snapshotPath,omitempty"`

	// CompletedAt is the time the last snapshot completed.
	// +optional
	CompletedAt *metav1.Time `json:"completedAt,omitempty"`

	// NextScheduledAt is when the next scheduled snapshot will run.
	// +optional
	NextScheduledAt *metav1.Time `json:"nextScheduledAt,omitempty"`

	// Conditions follow the standard k8s condition convention.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=impsnap,categories=imp
// +kubebuilder:printcolumn:name="Source",type=string,JSONPath=`.spec.sourceVMName`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Schedule",type=string,JSONPath=`.spec.schedule`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// ImpVMSnapshot triggers a point-in-time snapshot of a running ImpVM.
// Optionally repeats on a cron schedule. Artifacts are stored per spec.storage.
type ImpVMSnapshot struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ImpVMSnapshotSpec   `json:"spec,omitempty"`
	Status ImpVMSnapshotStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ImpVMSnapshotList contains a list of ImpVMSnapshot.
type ImpVMSnapshotList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ImpVMSnapshot `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ImpVMSnapshot{}, &ImpVMSnapshotList{})
}
