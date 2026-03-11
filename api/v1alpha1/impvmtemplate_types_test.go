package v1alpha1

import (
	"encoding/json"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestImpVMTemplateSpec_RoundTrip(t *testing.T) {
	tmpl := ImpVMTemplateSpec{
		ClassRef:   ClusterObjectRef{Name: "small"},
		NetworkRef: &LocalObjectRef{Name: "sandbox-net"},
		Image:      "ghcr.io/myorg/rootfs:ubuntu-22.04",
		Probes: &ProbeSpec{
			ReadinessProbe: &Probe{
				HTTP: &HTTPGetAction{Path: "/ready", Port: 8080},
			},
		},
		ExpireAfter: &metav1.Duration{Duration: 2 * time.Hour},
	}
	data, err := json.Marshal(tmpl)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got ImpVMTemplateSpec
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ClassRef.Name != "small" {
		t.Fatalf("ClassRef wrong: %v", got.ClassRef)
	}
	if got.Probes == nil || got.Probes.ReadinessProbe == nil || got.Probes.ReadinessProbe.HTTP == nil {
		t.Fatal("ReadinessProbe.HTTP lost")
	}
	if got.ExpireAfter == nil || got.ExpireAfter.Duration != 2*time.Hour {
		t.Fatalf("ExpireAfter wrong: %#v", got.ExpireAfter)
	}
}
