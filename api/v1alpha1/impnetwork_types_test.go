package v1alpha1

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

func TestIPAMSpec_roundTrip(t *testing.T) {
	net := ImpNetwork{
		ObjectMeta: metav1.ObjectMeta{Name: "net1", Namespace: "default"},
		Spec: ImpNetworkSpec{
			Subnet: "10.0.0.0/24",
			IPAM: &IPAMSpec{
				Provider: "cilium",
				Cilium:   &CiliumIPAMSpec{PoolRef: "my-pool"},
			},
		},
	}
	b, err := json.Marshal(net)
	require.NoError(t, err)
	var out ImpNetwork
	require.NoError(t, json.Unmarshal(b, &out))
	assert.Equal(t, "cilium", out.Spec.IPAM.Provider)
	assert.Equal(t, "my-pool", out.Spec.IPAM.Cilium.PoolRef)
}

func TestNetworkGroupSpec_roundTrip(t *testing.T) {
	net := ImpNetwork{
		ObjectMeta: metav1.ObjectMeta{Name: "net2", Namespace: "default"},
		Spec: ImpNetworkSpec{
			Subnet: "10.0.0.0/24",
			Groups: []NetworkGroupSpec{
				{Name: "workers", Connectivity: "subnet", ExpectedSize: 20},
				{Name: "control", Connectivity: "policy-only", ExpectedSize: 5},
			},
		},
	}
	b, err := json.Marshal(net)
	require.NoError(t, err)
	var out ImpNetwork
	require.NoError(t, json.Unmarshal(b, &out))
	assert.Len(t, out.Spec.Groups, 2)
	assert.Equal(t, "workers", out.Spec.Groups[0].Name)
	assert.Equal(t, int32(20), out.Spec.Groups[0].ExpectedSize)
	assert.Equal(t, "policy-only", out.Spec.Groups[1].Connectivity)
}
