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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	basev1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

// TenancyMode selects the isolation tier for a sandbox.
type TenancyMode string

const (
	// TenancyStandard is the relaxed default: internet access is unrestricted
	// apart from the unconditional baseline deny of cloud metadata and
	// cluster-internal ranges.
	TenancyStandard TenancyMode = "standard"

	// TenancyHard enables inter-sandbox isolation and enforced egress policy.
	// Requires Cilium; admission fails closed without it.
	TenancyHard TenancyMode = "hard"
)

// SandboxOwnerLabel marks generated base resources with their owning sandbox.
const (
	SandboxOwnerLabel = "sandbox.imp.dev/owner"

	// SubnetIndexAnnotation records which allocation slot inside the sandbox
	// base block a generated ImpNetwork occupies.
	SubnetIndexAnnotation = "sandbox.imp.dev/subnet-index"
)

// ImpSandboxSpec defines the desired state of an ImpSandbox.
type ImpSandboxSpec struct {
	// TemplateRef references an ImpVMTemplate used as the sandbox image source.
	// Mutually exclusive with ClassRef.
	// +optional
	TemplateRef *basev1alpha1.LocalObjectRef `json:"templateRef,omitempty"`

	// ClassRef references an ImpVMClass directly. Mutually exclusive with TemplateRef.
	// +optional
	ClassRef *basev1alpha1.ClusterObjectRef `json:"classRef,omitempty"`

	// Image is the OCI image used as the VM rootfs when ClassRef is set
	// directly without a template.
	// +optional
	Image string `json:"image,omitempty"`

	// NetworkRef references an existing ImpNetwork to attach the sandbox to.
	// When unset, the controller creates a dedicated ImpNetwork per sandbox.
	// +optional
	NetworkRef *basev1alpha1.LocalObjectRef `json:"networkRef,omitempty"`

	// Tenancy selects the isolation tier.
	// +optional
	// +kubebuilder:default=standard
	// +kubebuilder:validation:Enum=standard;hard
	Tenancy TenancyMode `json:"tenancy,omitempty"`

	// ExpireAfter caps sandbox wall-clock runtime from first Running
	// transition. 0 or unset disables automatic expiration. Minimum enabled
	// value is 60s. Resolution precedence mirrors ImpVM: sandbox-level value
	// wins over template defaults.
	// +optional
	ExpireAfter *metav1.Duration `json:"expireAfter,omitempty"`

	// NodeSelector constrains scheduling of the underlying VM.
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`
}

// ImpSandboxStatus reflects the observed state of an ImpSandbox.
type ImpSandboxStatus struct {
	// Conditions represent the latest available observations of the sandbox.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// VMName is the name of the generated ImpVM.
	// +optional
	VMName string `json:"vmName,omitempty"`

	// NetworkName is the name of the attached or generated ImpNetwork.
	// +optional
	NetworkName string `json:"networkName,omitempty"`

	// EffectiveTenancy records the tenancy actually enforced after floor
	// resolution. May differ from spec.tenancy when a cluster floor applies.
	// +optional
	EffectiveTenancy TenancyMode `json:"effectiveTenancy,omitempty"`

	// SessionSecretRef points to the Secret holding this sandbox's session
	// token for data-plane access (gateway gRPC). The token is deterministic
	// HMAC(clusterKey, sandboxUID): the Secret is the delivery mechanism for
	// SDKs, not the source of truth — rotating the cluster key invalidates
	// all sessions at once.
	// +optional
	SessionSecretRef *basev1alpha1.LocalObjectRef `json:"sessionSecretRef,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=isbx,categories=imp
// +kubebuilder:printcolumn:name="Tenancy",type=string,JSONPath=`.status.effectiveTenancy`
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// ImpSandbox is an AI-agent sandbox backed by an Imp microVM. It expands into
// owned ImpVM/ImpNetwork resources in its own namespace and is managed by the
// optional imp-sandbox add-on.
type ImpSandbox struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ImpSandboxSpec   `json:"spec,omitempty"`
	Status ImpSandboxStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ImpSandboxList contains a list of ImpSandbox.
type ImpSandboxList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ImpSandbox `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ImpSandbox{}, &ImpSandboxList{})
}
