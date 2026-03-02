# Admission Webhooks Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add validation and defaulting admission webhooks for `ImpVM`, `ImpVMClass`, and `ImpVMTemplate` and wire them into the operator binary.

**Architecture:** Each webhook is a struct in `internal/webhook/v1alpha1/` implementing controller-runtime's `CustomDefaulter` and/or `CustomValidator` interfaces. Tests are pure Go unit tests that call the webhook methods directly — no TLS server or envtest setup required. The webhooks are registered in `cmd/operator/main.go` via `ctrl.NewWebhookManagedBy`. The webhook server's TLS config is left at the controller-runtime default (reads certs from `/tmp/k8s-webhook-server/serving-certs/` by convention; cert-manager mounts there in production).

**Tech Stack:** `sigs.k8s.io/controller-runtime`, `k8s.io/apimachinery/pkg/util/validation/field`, `k8s.io/apimachinery/pkg/runtime`, standard `testing` package (no Ginkgo for webhook unit tests).

---

## Checklist before starting

```bash
cd /Users/giovanni/syscode/git/imp
pwd  # must end in /imp
go build ./...

# Confirm CustomDefaulter and CustomValidator interface signatures:
go doc sigs.k8s.io/controller-runtime/pkg/webhook CustomDefaulter
go doc sigs.k8s.io/controller-runtime/pkg/webhook CustomValidator

# Confirm webhook builder API:
go doc sigs.k8s.io/controller-runtime/pkg/builder WebhookManagedBy 2>/dev/null || \
  go doc sigs.k8s.io/controller-runtime NewWebhookManagedBy 2>/dev/null || \
  grep -r "WebhookManagedBy\|NewWebhookManagedBy" $(go env GOMODCACHE)/sigs.k8s.io/controller-runtime@v0.23.1/ --include="*.go" -l | head -5
```

> **SDK note:** Controller-runtime v0.23 defines `CustomDefaulter` and `CustomValidator` in
> `sigs.k8s.io/controller-runtime/pkg/webhook`. `admission.Warnings` is `[]string` from
> `sigs.k8s.io/controller-runtime/pkg/webhook/admission`. Adapt any interface signatures
> to match what `go doc` returns — do not guess.

---

## Context for the implementer

**Relevant existing types** (`api/v1alpha1/`):

```go
// ImpVMSpec (impvm_types.go)
type ImpVMSpec struct {
    TemplateRef *LocalObjectRef   `json:"templateRef,omitempty"`  // optional
    ClassRef    *ClusterObjectRef `json:"classRef,omitempty"`     // optional
    Image       string            `json:"image,omitempty"`
    Lifecycle   VMLifecycle       `json:"lifecycle,omitempty"`    // default=ephemeral
    NodeName    string            `json:"nodeName,omitempty"`
    // ... other fields omitted
}

// ImpVMTemplateSpec (impvmtemplate_types.go)
type ImpVMTemplateSpec struct {
    ClassRef   ClusterObjectRef `json:"classRef"` // NOT a pointer — inline required struct
    Image      string           `json:"image,omitempty"`
    // ...
}

// ImpVMClassSpec (impvmclass_types.go)
type ImpVMClassSpec struct {
    VCPU      int32 `json:"vcpu"`       // min=1 via marker
    MemoryMiB int32 `json:"memoryMiB"` // min=128 via marker
    DiskGiB   int32 `json:"diskGiB"`   // min=1 via marker
    Arch      Arch  `json:"arch,omitempty"` // default=multi
    // ...
}

const (
    VMLifecycleEphemeral  VMLifecycle = "ephemeral"
    VMLifecyclePersistent VMLifecycle = "persistent"
)
```

**Webhook logic rules:**

*ImpVM defaulter:*
- If `spec.lifecycle == ""` → set to `VMLifecycleEphemeral`

*ImpVM validator (create and update):*
1. Both `TemplateRef` and `ClassRef` set → `field.Invalid` on `spec.classRef`
2. Neither `TemplateRef` nor `ClassRef` set → `field.Required` on `spec.classRef`
3. `ClassRef` set, `TemplateRef` nil, `Image == ""` → `field.Required` on `spec.image`

