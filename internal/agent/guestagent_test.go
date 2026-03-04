package agent_test

import (
	"testing"

	impv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
	"github.com/syscode-labs/imp/internal/agent"
)

func boolPtr(b bool) *bool { return &b }

func TestResolveGuestAgentEnabled(t *testing.T) {
	tests := []struct {
		name  string
		vm    *impv1alpha1.ImpVM
		class *impv1alpha1.ImpVMClass
		want  bool
	}{
		{
			name:  "default true when all nil",
			vm:    &impv1alpha1.ImpVM{},
			class: &impv1alpha1.ImpVMClass{},
			want:  true,
		},
		{
			name: "class disables",
			vm:   &impv1alpha1.ImpVM{},
			class: &impv1alpha1.ImpVMClass{
				Spec: impv1alpha1.ImpVMClassSpec{
					GuestAgent: &impv1alpha1.GuestAgentConfig{Enabled: boolPtr(false)},
				},
			},
			want: false,
		},
		{
			name: "vm overrides class to enable",
			vm: &impv1alpha1.ImpVM{
				Spec: impv1alpha1.ImpVMSpec{
					GuestAgent: &impv1alpha1.GuestAgentConfig{Enabled: boolPtr(true)},
				},
			},
			class: &impv1alpha1.ImpVMClass{
				Spec: impv1alpha1.ImpVMClassSpec{
					GuestAgent: &impv1alpha1.GuestAgentConfig{Enabled: boolPtr(false)},
				},
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := agent.ResolveGuestAgentEnabled(tt.vm, tt.class)
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}
