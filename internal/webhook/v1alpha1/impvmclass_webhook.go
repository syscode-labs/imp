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

	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

// ImpVMClassWebhook validates ImpVMClass objects.
// Numeric field minimums (VCPU≥1, MemoryMiB≥128, DiskGiB≥1) are enforced by
// CEL validation markers on the CRD schema; this webhook is a registration
// stub that will hold immutability checks in Phase 2.
type ImpVMClassWebhook struct{}

var _ admission.Validator[*impdevv1alpha1.ImpVMClass] = &ImpVMClassWebhook{}

// ValidateCreate implements admission.Validator[*impdevv1alpha1.ImpVMClass].
func (w *ImpVMClassWebhook) ValidateCreate(_ context.Context, _ *impdevv1alpha1.ImpVMClass) (admission.Warnings, error) {
	return nil, nil
}

// ValidateUpdate implements admission.Validator[*impdevv1alpha1.ImpVMClass].
func (w *ImpVMClassWebhook) ValidateUpdate(_ context.Context, _, _ *impdevv1alpha1.ImpVMClass) (admission.Warnings, error) {
	return nil, nil
}

// ValidateDelete implements admission.Validator[*impdevv1alpha1.ImpVMClass].
func (w *ImpVMClassWebhook) ValidateDelete(_ context.Context, _ *impdevv1alpha1.ImpVMClass) (admission.Warnings, error) {
	return nil, nil
}
