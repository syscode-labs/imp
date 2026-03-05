package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// ImpVMTemplateSpec defines a reusable VM blueprint.
type ImpVMTemplateSpec struct {
	// ClassRef is the required compute class for VMs created from this template.
	ClassRef ClusterObjectRef `json:"classRef"`

	// NetworkRef optionally binds VMs to a specific ImpNetwork.
	// +optional
	NetworkRef *LocalObjectRef `json:"networkRef,omitempty"`

	// Image is the OCI image to use as the VM rootfs.
	// +optional
	Image string `json:"image,omitempty"`

	// Probes overrides probes inherited from the ImpVMClass.
	// +optional
	Probes *ProbeSpec `json:"probes,omitempty"`

	// GuestAgent controls guest agent injection. Overrides defaults when set.
	// +optional
	GuestAgent *GuestAgentConfig `json:"guestAgent,omitempty"`

	// RestartPolicy overrides the class-level restart policy for this template.
	// +optional
	RestartPolicy *RestartPolicy `json:"restartPolicy,omitempty"`

	// NetworkGroup places VMs from this template in a named group.
	// +optional
	NetworkGroup string `json:"networkGroup,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,shortName=impvmt,categories=imp
// +kubebuilder:printcolumn:name="Class",type=string,JSONPath=`.spec.classRef.name`
// +kubebuilder:printcolumn:name="Network",type=string,JSONPath=`.spec.networkRef.name`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// ImpVMTemplate is the Schema for the impvmtemplates API.
type ImpVMTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec ImpVMTemplateSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true

// ImpVMTemplateList contains a list of ImpVMTemplate.
type ImpVMTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ImpVMTemplate `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ImpVMTemplate{}, &ImpVMTemplateList{})
}