*ImpVM validator (update only):*
4. `oldVM.Spec.NodeName != ""` and `newVM.Spec.NodeName != oldVM.Spec.NodeName` → `field.Forbidden` on `spec.nodeName`

*ImpVMClass validator:*
- No cross-field validation beyond what markers enforce. Returns nil on create/update/delete.

*ImpVMTemplate validator:*
- `spec.classRef.name == ""` → `field.Required` on `spec.classRef.name`

**Import alias conventions** (matching rest of codebase):
```go
impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
```

**Import grouping** (golangci-lint enforces 3 groups):
```
stdlib

external k8s/controller-runtime packages

github.com/syscode-labs/imp/...
```

---

## Task 1: `ImpVM` webhook — defaulter + validator

**Files:**
- Create: `internal/webhook/v1alpha1/impvm_webhook.go`
- Create: `internal/webhook/v1alpha1/impvm_webhook_test.go`

### Step 1: Create the webhook implementation

```go
// internal/webhook/v1alpha1/impvm_webhook.go
package v1alpha1

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

// ImpVMWebhook implements defaulting and validation for ImpVM objects.
type ImpVMWebhook struct{}

// Default implements webhook.CustomDefaulter.
// Sets spec.lifecycle to "ephemeral" if not specified.
func (w *ImpVMWebhook) Default(_ context.Context, obj runtime.Object) error {
	vm, ok := obj.(*impdevv1alpha1.ImpVM)
	if !ok {
		return fmt.Errorf("expected an ImpVM, got %T", obj)
	}
	if vm.Spec.Lifecycle == "" {
		vm.Spec.Lifecycle = impdevv1alpha1.VMLifecycleEphemeral
	}
	return nil
}

// ValidateCreate implements webhook.CustomValidator.
func (w *ImpVMWebhook) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	vm, ok := obj.(*impdevv1alpha1.ImpVM)
	if !ok {
		return nil, fmt.Errorf("expected an ImpVM, got %T", obj)
	}
	return nil, validateImpVM(vm).ToAggregate()
}

// ValidateUpdate implements webhook.CustomValidator.
func (w *ImpVMWebhook) ValidateUpdate(_ context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	oldVM, ok := oldObj.(*impdevv1alpha1.ImpVM)
	if !ok {
		return nil, fmt.Errorf("expected an ImpVM (old), got %T", oldObj)
	}
	newVM, ok := newObj.(*impdevv1alpha1.ImpVM)
	if !ok {
		return nil, fmt.Errorf("expected an ImpVM (new), got %T", newObj)
	}

	errs := validateImpVM(newVM)

	// nodeName is immutable once set by the operator scheduler.
	if oldVM.Spec.NodeName != "" && newVM.Spec.NodeName != oldVM.Spec.NodeName {
		errs = append(errs, field.Forbidden(
			field.NewPath("spec", "nodeName"),
			"nodeName is immutable once set",
		))
	}

	return nil, errs.ToAggregate()
}

// ValidateDelete implements webhook.CustomValidator.
func (w *ImpVMWebhook) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

// validateImpVM returns field errors for invariants checked on both create and update.
func validateImpVM(vm *impdevv1alpha1.ImpVM) field.ErrorList {
	var errs field.ErrorList
	spec := field.NewPath("spec")

	hasTemplate := vm.Spec.TemplateRef != nil
	hasClass := vm.Spec.ClassRef != nil

	if hasTemplate && hasClass {
		errs = append(errs, field.Invalid(
			spec.Child("classRef"), vm.Spec.ClassRef.Name,
			"classRef and templateRef are mutually exclusive",
		))
	}
	if !hasTemplate && !hasClass {
		errs = append(errs, field.Required(
			spec.Child("classRef"),
			"one of classRef or templateRef is required",
		))
	}
	// image is required when classRef is used directly (template would supply it otherwise).
	if hasClass && !hasTemplate && vm.Spec.Image == "" {
		errs = append(errs, field.Required(
			spec.Child("image"),
			"image is required when classRef is set without templateRef",
		))
	}

	return errs
}
```

