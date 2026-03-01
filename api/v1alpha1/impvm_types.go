/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ImpVMSpec defines the desired state of an ImpVM.
// Exactly one of TemplateRef or ClassRef must be set.
type ImpVMSpec struct {
	// TemplateRef references an ImpVMTemplate. Mutually exclusive with ClassRef.
	// +optional
	TemplateRef *LocalObjectRef `json:"templateRef,omitempty"`

	// ClassRef references an ImpVMClass directly. Mutually exclusive with TemplateRef.
	// +optional
	ClassRef *ClusterObjectRef `json:"classRef,omitempty"`

	// NetworkRef references the ImpNetwork to attach this VM to.
	// +optional
	NetworkRef *LocalObjectRef `json:"networkRef,omitempty"`

	// Image is the OCI image used as the VM rootfs. CMD/ENTRYPOINT from the
	// manifest become the VM's init command. Required when ClassRef is set directly.
	// +optional
	Image string `json:"image,omitempty"`

	// Lifecycle controls VM behaviour after the workload exits.
	// +optional
	// +kubebuilder:default=ephemeral
	Lifecycle VMLifecycle `json:"lifecycle,omitempty"`

	// NodeName pins the VM to a specific node. Set by the operator scheduler;
	// do not set manually unless you know what you're doing.
	// +optional
	NodeName string `json:"nodeName,omitempty"`

	// NodeSelector constrains scheduling to nodes matching all labels.
	// Same semantics as Pod.spec.nodeSelector.
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// Env sets environment variables inside the VM via the guest agent.
	// +optional
	Env []corev1.EnvVar `json:"env,omitempty"`

	// EnvFrom populates env vars from ConfigMaps or Secrets.
	// +optional
	EnvFrom []corev1.EnvFromSource `json:"envFrom,omitempty"`

	// UserData is an optional cloud-init user-data payload. Off by default;
	// use the guest agent + VSOCK for env injection instead.
	// +optional
	UserData *UserDataSource `json:"userData,omitempty"`

	// Probes override probe settings inherited from ImpVMTemplate or ImpVMClass.
	// +optional
	Probes *ProbeSpec `json:"probes,omitempty"`
}

// UserDataSource references a ConfigMap containing cloud-init user-data.
type UserDataSource struct {
	ConfigMapRef LocalObjectRef `json:"configMapRef"`
}

// ImpVMStatus reflects the observed state of an ImpVM.
type ImpVMStatus struct {
	// Phase is the current lifecycle phase.
	// +optional
	Phase VMPhase `json:"phase,omitempty"`

	// NodeName is the node where the VM is running.
	// +optional
	NodeName string `json:"nodeName,omitempty"`

	// IP is the IP address assigned to the VM by the ImpNetwork.
	// +optional
	IP string `json:"ip,omitempty"`

	// FirecrackerPID is the PID of the Firecracker process on the node (informational).
	// +optional
	FirecrackerPID int64 `json:"firecrackerPID,omitempty"`

	// Conditions follow the standard k8s condition convention.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=impvm,categories=imp
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Node",type=string,JSONPath=`.status.nodeName`
// +kubebuilder:printcolumn:name="IP",type=string,JSONPath=`.status.ip`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// ImpVM is the Schema for the impvms API.
type ImpVM struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ImpVMSpec   `json:"spec,omitempty"`
	Status ImpVMStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ImpVMList contains a list of ImpVM.
type ImpVMList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ImpVM `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ImpVM{}, &ImpVMList{})
}
