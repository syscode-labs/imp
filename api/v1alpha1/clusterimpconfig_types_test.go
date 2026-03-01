package v1alpha1

import (
	"encoding/json"
	"testing"
)

func TestClusterImpConfigSpec_Defaults(t *testing.T) {
	cfg := ClusterImpConfigSpec{}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got ClusterImpConfigSpec
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// zero value should round-trip cleanly
	if got.Capacity.DefaultFraction != "" {
		t.Fatalf("unexpected default fraction: %v", got.Capacity.DefaultFraction)
	}
}

func TestClusterImpConfigSpec_CNIProvider(t *testing.T) {
	cfg := ClusterImpConfigSpec{
		Networking: NetworkingConfig{
			CNI: CNIConfig{
				AutoDetect: true,
				Provider:   "cilium-kubeproxy-free",
				NATBackend: "nftables",
			},
		},
	}
	data, _ := json.Marshal(cfg)
	var got ClusterImpConfigSpec
	json.Unmarshal(data, &got) //nolint:errcheck
	if got.Networking.CNI.Provider != "cilium-kubeproxy-free" {
		t.Fatalf("CNI provider lost: %v", got.Networking.CNI.Provider)
	}
}