> **Adapter note:** After running `go doc sigs.k8s.io/controller-runtime/pkg/webhook CustomDefaulter`,
> if the `Default` signature differs (e.g. takes no context), adapt accordingly.
> Same for `CustomValidator.ValidateCreate` — check the exact signature before writing.

### Step 2: Verify it compiles

```bash
cd /Users/giovanni/syscode/git/imp && go build ./internal/webhook/...
```

Expected: no output. If the `admission.Warnings` type path differs, check:
```bash
go doc sigs.k8s.io/controller-runtime/pkg/webhook/admission Warnings
```

### Step 3: Write the unit tests

```go
// internal/webhook/v1alpha1/impvm_webhook_test.go
package v1alpha1

import (
	"context"
	"testing"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

func newVM(templateRef, classRef, image string) *impdevv1alpha1.ImpVM {
	vm := &impdevv1alpha1.ImpVM{}
	vm.Namespace = "default"
	vm.Name = "test"
	if templateRef != "" {
		vm.Spec.TemplateRef = &impdevv1alpha1.LocalObjectRef{Name: templateRef}
	}
	if classRef != "" {
		vm.Spec.ClassRef = &impdevv1alpha1.ClusterObjectRef{Name: classRef}
	}
	vm.Spec.Image = image
	return vm
}

// --- Defaulter ---

func TestImpVMWebhook_Default_SetsLifecycle(t *testing.T) {
	wh := &ImpVMWebhook{}
	vm := newVM("", "small", "ghcr.io/org/runner:latest")

	if err := wh.Default(context.Background(), vm); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if vm.Spec.Lifecycle != impdevv1alpha1.VMLifecycleEphemeral {
		t.Errorf("Lifecycle = %q, want %q", vm.Spec.Lifecycle, impdevv1alpha1.VMLifecycleEphemeral)
	}
}

func TestImpVMWebhook_Default_PreservesExistingLifecycle(t *testing.T) {
	wh := &ImpVMWebhook{}
	vm := newVM("", "small", "ghcr.io/org/runner:latest")
	vm.Spec.Lifecycle = impdevv1alpha1.VMLifecyclePersistent

	if err := wh.Default(context.Background(), vm); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if vm.Spec.Lifecycle != impdevv1alpha1.VMLifecyclePersistent {
		t.Errorf("Lifecycle = %q, want persistent", vm.Spec.Lifecycle)
	}
}

// --- ValidateCreate ---

func TestImpVMWebhook_ValidateCreate_BothRefs(t *testing.T) {
	wh := &ImpVMWebhook{}
	vm := newVM("my-template", "small", "ghcr.io/org/runner:latest")

	_, err := wh.ValidateCreate(context.Background(), vm)
	if err == nil {
		t.Fatal("expected error when both templateRef and classRef are set")
	}
}

func TestImpVMWebhook_ValidateCreate_NoRefs(t *testing.T) {
	wh := &ImpVMWebhook{}
	vm := newVM("", "", "ghcr.io/org/runner:latest")

	_, err := wh.ValidateCreate(context.Background(), vm)
	if err == nil {
		t.Fatal("expected error when neither templateRef nor classRef is set")
	}
}

func TestImpVMWebhook_ValidateCreate_ClassRefWithoutImage(t *testing.T) {
	wh := &ImpVMWebhook{}
	vm := newVM("", "small", "") // classRef set, image empty

	_, err := wh.ValidateCreate(context.Background(), vm)
	if err == nil {
		t.Fatal("expected error when classRef is set without image")
	}
}

func TestImpVMWebhook_ValidateCreate_Valid_ClassRef(t *testing.T) {
	wh := &ImpVMWebhook{}
	vm := newVM("", "small", "ghcr.io/org/runner:latest")

	_, err := wh.ValidateCreate(context.Background(), vm)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestImpVMWebhook_ValidateCreate_Valid_TemplateRef(t *testing.T) {
	wh := &ImpVMWebhook{}
	vm := newVM("my-template", "", "") // template provides class + image

	_, err := wh.ValidateCreate(context.Background(), vm)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// --- ValidateUpdate ---

func TestImpVMWebhook_ValidateUpdate_NodeNameImmutable(t *testing.T) {
	wh := &ImpVMWebhook{}
	old := newVM("", "small", "ghcr.io/org/runner:latest")
	old.Spec.NodeName = "node-1"

	updated := newVM("", "small", "ghcr.io/org/runner:latest")
	updated.Spec.NodeName = "node-2" // changed!

	_, err := wh.ValidateUpdate(context.Background(), old, updated)
	if err == nil {
		t.Fatal("expected error when nodeName is changed after being set")
	}
}

func TestImpVMWebhook_ValidateUpdate_NodeNameCanBeSetFromEmpty(t *testing.T) {
	wh := &ImpVMWebhook{}
	old := newVM("", "small", "ghcr.io/org/runner:latest")
	// old.Spec.NodeName == "" (not set yet)

	updated := newVM("", "small", "ghcr.io/org/runner:latest")
	updated.Spec.NodeName = "node-1" // first assignment by scheduler — allowed

	_, err := wh.ValidateUpdate(context.Background(), old, updated)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestImpVMWebhook_ValidateUpdate_NodeNameUnchanged(t *testing.T) {
	wh := &ImpVMWebhook{}
	old := newVM("", "small", "ghcr.io/org/runner:latest")
	old.Spec.NodeName = "node-1"

	updated := newVM("", "small", "ghcr.io/org/runner:latest")
	updated.Spec.NodeName = "node-1" // same — allowed

	_, err := wh.ValidateUpdate(context.Background(), old, updated)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// --- ValidateDelete ---

func TestImpVMWebhook_ValidateDelete(t *testing.T) {
	wh := &ImpVMWebhook{}
	vm := newVM("", "small", "ghcr.io/org/runner:latest")

	_, err := wh.ValidateDelete(context.Background(), vm)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}
```

