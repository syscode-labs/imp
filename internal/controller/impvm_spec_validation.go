package controller

import (
	"fmt"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

// validateImpVMSpecRefs mirrors core reference invariants from admission validation
// so invalid specs are surfaced clearly even when webhooks are disabled.
func validateImpVMSpecRefs(vm *impdevv1alpha1.ImpVM) error {
	hasTemplate := vm.Spec.TemplateRef != nil
	hasClass := vm.Spec.ClassRef != nil

	// Keep backward compatibility for legacy minimal specs that set none of
	// image/classRef/templateRef. This path is still exercised by existing tests
	// and older manifests. We only fail fast on explicit but invalid wiring.
	if !hasTemplate && !hasClass && vm.Spec.Image == "" {
		return nil
	}

	switch {
	case hasTemplate && hasClass:
		return fmt.Errorf("invalid spec: classRef and templateRef are mutually exclusive")
	case !hasTemplate && !hasClass:
		return fmt.Errorf("invalid spec: exactly one of classRef or templateRef must be set")
	default:
		return nil
	}
}

func hasSpecInvalidCondition(vm *impdevv1alpha1.ImpVM, msg string) bool {
	ready := apimeta.FindStatusCondition(vm.Status.Conditions, impdevv1alpha1.ConditionReady)
	if ready == nil {
		return false
	}
	return ready.Status == metav1.ConditionFalse &&
		ready.Reason == EventReasonSpecInvalid &&
		ready.Message == msg
}
