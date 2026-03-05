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
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

// newVM builds a minimal ImpVM for use in tests.
// Pass empty string for templateRef, classRef, or image to leave them unset.
func newVM(templateRef, classRef, image string) *impdevv1alpha1.ImpVM {
	vm := &impdevv1alpha1.ImpVM{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-vm",
			Namespace: "default",
		},
	}
	if templateRef != "" {
		vm.Spec.TemplateRef = &impdevv1alpha1.LocalObjectRef{Name: templateRef}
	}
	if classRef != "" {
		vm.Spec.ClassRef = &impdevv1alpha1.ClusterObjectRef{Name: classRef}
	}
	vm.Spec.Image = image
	return vm
}

// --- Defaulter tests -------------------------------------------------------

func TestImpVMWebhook_Default_SetsLifecycle(t *testing.T) {
	wh := &ImpVMWebhook{}
	vm := newVM("", "my-class", "my-image")
	// lifecycle is empty

	if err := wh.Default(context.Background(), vm); err != nil {
		t.Fatalf("Default() returned unexpected error: %v", err)
	}
	if vm.Spec.Lifecycle != impdevv1alpha1.VMLifecycleEphemeral {
		t.Errorf("expected lifecycle=%q, got %q", impdevv1alpha1.VMLifecycleEphemeral, vm.Spec.Lifecycle)
	}
}

func TestImpVMWebhook_Default_PreservesExistingLifecycle(t *testing.T) {
	wh := &ImpVMWebhook{}
	vm := newVM("", "my-class", "my-image")
	vm.Spec.Lifecycle = impdevv1alpha1.VMLifecyclePersistent

	if err := wh.Default(context.Background(), vm); err != nil {
		t.Fatalf("Default() returned unexpected error: %v", err)
	}
	if vm.Spec.Lifecycle != impdevv1alpha1.VMLifecyclePersistent {
		t.Errorf("expected lifecycle=%q, got %q", impdevv1alpha1.VMLifecyclePersistent, vm.Spec.Lifecycle)
	}
}

// --- mergeRestartPolicy tests ----------------------------------------------

func TestImpVMWebhook_mergeRestartPolicy_ClassInherited(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := impdevv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("scheme: %v", err)
	}

	cls := &impdevv1alpha1.ImpVMClass{}
	cls.Name = "small"
	cls.Spec.RestartPolicy = &impdevv1alpha1.RestartPolicy{Mode: "in-place"}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cls).Build()
	wh := &ImpVMWebhook{Client: c}

	vm := newVM("", "small", "my-image")
	// RestartPolicy is nil on the VM — should be inherited from class.

	if err := wh.Default(context.Background(), vm); err != nil {
		t.Fatalf("Default() returned unexpected error: %v", err)
	}
	if vm.Spec.RestartPolicy == nil {
		t.Fatal("expected RestartPolicy to be stamped from class, got nil")
	}
	if vm.Spec.RestartPolicy.Mode != "in-place" {
		t.Errorf("expected RestartPolicy.Mode=%q, got %q", "in-place", vm.Spec.RestartPolicy.Mode)
	}
}

func TestImpVMWebhook_mergeRestartPolicy_VMOverridesClass(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := impdevv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("scheme: %v", err)
	}

	cls := &impdevv1alpha1.ImpVMClass{}
	cls.Name = "small"
	cls.Spec.RestartPolicy = &impdevv1alpha1.RestartPolicy{Mode: "in-place"}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cls).Build()
	wh := &ImpVMWebhook{Client: c}

	vm := newVM("", "small", "my-image")
	// VM explicitly sets a different policy — it should win.
	vm.Spec.RestartPolicy = &impdevv1alpha1.RestartPolicy{Mode: "reschedule"}

	if err := wh.Default(context.Background(), vm); err != nil {
		t.Fatalf("Default() returned unexpected error: %v", err)
	}
	if vm.Spec.RestartPolicy == nil {
		t.Fatal("expected RestartPolicy to remain set, got nil")
	}
	if vm.Spec.RestartPolicy.Mode != "reschedule" {
		t.Errorf("expected RestartPolicy.Mode=%q (VM override), got %q", "reschedule", vm.Spec.RestartPolicy.Mode)
	}
}

// --- ValidateCreate tests --------------------------------------------------