### Step 4: Run the tests

```bash
cd /Users/giovanni/syscode/git/imp && go test ./internal/webhook/... -v -count=1
```

Expected: all 11 tests PASS.

### Step 5: Run linter

```bash
cd /Users/giovanni/syscode/git/imp && golangci-lint run ./internal/webhook/...
```

Expected: 0 issues.

### Step 6: Commit

```bash
cd /Users/giovanni/syscode/git/imp
git add internal/webhook/v1alpha1/impvm_webhook.go internal/webhook/v1alpha1/impvm_webhook_test.go
git commit -m "feat(webhook): ImpVM defaulter + validator"
```

---

## Task 2: `ImpVMClass` webhook — minimal validator

**Files:**
- Create: `internal/webhook/v1alpha1/impvmclass_webhook.go`
- Create: `internal/webhook/v1alpha1/impvmclass_webhook_test.go`

### Step 1: Create the webhook

```go
// internal/webhook/v1alpha1/impvmclass_webhook.go
package v1alpha1

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

// ImpVMClassWebhook validates ImpVMClass objects.
// Numeric field minimums (VCPU≥1, MemoryMiB≥128, DiskGiB≥1) are enforced by
// CEL validation markers on the CRD schema; this webhook is a registration
// stub that will hold immutability checks in Phase 2.
type ImpVMClassWebhook struct{}

// ValidateCreate implements webhook.CustomValidator.
func (w *ImpVMClassWebhook) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	if _, ok := obj.(*impdevv1alpha1.ImpVMClass); !ok {
		return nil, fmt.Errorf("expected an ImpVMClass, got %T", obj)
	}
	return nil, nil
}

// ValidateUpdate implements webhook.CustomValidator.
func (w *ImpVMClassWebhook) ValidateUpdate(_ context.Context, _, newObj runtime.Object) (admission.Warnings, error) {
	if _, ok := newObj.(*impdevv1alpha1.ImpVMClass); !ok {
		return nil, fmt.Errorf("expected an ImpVMClass, got %T", newObj)
	}
	return nil, nil
}

// ValidateDelete implements webhook.CustomValidator.
func (w *ImpVMClassWebhook) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}
```

