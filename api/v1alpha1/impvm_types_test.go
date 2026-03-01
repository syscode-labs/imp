package v1alpha1

import (
	"encoding/json"
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestImpVMSpec_TemplateRef_XOR_ClassRef(t *testing.T) {
	// A VM with only templateRef should round-trip cleanly
	vm := ImpVMSpec{
		TemplateRef: &LocalObjectRef{Name: "ubuntu-sandbox"},
		Lifecycle:   VMLifecycleEphemeral,
	}
	data, err := json.Marshal(vm)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got ImpVMSpec
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.TemplateRef == nil || got.TemplateRef.Name != "ubuntu-sandbox" {
		t.Fatal("TemplateRef lost in round-trip")
	}
	if got.ClassRef != nil {
		t.Fatal("ClassRef should be nil")
	}
}

func TestImpVMSpec_EnvVars(t *testing.T) {
	vm := ImpVMSpec{
		ClassRef: &ClusterObjectRef{Name: "small"},
		Image:    "ghcr.io/myorg/my-app:latest",
		Env: []corev1.EnvVar{
			{Name: "PORT", Value: "8080"},
		},
	}
	data, _ := json.Marshal(vm)
	var got ImpVMSpec
	json.Unmarshal(data, &got) //nolint:errcheck
	if len(got.Env) != 1 || got.Env[0].Name != "PORT" {
		t.Fatalf("Env lost in round-trip: %+v", got.Env)
	}
}
