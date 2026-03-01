package v1alpha1

import (
	"encoding/json"
	"testing"
)

func TestImpVMClassSpec_RoundTrip(t *testing.T) {
	cls := ImpVMClassSpec{
		VCPU:      2,
		MemoryMiB: 1024,
		DiskGiB:   20,
		Arch:      ArchMulti,
		Probes: &ProbeSpec{
			StartupProbe: &Probe{
				Exec:             &ExecAction{Command: []string{"systemctl", "is-system-running"}},
				PeriodSeconds:    1,
				FailureThreshold: 30,
			},
		},
	}
	data, err := json.Marshal(cls)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got ImpVMClassSpec
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.VCPU != 2 || got.MemoryMiB != 1024 || got.DiskGiB != 20 {
		t.Fatalf("compute fields wrong: %+v", got)
	}
	if got.Arch != ArchMulti {
		t.Fatalf("arch wrong: %v", got.Arch)
	}
	if got.Probes == nil || got.Probes.StartupProbe == nil {
		t.Fatal("Probes lost in round-trip")
	}
}
