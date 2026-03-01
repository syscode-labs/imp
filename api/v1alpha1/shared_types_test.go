package v1alpha1

import (
	"encoding/json"
	"testing"
)

func TestProbeSpec_RoundTrip(t *testing.T) {
	original := ProbeSpec{
		StartupProbe: &Probe{
			Exec:                &ExecAction{Command: []string{"systemctl", "is-system-running"}},
			InitialDelaySeconds: 2,
			PeriodSeconds:       1,
			FailureThreshold:    30,
		},
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got ProbeSpec
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.StartupProbe == nil || got.StartupProbe.Exec == nil {
		t.Fatal("StartupProbe.Exec lost in round-trip")
	}
	if got.StartupProbe.Exec.Command[0] != "systemctl" {
		t.Fatalf("unexpected command: %v", got.StartupProbe.Exec.Command)
	}
}

func TestVMLifecycle_Values(t *testing.T) {
	cases := []VMLifecycle{VMLifecycleEphemeral, VMLifecyclePersistent}
	for _, c := range cases {
		data, _ := json.Marshal(c)
		var got VMLifecycle
		json.Unmarshal(data, &got) //nolint:errcheck
		if got != c {
			t.Errorf("round-trip failed for %q", c)
		}
	}
}
