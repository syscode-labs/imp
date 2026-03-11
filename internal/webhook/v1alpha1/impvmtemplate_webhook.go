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

// ImpVMTemplateWebhook validates ImpVMTemplate objects.
type ImpVMTemplateWebhook struct{}

var _ admission.Validator[*impdevv1alpha1.ImpVMTemplate] = &ImpVMTemplateWebhook{}

// ValidateCreate implements admission.Validator[*impdevv1alpha1.ImpVMTemplate].
func (w *ImpVMTemplateWebhook) ValidateCreate(_ context.Context, tmpl *impdevv1alpha1.ImpVMTemplate) (admission.Warnings, error) {
	return nil, validateImpVMTemplate(tmpl).ToAggregate()
}

// ValidateUpdate implements admission.Validator[*impdevv1alpha1.ImpVMTemplate].
func (w *ImpVMTemplateWebhook) ValidateUpdate(_ context.Context, _, newTmpl *impdevv1alpha1.ImpVMTemplate) (admission.Warnings, error) {
	return nil, validateImpVMTemplate(newTmpl).ToAggregate()
}

// ValidateDelete implements admission.Validator[*impdevv1alpha1.ImpVMTemplate].
func (w *ImpVMTemplateWebhook) ValidateDelete(_ context.Context, _ *impdevv1alpha1.ImpVMTemplate) (admission.Warnings, error) {
	return nil, nil
}

// validateImpVMTemplate checks the spec invariants shared by create and update.
func validateImpVMTemplate(tmpl *impdevv1alpha1.ImpVMTemplate) field.ErrorList {
	var errs field.ErrorList

	if tmpl.Spec.ClassRef.Name == "" {
		errs = append(errs, field.Required(
			field.NewPath("spec", "classRef", "name"),
			"classRef.name is required",
		))
	}
	if tmpl.Spec.ExpireAfter != nil && tmpl.Spec.ExpireAfter.Duration < 0 {
		errs = append(errs, field.Invalid(
			field.NewPath("spec", "expireAfter"),
			tmpl.Spec.ExpireAfter.Duration.String(),
			"expireAfter must be >= 0",
		))
	}

	return errs
}
