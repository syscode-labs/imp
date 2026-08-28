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

// Package sandboxv1alpha1 implements admission webhooks for the sandbox.imp.dev
// v1alpha1 group. Served exclusively by cmd/sandbox.
package sandboxv1alpha1

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	sandboxv1alpha1 "github.com/syscode-labs/imp/api/sandbox/v1alpha1"
	impv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
	"github.com/syscode-labs/imp/internal/cnidetect"
)

// ImpSandboxWebhook validates ImpSandbox objects.
type ImpSandboxWebhook struct {
	// Client resolves ClusterImpConfig for floor enforcement. May be nil in
	// unit tests that do not exercise cluster lookups.
	Client client.Client

	// CNIStore exposes startup CNI detection results for fail-closed hard
	// tenancy admission.
	CNIStore *cnidetect.Store
}

var _ admission.Validator[*sandboxv1alpha1.ImpSandbox] = &ImpSandboxWebhook{}

// ValidateCreate validates a new ImpSandbox.
func (w *ImpSandboxWebhook) ValidateCreate(ctx context.Context, sb *sandboxv1alpha1.ImpSandbox) (admission.Warnings, error) {
	return nil, w.validate(ctx, sb).ToAggregate()
}

// ValidateUpdate validates an updated ImpSandbox.
func (w *ImpSandboxWebhook) ValidateUpdate(ctx context.Context, oldSB, newSB *sandboxv1alpha1.ImpSandbox) (admission.Warnings, error) {
	errs := w.validate(ctx, newSB)
	if oldSB.Spec.Tenancy != newSB.Spec.Tenancy && effectiveTenancy(oldSB) == sandboxv1alpha1.TenancyHard {
		errs = append(errs, field.Forbidden(
			field.NewPath("spec", "tenancy"),
			"tenancy cannot be downgraded from hard",
		))
	}
	return nil, errs.ToAggregate()
}

// ValidateDelete allows all deletes.
func (w *ImpSandboxWebhook) ValidateDelete(_ context.Context, _ *sandboxv1alpha1.ImpSandbox) (admission.Warnings, error) {
	return nil, nil
}

func (w *ImpSandboxWebhook) validate(ctx context.Context, sb *sandboxv1alpha1.ImpSandbox) field.ErrorList {
	var errs field.ErrorList

	hasTemplate := sb.Spec.TemplateRef != nil
	hasClass := sb.Spec.ClassRef != nil
	switch {
	case hasTemplate && hasClass:
		errs = append(errs, field.Invalid(
			field.NewPath("spec", "classRef"),
			sb.Spec.ClassRef,
			"classRef and templateRef are mutually exclusive",
		))
	case !hasTemplate && !hasClass:
		errs = append(errs, field.Required(
			field.NewPath("spec", "classRef"),
			"exactly one of classRef or templateRef must be set",
		))
	case hasClass && !hasTemplate && sb.Spec.Image == "":
		errs = append(errs, field.Required(
			field.NewPath("spec", "image"),
			"image is required when classRef is set without templateRef",
		))
	}

	if sb.Spec.ExpireAfter != nil {
		d := sb.Spec.ExpireAfter.Duration
		if d < 0 || (d > 0 && d < 60*time.Second) {
			errs = append(errs, field.Invalid(
				field.NewPath("spec", "expireAfter"),
				d.String(),
				"expireAfter must be 0 (disabled) or at least 60s",
			))
		}
	}

	if err := w.validateTenancyAgainstFloor(ctx, sb); err != nil {
		errs = append(errs, field.Forbidden(
			field.NewPath("spec", "tenancy"), err.Error(),
		))
	}

	return errs
}

// validateTenancyAgainstFloor refuses requests below the configured cluster
// floor and fails closed when hard is requested without Cilium capability.
func (w *ImpSandboxWebhook) validateTenancyAgainstFloor(ctx context.Context, sb *sandboxv1alpha1.ImpSandbox) error {
	if w.Client == nil {
		return nil
	}

	requested := effectiveTenancy(sb)

	floor := sandboxv1alpha1.TenancyStandard
	cfg := &impv1alpha1.ClusterImpConfig{}
	if err := w.Client.Get(ctx, types.NamespacedName{Name: "cluster"}, cfg); err == nil &&
		cfg.Spec.Sandbox != nil && cfg.Spec.Sandbox.FloorTenancy != "" {
		floor = sandboxv1alpha1.TenancyMode(cfg.Spec.Sandbox.FloorTenancy)
	}
	if rank(floor) > rank(requested) {
		return &floorError{floor}
	}

	if requested == sandboxv1alpha1.TenancyHard && !ciliumCapable(w.CNIStore) {
		return &capabilityError{}
	}
	return nil
}

func ciliumCapable(store *cnidetect.Store) bool {
	if store == nil {
		return false
	}
	result, ok := store.Result()
	if !ok {
		return false
	}
	return result.Provider == cnidetect.ProviderCilium || result.Provider == cnidetect.ProviderCiliumKubeProxyFree
}

// effectiveTenancy applies the CRD default when unset so validation and the
// reconciler agree on what was requested.
func effectiveTenancy(sb *sandboxv1alpha1.ImpSandbox) sandboxv1alpha1.TenancyMode {
	if sb.Spec.Tenancy == "" {
		return sandboxv1alpha1.TenancyStandard
	}
	return sb.Spec.Tenancy
}

func rank(t sandboxv1alpha1.TenancyMode) int {
	if t == sandboxv1alpha1.TenancyHard {
		return 2
	}
	return 1
}

type floorError struct{ floor sandboxv1alpha1.TenancyMode }

func (e *floorError) Error() string {
	return "spec.tenancy is below the cluster floor \"" + string(e.floor) +
		"\" set in ClusterImpConfig.sandbox.floorTenancy"
}

type capabilityError struct{}

func (e *capabilityError) Error() string {
	return "hard tenancy requires Cilium; no Cilium CRDs detected (fail closed)"
}
