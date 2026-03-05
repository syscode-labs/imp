package v1alpha1

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

func TestImpVMStatus_restartFields(t *testing.T) {
	now := metav1.Now()
	status := ImpVMStatus{
		RestartCount:   3,
		NextRetryAfter: &now,
	}
	b, err := json.Marshal(status)
	require.NoError(t, err)
	var out ImpVMStatus
	require.NoError(t, json.Unmarshal(b, &out))
	assert.Equal(t, int32(3), out.RestartCount)
	assert.NotNil(t, out.NextRetryAfter)
}

func TestImpVMStatus_exhaustedAt(t *testing.T) {
	exhausted := metav1.Now()
	status2 := ImpVMStatus{
		RestartCount: 5,
		ExhaustedAt:  &exhausted,
	}
	b2, err2 := json.Marshal(status2)
	require.NoError(t, err2)
	var out2 ImpVMStatus
	require.NoError(t, json.Unmarshal(b2, &out2))
	assert.NotNil(t, out2.ExhaustedAt)
	assert.Equal(t, int32(5), out2.RestartCount)
}

func TestRestartPolicy_roundTrip(t *testing.T) {
	rp := RestartPolicy{
		Mode:           "reschedule",
		Backoff:        RestartBackoff{MaxRetries: 10, InitialDelay: "5s", MaxDelay: "10m"},
		OnExhaustion:   "cool-down",
		CoolDownPeriod: "2h",
	}
	b, err := json.Marshal(rp)
	require.NoError(t, err)
	var out RestartPolicy
	require.NoError(t, json.Unmarshal(b, &out))
	assert.Equal(t, "reschedule", out.Mode)
	assert.Equal(t, int32(10), out.Backoff.MaxRetries)
	assert.Equal(t, "cool-down", out.OnExhaustion)
}
