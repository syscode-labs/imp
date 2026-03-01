package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// ImpVMClassSpec defines compute resources for a class of VMs.
// All ImpVMs and ImpVMTemplates referencing this class inherit these values.
type ImpVMClassSpec struct {
	// VCPU is the number of virtual CPUs allocated to each VM.
	// +kubebuilder:validation:Minimum=1
	VCPU int32 `json:"vcpu"`

	// MemoryMiB is the amount of RAM in mebibytes.
	// +kubebuilder:validation:Minimum=128
	MemoryMiB int32 `json:"memoryMiB"`

	// DiskGiB is the root disk size in gibibytes.
	// +kubebuilder:validation:Minimum=1
	DiskGiB int32 `json:"diskGiB"`

	// Arch is the CPU architecture this class targets.
	// Use "multi" to allow scheduling on either amd64 or arm64 nodes.
	// +optional
	// +kubebuilder:default=multi
	Arch Arch `json:"arch,omitempty"`

	// Probes defines default probes for VMs using this class.
	// Can be overridden by ImpVMTemplate or ImpVM.
	// +optional
	Probes *ProbeSpec `json:"probes,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,shortName=impcls,categories=imp
// +kubebuilder:printcolumn:name="VCPU",type=integer,JSONPath=`.spec.vcpu`
// +kubebuilder:printcolumn:name="Memory",type=integer,JSONPath=`.spec.memoryMiB`
// +kubebuilder:printcolumn:name="Disk",type=integer,JSONPath=`.spec.diskGiB`
// +kubebuilder:printcolumn:name="Arch",type=string,JSONPath=`.spec.arch`

// ImpVMClass is the Schema for the impvmclasses API.
type ImpVMClass struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec ImpVMClassSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true

// ImpVMClassList contains a list of ImpVMClass.
type ImpVMClassList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ImpVMClass `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ImpVMClass{}, &ImpVMClassList{})
}
