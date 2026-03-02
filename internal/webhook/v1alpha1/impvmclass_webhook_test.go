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

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

// newClass builds a minimal ImpVMClass for use in tests.
func newClass(vcpu, memMiB, diskGiB int32) *impdevv1alpha1.ImpVMClass {
	return &impdevv1alpha1.ImpVMClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-class",
		},
		Spec: impdevv1alpha1.ImpVMClassSpec{
			VCPU:      vcpu,
			MemoryMiB: memMiB,
			DiskGiB:   diskGiB,
		},
	}
}

func TestImpVMClassWebhook_ValidateCreate(t *testing.T) {
	wh := &ImpVMClassWebhook{}
	cls := newClass(2, 256, 10)

	_, err := wh.ValidateCreate(context.Background(), cls)
	if err != nil {
		t.Errorf("expected no error for valid class, got: %v", err)
	}
}

func TestImpVMClassWebhook_ValidateUpdate(t *testing.T) {
	wh := &ImpVMClassWebhook{}
	oldCls := newClass(2, 256, 10)
	newCls := newClass(4, 512, 20)

	_, err := wh.ValidateUpdate(context.Background(), oldCls, newCls)
	if err != nil {
		t.Errorf("expected no error on update, got: %v", err)
	}
}

func TestImpVMClassWebhook_ValidateDelete(t *testing.T) {
	wh := &ImpVMClassWebhook{}
	cls := newClass(2, 256, 10)

	_, err := wh.ValidateDelete(context.Background(), cls)
	if err != nil {
		t.Errorf("expected no error on delete, got: %v", err)
	}
}