func TestImpVMWebhook_ValidateCreate_BothRefs(t *testing.T) {
	wh := &ImpVMWebhook{}
	vm := newVM("my-template", "my-class", "my-image")

	_, err := wh.ValidateCreate(context.Background(), vm)
	if err == nil {
		t.Fatal("expected error for both templateRef and classRef set, got nil")
	}
}

func TestImpVMWebhook_ValidateCreate_NoRefs(t *testing.T) {
	wh := &ImpVMWebhook{}
	vm := newVM("", "", "")

	_, err := wh.ValidateCreate(context.Background(), vm)
	if err == nil {
		t.Fatal("expected error for neither templateRef nor classRef set, got nil")
	}
}

func TestImpVMWebhook_ValidateCreate_ClassRefWithoutImage(t *testing.T) {
	wh := &ImpVMWebhook{}
	vm := newVM("", "my-class", "") // classRef set, image empty

	_, err := wh.ValidateCreate(context.Background(), vm)
	if err == nil {
		t.Fatal("expected error when classRef is set without image, got nil")
	}
}

func TestImpVMWebhook_ValidateCreate_Valid_ClassRef(t *testing.T) {
	wh := &ImpVMWebhook{}
	vm := newVM("", "my-class", "my-image")

	_, err := wh.ValidateCreate(context.Background(), vm)
	if err != nil {
		t.Errorf("expected no error for valid classRef+image, got: %v", err)
	}
}

func TestImpVMWebhook_ValidateCreate_Valid_TemplateRef(t *testing.T) {
	wh := &ImpVMWebhook{}
	vm := newVM("my-template", "", "") // templateRef only, no image required

	_, err := wh.ValidateCreate(context.Background(), vm)
	if err != nil {
		t.Errorf("expected no error for valid templateRef, got: %v", err)
	}
}

// --- ValidateUpdate tests --------------------------------------------------

func TestImpVMWebhook_ValidateUpdate_NodeNameImmutable(t *testing.T) {
	wh := &ImpVMWebhook{}
	oldVM := newVM("my-template", "", "")
	oldVM.Spec.NodeName = "node-1"
	newVM := newVM("my-template", "", "")
	newVM.Spec.NodeName = "node-2"

	_, err := wh.ValidateUpdate(context.Background(), oldVM, newVM)
	if err == nil {
		t.Fatal("expected error when nodeName is changed after being set, got nil")
	}
}

func TestImpVMWebhook_ValidateUpdate_NodeNameCanBeSetFromEmpty(t *testing.T) {
	wh := &ImpVMWebhook{}
	oldVM := newVM("my-template", "", "")
	oldVM.Spec.NodeName = ""
	newVM := newVM("my-template", "", "")
	newVM.Spec.NodeName = "node-1"

	_, err := wh.ValidateUpdate(context.Background(), oldVM, newVM)
	if err != nil {
		t.Errorf("expected no error when setting nodeName from empty, got: %v", err)
	}
}

func TestImpVMWebhook_ValidateUpdate_NodeNameUnchanged(t *testing.T) {
	wh := &ImpVMWebhook{}
	oldVM := newVM("my-template", "", "")
	oldVM.Spec.NodeName = "node-1"
	newVM := newVM("my-template", "", "")
	newVM.Spec.NodeName = "node-1"

	_, err := wh.ValidateUpdate(context.Background(), oldVM, newVM)
	if err != nil {
		t.Errorf("expected no error when nodeName is unchanged, got: %v", err)
	}
}

func TestImpVMWebhook_ValidateUpdate_NodeNameCannotBeCleared(t *testing.T) {
	wh := &ImpVMWebhook{}
	old := newVM("my-template", "", "")
	old.Spec.NodeName = "node-1"

	updated := newVM("my-template", "", "")
	updated.Spec.NodeName = "" // attempt to clear — should be rejected

	_, err := wh.ValidateUpdate(context.Background(), old, updated)
	if err == nil {
		t.Fatal("expected error when clearing a set nodeName")
	}
}

// --- ValidateDelete tests --------------------------------------------------

func TestImpVMWebhook_ValidateDelete(t *testing.T) {
	wh := &ImpVMWebhook{}
	vm := newVM("my-template", "", "")

	_, err := wh.ValidateDelete(context.Background(), vm)
	if err != nil {
		t.Errorf("expected no error on delete, got: %v", err)
	}
}
