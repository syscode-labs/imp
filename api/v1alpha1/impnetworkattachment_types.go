package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// AttachmentMode selects how an ImpVM reaches the allowlisted physical network.
// Only access mode is supported: the guest sees untagged frames on exactly one
// VLAN (or on the untagged native network).
// +kubebuilder:validation:Enum=access
type AttachmentMode string

const (
	// AttachmentModeAccess bridges the VM onto one VLAN or the untagged parent.
	AttachmentModeAccess AttachmentMode = "access"
)

// Annotations stamped by admission and mirrored into status.
const (
	// AnnotationRequester records the username that created the attachment.
	AnnotationRequester = "imp.dev/requester"
	// AnnotationRequesterGroups records the groups of the creating user.
	AnnotationRequesterGroups = "imp.dev/requester-groups"
	// AnnotationForceDelete overrides the running-VM deletion guard when set
	// to "true" by an administrator.
	AnnotationForceDelete = "imp.dev/force-delete"
)

// ImpNetworkAttachment phases.
const (
	// AttachmentPhasePending waits for its VM to exist and be scheduled.
	AttachmentPhasePending = "Pending"
	// AttachmentPhaseAuthorized passed allowlist, subject, and node-binding checks.
	AttachmentPhaseAuthorized = "Authorized"
	// AttachmentPhaseDenied failed a policy check; no host resources will be created.
	AttachmentPhaseDenied = "Denied"
	// AttachmentPhaseFailed means provisioning on the node failed.
	AttachmentPhaseFailed = "Failed"
)

// ImpNetworkAttachmentSpec defines the request to attach one ImpVM to one
// administrator-allowlisted LAN/VLAN attachment definition.
type ImpNetworkAttachmentSpec struct {
	// VMRef references the ImpVM in the same namespace whose guest joins the
	// allowlisted network.
	VMRef LocalObjectRef `json:"vmRef"`

	// AttachmentRef names a LAN attachment definition from
	// ClusterImpConfig.spec.networking.lanAttachments. The definition carries
	// the VLAN ID, subnet, and DHCP policy; host interfaces are never chosen
	// by users.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	AttachmentRef string `json:"attachmentRef"`

	// Mode selects the attachment mode. Only "access" is supported.
	// +kubebuilder:default=access
	Mode AttachmentMode `json:"mode,omitempty"`

	// DHCP declares that the guest will obtain its address via DHCP on the
	// attached network. Allowed only when the referenced definition sets
	// allowDHCP=true. Mutually exclusive with IP.
	// +optional
	DHCP *DHCPRequestSpec `json:"dhcp,omitempty"`

	// IP is a static address inside the definition subnet that the guest will
	// configure itself. Required when DHCP is not requested.
	// +optional
	// +kubebuilder:validation:Pattern=`^([0-9]{1,3}\.){3}[0-9]{1,3}$`
	IP string `json:"ip,omitempty"`
}

// DHCPRequestSpec requests guest-side DHCP on an attached network.
type DHCPRequestSpec struct {
	// Enabled turns on the DHCP request for this attachment.
	Enabled bool `json:"enabled"`
}

// ImpNetworkAttachmentStatus records authorization and audit state.
// Audit fields are written by the system only; treat them as immutable once set.
type ImpNetworkAttachmentStatus struct {
	// Phase is the high-level state of this attachment request.
	// +optional
	// +kubebuilder:validation:Enum=Pending;Authorized;Denied;Failed
	Phase string `json:"phase,omitempty"`

	// Requester is the authenticated username recorded at creation time.
	// Mirrored from the imp.dev/requester annotation by admission.
	// +optional
	Requester string `json:"requester,omitempty"`

	// Node is the node running the referenced ImpVM at authorization time.
	// +optional
	Node string `json:"node,omitempty"`

	// MACAddress is the deterministic MAC assigned to the VM's attached
	// interface.
	// +optional
	MACAddress string `json:"macAddress,omitempty"`

	// AssignedIP is the address observed on the guest interface: the requested
	// static IP or the DHCP lease reported by the guest agent.
	// +optional
	AssignedIP string `json:"assignedIP,omitempty"`

	// Conditions follow the standard k8s condition convention.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=impnetattach,categories=imp
// +kubebuilder:printcolumn:name="VM",type=string,JSONPath=`.spec.vmRef.name`
// +kubebuilder:printcolumn:name="Attachment",type=string,JSONPath=`.spec.attachmentRef`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Node",type=string,JSONPath=`.status.node`
// +kubebuilder:printcolumn:name="IP",type=string,JSONPath=`.status.assignedIP`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// ImpNetworkAttachment attaches one ImpVM to an administrator-allowlisted
// physical network or VLAN. Creating this resource requires permissions
// distinct from ImpVM/ImpNetwork roles. Isolated ImpNetwork networking remains
// the default for all workloads without this explicit grant.
type ImpNetworkAttachment struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ImpNetworkAttachmentSpec   `json:"spec,omitempty"`
	Status ImpNetworkAttachmentStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ImpNetworkAttachmentList contains a list of ImpNetworkAttachment.
type ImpNetworkAttachmentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ImpNetworkAttachment `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ImpNetworkAttachment{}, &ImpNetworkAttachmentList{})
}