### Step 2: Write the tests

```go
// internal/webhook/v1alpha1/impvmclass_webhook_test.go
package v1alpha1

import (
	"context"
	"testing"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

func newClass(vcpu, memMiB, diskGiB int32) *impdevv1alpha1.ImpVMClass {
	c := &impdevv1alpha1.ImpVMClass{}
	c.Name = "small"
	c.Spec.VCPU = vcpu
	c.Spec.MemoryMiB = memMiB
	c.Spec.DiskGiB = diskGiB
	return c
}

func TestImpVMClassWebhook_ValidateCreate(t *testing.T) {
	wh := &ImpVMClassWebhook{}
	_, err := wh.ValidateCreate(context.Background(), newClass(2, 512, 10))
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestImpVMClassWebhook_ValidateUpdate(t *testing.T) {
	wh := &ImpVMClassWebhook{}
	old := newClass(2, 512, 10)
	updated := newClass(4, 1024, 20)
	_, err := wh.ValidateUpdate(context.Background(), old, updated)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestImpVMClassWebhook_ValidateDelete(t *testing.T) {
	wh := &ImpVMClassWebhook{}
	_, err := wh.ValidateDelete(context.Background(), newClass(2, 512, 10))
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}
```

### Step 3: Run tests + lint

```bash
cd /Users/giovanni/syscode/git/imp && go test ./internal/webhook/... -v -count=1
golangci-lint run ./internal/webhook/...
```

Expected: all tests PASS, 0 lint issues.

### Step 4: Commit

```bash
cd /Users/giovanni/syscode/git/imp
git add internal/webhook/v1alpha1/impvmclass_webhook.go internal/webhook/v1alpha1/impvmclass_webhook_test.go
git commit -m "feat(webhook): ImpVMClass validator stub"
```

---

## Task 3: `ImpVMTemplate` webhook — validator

**Files:**
- Create: `internal/webhook/v1alpha1/impvmtemplate_webhook.go`
- Create: `internal/webhook/v1alpha1/impvmtemplate_webhook_test.go`

### Step 1: Create the webhook

```go
// internal/webhook/v1alpha1/impvmtemplate_webhook.go
package v1alpha1

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

// ImpVMTemplateWebhook validates ImpVMTemplate objects.
type ImpVMTemplateWebhook struct{}

// ValidateCreate implements webhook.CustomValidator.
func (w *ImpVMTemplateWebhook) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	tmpl, ok := obj.(*impdevv1alpha1.ImpVMTemplate)
	if !ok {
		return nil, fmt.Errorf("expected an ImpVMTemplate, got %T", obj)
	}
	return nil, validateImpVMTemplate(tmpl).ToAggregate()
}

// ValidateUpdate implements webhook.CustomValidator.
func (w *ImpVMTemplateWebhook) ValidateUpdate(_ context.Context, _, newObj runtime.Object) (admission.Warnings, error) {
	tmpl, ok := newObj.(*impdevv1alpha1.ImpVMTemplate)
	if !ok {
		return nil, fmt.Errorf("expected an ImpVMTemplate, got %T", newObj)
	}
	return nil, validateImpVMTemplate(tmpl).ToAggregate()
}

// ValidateDelete implements webhook.CustomValidator.
func (w *ImpVMTemplateWebhook) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

func validateImpVMTemplate(tmpl *impdevv1alpha1.ImpVMTemplate) field.ErrorList {
	var errs field.ErrorList
	// ClassRef is a non-pointer inline struct; the kubebuilder required marker
	// does not guard against an empty name string.
	if tmpl.Spec.ClassRef.Name == "" {
		errs = append(errs, field.Required(
			field.NewPath("spec", "classRef", "name"),
			"classRef.name is required",
		))
	}
	return errs
}
```

### Step 2: Write the tests

