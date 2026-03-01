package v1alpha1

import (
	"encoding/json"
	"testing"
)

func TestImpNetworkSpec_RoundTrip(t *testing.T) {
	net := ImpNetworkSpec{
		Subnet:  "192.168.100.0/24",
		Gateway: "192.168.100.1",
		NAT:     NATSpec{Enabled: true, EgressInterface: "eth0"},
		DNS:     []string{"1.1.1.1", "8.8.8.8"},
		Cilium:  &CiliumNetworkSpec{ExcludeFromIPAM: true, MasqueradeViaCilium: true},
	}
	data, err := json.Marshal(net)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got ImpNetworkSpec
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Subnet != "192.168.100.0/24" {
		t.Fatalf("Subnet wrong: %v", got.Subnet)
	}
	if !got.NAT.Enabled {
		t.Fatal("NAT.Enabled lost")
	}
	if len(got.DNS) != 2 {
		t.Fatalf("DNS wrong: %v", got.DNS)
	}
	if got.Cilium == nil || !got.Cilium.MasqueradeViaCilium {
		t.Fatal("Cilium config lost")
	}
}
