package agent

import impv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"

// ResolveGuestAgentEnabled resolves whether the guest agent should be injected
// for the given VM. Resolution order: ImpVM → ImpVMClass → default (true).
func ResolveGuestAgentEnabled(vm *impv1alpha1.ImpVM, class *impv1alpha1.ImpVMClass) bool {
	if vm.Spec.GuestAgent != nil && vm.Spec.GuestAgent.Enabled != nil {
		return *vm.Spec.GuestAgent.Enabled
	}
	if class != nil && class.Spec.GuestAgent != nil && class.Spec.GuestAgent.Enabled != nil {
		return *class.Spec.GuestAgent.Enabled
	}
	return true // default: enabled
}
