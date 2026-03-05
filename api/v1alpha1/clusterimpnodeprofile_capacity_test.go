package v1alpha1_test

import (
	"encoding/json"
	"testing"

	v1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

func TestClusterImpNodeProfile_capacityFieldsRoundTrip(t *testing.T) {
	p := v1alpha1.ClusterImpNodeProfile{}
	p.Spec.VCPUCapacity = 16
	p.Spec.MemoryMiB = 32768

	data, err := json.Marshal(p.Spec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got v1alpha1.ClusterImpNodeProfileSpec
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.VCPUCapacity != 16 {
		t.Errorf("VCPUCapacity: got %d, want 16", got.VCPUCapacity)
	}
	if got.MemoryMiB != 32768 {
		t.Errorf("MemoryMiB: got %d, want 32768", got.MemoryMiB)
	}
}
