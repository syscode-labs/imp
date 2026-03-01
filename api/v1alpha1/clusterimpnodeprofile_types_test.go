package v1alpha1

import (
	"encoding/json"
	"testing"
)

func TestClusterImpNodeProfileSpec_RoundTrip(t *testing.T) {
	np := ClusterImpNodeProfileSpec{
		CapacityFraction: "0.85",
		MaxImpVMs:        10,
	}
	data, err := json.Marshal(np)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got ClusterImpNodeProfileSpec
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.CapacityFraction != "0.85" {
		t.Fatalf("CapacityFraction wrong: %v", got.CapacityFraction)
	}
	if got.MaxImpVMs != 10 {
		t.Fatalf("MaxImpVMs wrong: %v", got.MaxImpVMs)
	}
}