```go
// internal/webhook/v1alpha1/impvmtemplate_webhook_test.go
package v1alpha1

import (
	"context"
	"testing"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

func newTemplate(classRefName string) *impdevv1alpha1.ImpVMTemplate {
	tmpl := &impdevv1alpha1.ImpVMTemplate{}
	tmpl.Namespace = "default"
	tmpl.Name = "test"
	tmpl.Spec.ClassRef.Name = classRefName
	return tmpl
}

func TestImpVMTemplateWebhook_ValidateCreate_Valid(t *testing.T) {
	wh := &ImpVMTemplateWebhook{}
	_, err := wh.ValidateCreate(context.Background(), newTemplate("small"))
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestImpVMTemplateWebhook_ValidateCreate_EmptyClassName(t *testing.T) {
	wh := &ImpVMTemplateWebhook{}
	_, err := wh.ValidateCreate(context.Background(), newTemplate(""))
	if err == nil {
		t.Fatal("expected error for empty classRef.name")
	}
}

func TestImpVMTemplateWebhook_ValidateUpdate_Valid(t *testing.T) {
	wh := &ImpVMTemplateWebhook{}
	old := newTemplate("small")
	updated := newTemplate("large")
	_, err := wh.ValidateUpdate(context.Background(), old, updated)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestImpVMTemplateWebhook_ValidateDelete(t *testing.T) {
	wh := &ImpVMTemplateWebhook{}
	_, err := wh.ValidateDelete(context.Background(), newTemplate("small"))
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}
```

### Step 3: Run tests + lint

```bash
cd /Users/giovanni/syscode/git/imp && go test ./internal/webhook/... -v -count=1
golangci-lint run ./internal/webhook/...
```

Expected: all tests PASS, 0 lint issues.

### Step 4: Commit

```bash
cd /Users/giovanni/syscode/git/imp
git add internal/webhook/v1alpha1/impvmtemplate_webhook.go internal/webhook/v1alpha1/impvmtemplate_webhook_test.go
git commit -m "feat(webhook): ImpVMTemplate validator"
```

---

## Task 4: Wire webhooks into `cmd/operator/main.go`

**Files:**
- Modify: `cmd/operator/main.go`

### Step 1: Read the current main.go

```bash
cat /Users/giovanni/syscode/git/imp/cmd/operator/main.go
```

### Step 2: Check the exact webhook builder API in this version

```bash
# Find the builder function name:
grep -r "func WebhookManagedBy\|func NewWebhookManagedBy" \
  $(go env GOMODCACHE)/sigs.k8s.io/controller-runtime@v0.23.1/ --include="*.go"
```

Expected: finds either `WebhookManagedBy` in `pkg/builder` or `NewWebhookManagedBy` somewhere.

In controller-runtime v0.23, the webhook registration API is:

```go
import "sigs.k8s.io/controller-runtime/pkg/builder"

if err := builder.WebhookManagedBy(mgr).
    For(&impv1alpha1.ImpVM{}).
    WithDefaulter(&webhookv1alpha1.ImpVMWebhook{}).
    WithValidator(&webhookv1alpha1.ImpVMWebhook{}).
    Complete(); err != nil {
    ...
}
```

**Adapt to whatever the `go doc` / grep returns.**

### Step 3: Add the import and webhook registrations to `main.go`

Add import:
```go
"sigs.k8s.io/controller-runtime/pkg/builder"

webhookv1alpha1 "github.com/syscode-labs/imp/internal/webhook/v1alpha1"
```

After the existing `ImpVMReconciler` registration block, add:

