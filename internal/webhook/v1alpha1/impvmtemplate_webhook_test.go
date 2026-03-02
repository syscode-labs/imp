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

// newTemplate builds a minimal ImpVMTemplate for use in tests.
func newTemplate(classRefName string) *impdevv1alpha1.ImpVMTemplate {
	return &impdevv1alpha1.ImpVMTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-template",
			Namespace: "default",
		},
		Spec: impdevv1alpha1.ImpVMTemplateSpec{
			ClassRef: impdevv1alpha1.ClusterObjectRef{Name: classRefName},
		},
	}
}

func TestImpVMTemplateWebhook_ValidateCreate_Valid(t *testing.T) {
	wh := &ImpVMTemplateWebhook{}
	tmpl := newTemplate("small")

	_, err := wh.ValidateCreate(context.Background(), tmpl)
	if err != nil {
		t.Errorf("expected no error for valid template, got: %v", err)
	}
}

func TestImpVMTemplateWebhook_ValidateCreate_EmptyClassName(t *testing.T) {
	wh := &ImpVMTemplateWebhook{}
	tmpl := newTemplate("")

	_, err := wh.ValidateCreate(context.Background(), tmpl)
	if err == nil {
		t.Fatal("expected error when classRef.name is empty, got nil")
	}
}

func TestImpVMTemplateWebhook_ValidateUpdate_Valid(t *testing.T) {
	wh := &ImpVMTemplateWebhook{}
	oldTmpl := newTemplate("small")
	newTmpl := newTemplate("large")

	_, err := wh.ValidateUpdate(context.Background(), oldTmpl, newTmpl)
	if err != nil {
		t.Errorf("expected no error for valid update, got: %v", err)
	}
}

func TestImpVMTemplateWebhook_ValidateDelete(t *testing.T) {
	wh := &ImpVMTemplateWebhook{}
	tmpl := newTemplate("small")

	_, err := wh.ValidateDelete(context.Background(), tmpl)
	if err != nil {
		t.Errorf("expected no error on delete, got: %v", err)
	}
}
