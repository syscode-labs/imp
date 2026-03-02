/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	"context"

	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

// ImpVMWebhook implements defaulting and validation for ImpVM.
type ImpVMWebhook struct{}

var (
	_ admission.Defaulter[*impdevv1alpha1.ImpVM] = &ImpVMWebhook{}
	_ admission.Validator[*impdevv1alpha1.ImpVM] = &ImpVMWebhook{}
)

// Default implements admission.Defaulter[*impdevv1alpha1.ImpVM].
func (w *ImpVMWebhook) Default(_ context.Context, vm *impdevv1alpha1.ImpVM) error {
	if vm.Spec.Lifecycle == "" {
		vm.Spec.Lifecycle = impdevv1alpha1.VMLifecycleEphemeral
	}
	return nil
}

// ValidateCreate implements admission.Validator[*impdevv1alpha1.ImpVM].
func (w *ImpVMWebhook) ValidateCreate(_ context.Context, vm *impdevv1alpha1.ImpVM) (admission.Warnings, error) {
	return nil, validateImpVM(vm).ToAggregate()
}

// ValidateUpdate implements admission.Validator[*impdevv1alpha1.ImpVM].
func (w *ImpVMWebhook) ValidateUpdate(_ context.Context, oldVM, newVM *impdevv1alpha1.ImpVM) (admission.Warnings, error) {
	errs := validateImpVM(newVM)

	if oldVM.Spec.NodeName != "" && newVM.Spec.NodeName != oldVM.Spec.NodeName {
		errs = append(errs, field.Forbidden(
			field.NewPath("spec", "nodeName"),
			"nodeName is immutable once set",
		))
	}

	return nil, errs.ToAggregate()
}

// ValidateDelete implements admission.Validator[*impdevv1alpha1.ImpVM].
func (w *ImpVMWebhook) ValidateDelete(_ context.Context, _ *impdevv1alpha1.ImpVM) (admission.Warnings, error) {
	return nil, nil
}

// validateImpVM checks the spec invariants shared by create and update.
func validateImpVM(vm *impdevv1alpha1.ImpVM) field.ErrorList {
	var errs field.ErrorList

	hasTemplate := vm.Spec.TemplateRef != nil
	hasClass := vm.Spec.ClassRef != nil

	switch {
	case hasTemplate && hasClass:
		errs = append(errs, field.Invalid(
			field.NewPath("spec", "classRef"),
			vm.Spec.ClassRef,
			"classRef and templateRef are mutually exclusive",
		))
	case !hasTemplate && !hasClass:
		errs = append(errs, field.Required(
			field.NewPath("spec", "classRef"),
			"exactly one of classRef or templateRef must be set",
		))
	case hasClass && !hasTemplate && vm.Spec.Image == "":
		errs = append(errs, field.Required(
			field.NewPath("spec", "image"),
			"image is required when classRef is set without templateRef",
		))
	}

	return errs
}