```go
// ImpVM webhook (defaulting + validation)
if err := builder.WebhookManagedBy(mgr).
    For(&impv1alpha1.ImpVM{}).
    WithDefaulter(&webhookv1alpha1.ImpVMWebhook{}).
    WithValidator(&webhookv1alpha1.ImpVMWebhook{}).
    Complete(); err != nil {
    setupLog.Error(err, "unable to register ImpVM webhook")
    os.Exit(1)
}

// ImpVMClass webhook (validation)
if err := builder.WebhookManagedBy(mgr).
    For(&impv1alpha1.ImpVMClass{}).
    WithValidator(&webhookv1alpha1.ImpVMClassWebhook{}).
    Complete(); err != nil {
    setupLog.Error(err, "unable to register ImpVMClass webhook")
    os.Exit(1)
}

// ImpVMTemplate webhook (validation)
if err := builder.WebhookManagedBy(mgr).
    For(&impv1alpha1.ImpVMTemplate{}).
    WithValidator(&webhookv1alpha1.ImpVMTemplateWebhook{}).
    Complete(); err != nil {
    setupLog.Error(err, "unable to register ImpVMTemplate webhook")
    os.Exit(1)
}
```

> **Note:** The manager's `WebhookServer` is created automatically by controller-runtime when
> webhook handlers are registered. It listens on `:9443` by default and reads TLS certs from
> `/tmp/k8s-webhook-server/serving-certs/`. No additional manager options are needed for now.

### Step 4: Verify compilation

```bash
cd /Users/giovanni/syscode/git/imp && go build ./cmd/operator/
```

Expected: no output. If `builder.WebhookManagedBy` doesn't exist or has a different signature, find the correct import:
```bash
go doc sigs.k8s.io/controller-runtime/pkg/builder | grep -i webhook
```

And adapt accordingly.

### Step 5: Run full test suite

```bash
KUBEBUILDER_ASSETS="/Users/giovanni/syscode/git/imp/bin/k8s/k8s/1.35.0-darwin-amd64" \
  go test ./... -count=1
```

Expected: all packages pass.

### Step 6: Run linter

```bash
golangci-lint run ./...
```

Expected: 0 issues.

### Step 7: Commit

```bash
cd /Users/giovanni/syscode/git/imp
git add cmd/operator/main.go
git commit -m "feat(operator): register ImpVM/ImpVMClass/ImpVMTemplate admission webhooks"
```

---

## Task 5: Final verification

### Step 1: Full test run

```bash
KUBEBUILDER_ASSETS="/Users/giovanni/syscode/git/imp/bin/k8s/k8s/1.35.0-darwin-amd64" \
  go test ./... -count=1 -v 2>&1 | tail -20
```

### Step 2: go mod tidy

```bash
cd /Users/giovanni/syscode/git/imp && go mod tidy
git diff go.mod go.sum
```

If changed:
```bash
git add go.mod go.sum && git commit -m "chore: go mod tidy"
```

### Step 3: Final lint

```bash
golangci-lint run ./...
```

Expected: 0 issues.

### Step 4: Show git log

```bash
git log --oneline -8
```

---

## Notes for the implementer

**`CustomDefaulter` / `CustomValidator` interface check:** The exact method signatures in
controller-runtime v0.23 may differ slightly from this plan (e.g. context param may be absent
in older versions, or return type may differ). Always run `go doc` first and adapt.

**`admission.Warnings`:** Defined as `type Warnings []string` in
`sigs.k8s.io/controller-runtime/pkg/webhook/admission`. If the import path is wrong, check:
```bash
grep -r "type Warnings" $(go env GOMODCACHE)/sigs.k8s.io/controller-runtime@v0.23.1/ --include="*.go"
```

**`field.ErrorList.ToAggregate()`:** Returns nil when the list is empty — safe to return directly
as the `error` in `ValidateCreate`/`ValidateUpdate`.

**Webhook server TLS:** In production, cert-manager populates
`/tmp/k8s-webhook-server/serving-certs/` via a volume mount. For local dev / e2e Kind tests,
either disable webhooks (`--disable-webhooks` flag not yet added — future work) or install
cert-manager in the Kind cluster. Unit tests in this plan don't need TLS.

**`builder.WebhookManagedBy` vs `ctrl.NewWebhookManagedBy`:** If grep shows the function lives
in `ctrl` package (ctrl alias = `sigs.k8s.io/controller-runtime`), use `ctrl.NewWebhookManagedBy`.
If it's in `builder` package, use `builder.WebhookManagedBy`. The plan uses `builder` — adapt if wrong.
