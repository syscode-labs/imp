package v1alpha1

import (
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ClusterImpConfigSpec defines operator-wide settings.
// There must be exactly one ClusterImpConfig named "cluster" per cluster.
type ClusterImpConfigSpec struct {
	// Networking controls CNI detection and NAT backend selection.
	// +optional
	Networking NetworkingConfig `json:"networking,omitempty"`

	// Capacity sets default compute headroom across all nodes.
	// +optional
	Capacity CapacityConfig `json:"capacity,omitempty"`

	// Observability configures metrics and tracing.
	// +optional
	Observability ObservabilityConfig `json:"observability,omitempty"`

	// DefaultHttpCheck sets the cluster-wide default for the operator HTTP health check.
	// Individual ImpVMs can override this per-VM via spec.probes.httpCheck.
	// Disabled by default.
	// +optional
	DefaultHttpCheck *HTTPCheckSpec `json:"defaultHttpCheck,omitempty"`

	// Sandbox configures settings for the optional Imp Sandbox add-on.
	// Ignored when the imp-sandbox component is not installed.
	// +optional
	Sandbox *SandboxConfig `json:"sandbox,omitempty"`
}

// SandboxConfig holds cluster-wide settings for the Imp Sandbox add-on.
type SandboxConfig struct {
	// FloorTenancy raises the minimum tenancy tier accepted for sandboxes
	// cluster-wide. When set to "hard", all sandboxes must request hard
	// tenancy regardless of their spec. Requires the imp-sandbox add-on.
	// +optional
	// +kubebuilder:default=standard
	// +kubebuilder:validation:Enum=standard;hard
	FloorTenancy string `json:"floorTenancy,omitempty"`
}

// NetworkingConfig holds cluster-wide networking settings.
type NetworkingConfig struct {
	// CNI controls how the operator detects and integrates with the cluster CNI.
	// +optional
	CNI CNIConfig `json:"cni,omitempty"`

	// IPAM controls IP address management for ImpNetworks.
	// +optional
	IPAM IPAMConfig `json:"ipam,omitempty"`

	// LANAttachments declares the allowlisted physical network/VLAN attachment
	// definitions. ImpNetworkAttachment resources may reference these by name
	// only; users can never specify host interfaces or arbitrary VLANs.
	// +optional
	// +listType=map
	// +listMapKey=name
	LANAttachments []LANAttachmentSpec `json:"lanAttachments,omitempty"`
}

// LANAttachmentSpec is one administrator-defined allowlist entry for elevated
// physical LAN/VLAN attachment.
type LANAttachmentSpec struct {
	// Name identifies this definition. ImpNetworkAttachments reference it via
	// spec.attachmentRef.
	Name string `json:"name"`

	// VLANID is the 802.1Q VLAN tag guests are bridged onto.
	// 0 means untagged: VMs bridge directly onto the bound parent interface.
	// 1–4094 selects a tagged VLAN subinterface (<parent>.<vlanID>).
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=4094
	VLANID int32 `json:"vlanID"`

	// SubnetCIDR is the IP subnet of the physical network, used to validate
	// static addresses and to document the lease range.
	// +kubebuilder:validation:Pattern=`^([0-9]{1,3}\.){3}[0-9]{1,3}/[0-9]{1,2}$`
	SubnetCIDR string `json:"subnetCIDR"`

	// AllowDHCP permits ImpNetworkAttachments on this definition to request a
	// guest-side DHCP lease.
	// +optional
	AllowDHCP bool `json:"allowDHCP,omitempty"`

	// AllowedSubjects optionally restricts which authenticated users or groups
	// may create attachments referencing this definition. Entries use RBAC
	// subject syntax ("user:alice", "group:platform-admins"). Empty means any
	// holder of the LAN-attachment RBAC permission is allowed.
	// +optional
	AllowedSubjects []string `json:"allowedSubjects,omitempty"`
}

// Permits reports whether the authenticated identity matches this
// definition's subject allowlist. An empty allowlist permits any identity;
// an empty username never matches a non-empty allowlist.
func (s *LANAttachmentSpec) Permits(username string, groups []string) bool {
	if len(s.AllowedSubjects) == 0 {
		return true
	}
	if username == "" {
		return false
	}
	for _, subj := range s.AllowedSubjects {
		switch {
		case strings.HasPrefix(subj, "user:"):
			if subj[len("user:"):] == username {
				return true
			}
		case strings.HasPrefix(subj, "group:"):
			want := subj[len("group:"):]
			for _, have := range groups {
				if have == want {
					return true
				}
			}
		}
	}
	return false
}

// CNIConfig configures CNI detection and NAT backend selection.
type CNIConfig struct {
	// AutoDetect enables automatic CNI detection at operator startup.
	// +optional
	// +kubebuilder:default=true
	AutoDetect bool `json:"autoDetect,omitempty"`

	// Provider explicitly sets the CNI, skipping auto-detection.
	// Valid values: cilium-kubeproxy-free, cilium, flannel, calico, unknown.
	// +optional
	Provider string `json:"provider,omitempty"`

	// NATBackend selects the NAT implementation used by the node agent.
	// +optional
	// +kubebuilder:validation:Enum=nftables;iptables
	NATBackend string `json:"natBackend,omitempty"`
}

// IPAMConfig controls IP address management.
type IPAMConfig struct {
	// Provider selects the IPAM backend.
	// +optional
	// +kubebuilder:default=internal
	// +kubebuilder:validation:Enum=internal
	Provider string `json:"provider,omitempty"`
}

// CapacityConfig controls how much node compute is reserved for ImpVMs.
type CapacityConfig struct {
	// DefaultFraction is the fraction of node allocatable CPU and memory
	// available to ImpVMs across all nodes. Range: 0.0–1.0.
	// +optional
	// +kubebuilder:default="0.9"
	// +kubebuilder:validation:Pattern=`^(0(\.[0-9]+)?|1(\.0+)?)$`
	DefaultFraction string `json:"defaultFraction,omitempty"`
}

// ObservabilityConfig enables metrics and tracing.
type ObservabilityConfig struct {
	// Metrics configures Prometheus metrics exposure.
	// +optional
	Metrics MetricsConfig `json:"metrics,omitempty"`

	// Tracing configures OpenTelemetry trace export (opt-in).
	// +optional
	Tracing TracingConfig `json:"tracing,omitempty"`
}

// MetricsConfig controls Prometheus metrics.
type MetricsConfig struct {
	// Enabled turns on the /metrics endpoint.
	// +optional
	// +kubebuilder:default=true
	Enabled bool `json:"enabled,omitempty"`

	// Port is the port the /metrics endpoint listens on.
	// +optional
	// +kubebuilder:default=9090
	Port int32 `json:"port,omitempty"`
}

// TracingConfig controls OpenTelemetry tracing.
type TracingConfig struct {
	// Enabled turns on trace export. Off by default.
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// Endpoint is the OTLP gRPC endpoint (e.g. "http://otel-collector:4317").
	// +optional
	Endpoint string `json:"endpoint,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,shortName=impcfg,categories=imp
// +kubebuilder:printcolumn:name="CNI",type=string,JSONPath=`.spec.networking.cni.provider`
// +kubebuilder:printcolumn:name="DefaultFraction",type=string,JSONPath=`.spec.capacity.defaultFraction`

// ClusterImpConfig is the Schema for the clusterimpconfigs API.
// Create exactly one instance named "cluster".
type ClusterImpConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec ClusterImpConfigSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true

// ClusterImpConfigList contains a list of ClusterImpConfig.
type ClusterImpConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterImpConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClusterImpConfig{}, &ClusterImpConfigList{})
}
