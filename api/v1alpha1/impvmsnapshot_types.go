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

	// Retention is the number of completed executions to keep (oldest pruned first).
	// +optional
	// +kubebuilder:default=3
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=10
	Retention int32 `json:"retention,omitempty"`

	// Storage defines where the snapshot artifact is persisted.
	Storage SnapshotStorageSpec `json:"storage"`

	// BaseSnapshot pins a specific child execution as the elected base image.
	// Set declaratively or via `kubectl imp elect`.
	// Consumers (ImpWarmPool, ImpVMMigration) use this as their boot source.
	// The operator validates the named child exists and has phase=Succeeded.
	// +optional
	BaseSnapshot string `json:"baseSnapshot,omitempty"`
}

// SnapshotStorageSpec configures snapshot artifact storage.
type SnapshotStorageSpec struct {
	// Type selects the storage backend.
	// +kubebuilder:validation:Enum=node-local;oci-registry
	Type string `json:"type"`

	// NodeLocal configures node-local artifact storage.
	// Required when Type is "node-local".
	// +optional
	NodeLocal *NodeLocalSpec `json:"nodeLocal,omitempty"`

	// OCIRegistry configures an OCI registry destination.
	// Required when Type is "oci-registry".
	// +optional
	OCIRegistry *OCIRegistrySpec `json:"ociRegistry,omitempty"`
}

// NodeLocalSpec configures node-local snapshot storage.
type NodeLocalSpec struct {
	// Path is the base directory for snapshot artifacts on the node.
	// Supports remotely-mounted paths (NFS, etc.).
	// +kubebuilder:default="/var/lib/imp/snapshots"
	Path string `json:"path,omitempty"`
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

	// LastExecutionRef is the most recently created child execution object.
	// +optional
	LastExecutionRef *corev1.LocalObjectReference `json:"lastExecutionRef,omitempty"`

	// BaseSnapshot mirrors spec.baseSnapshot once the referenced child is
	// validated as Succeeded. Consumers read this field only.
	// +optional
	BaseSnapshot string `json:"baseSnapshot,omitempty"`

	// TerminatedAt is set by the agent once the execution reaches a terminal
	// state (Succeeded or Failed) and all cleanup is complete.
	// The operator uses this — not phase alone — to gate new execution creation.
	// +optional
	TerminatedAt *metav1.Time `json:"terminatedAt,omitempty"`

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
