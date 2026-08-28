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

package sandboxv1alpha1

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	sandboxv1alpha1 "github.com/syscode-labs/imp/api/sandbox/v1alpha1"
	impv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
	"github.com/syscode-labs/imp/internal/cnidetect"
)

func newTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(s))
	require.NoError(t, impv1alpha1.AddToScheme(s))
	require.NoError(t, sandboxv1alpha1.AddToScheme(s))
	return s
}

func validSandbox() *sandboxv1alpha1.ImpSandbox {
	return &sandboxv1alpha1.ImpSandbox{
		ObjectMeta: metav1.ObjectMeta{Name: "sb", Namespace: "team-a"},
		Spec: sandboxv1alpha1.ImpSandboxSpec{
			ClassRef: &impv1alpha1.ClusterObjectRef{Name: "small"},
			Image:    "alpine:3",
			Tenancy:  sandboxv1alpha1.TenancyStandard,
		},
	}
}

func ciliumStore(provider cnidetect.Provider) *cnidetect.Store {
	s := &cnidetect.Store{}
	s.Set(cnidetect.Result{Provider: provider})
	return s
}

func TestValidateCreate_acceptsValidStandardSandbox(t *testing.T) {
	w := &ImpSandboxWebhook{Client: fake.NewClientBuilder().WithScheme(newTestScheme(t)).Build()}
	warnings, err := w.ValidateCreate(context.Background(), validSandbox())
	assert.Empty(t, warnings)
	assert.NoError(t, err)
}

func TestValidateCreate_rejectsTemplateAndClassTogether(t *testing.T) {
	sb := validSandbox()
	sb.Spec.TemplateRef = &impv1alpha1.LocalObjectRef{Name: "tpl"}
	w := &ImpSandboxWebhook{}
	_, err := w.ValidateCreate(context.Background(), sb)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mutually exclusive")
}

func TestValidateCreate_requiresRefOrImage(t *testing.T) {
	sb := validSandbox()
	sb.Spec.ClassRef = nil
	sb.Spec.Image = ""
	w := &ImpSandboxWebhook{}
	_, err := w.ValidateCreate(context.Background(), sb)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exactly one of classRef or templateRef")
}

func TestValidateCreate_imageRequiredWithClassRefOnly(t *testing.T) {
	sb := validSandbox()
	sb.Spec.Image = ""
	w := &ImpSandboxWebhook{}
	_, err := w.ValidateCreate(context.Background(), sb)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "image is required")
}

func TestValidateCreate_rejectsShortExpireAfter(t *testing.T) {
	sb := validSandbox()
	sb.Spec.ExpireAfter = &metav1.Duration{Duration: 30 * time.Second}
	w := &ImpSandboxWebhook{}
	_, err := w.ValidateCreate(context.Background(), sb)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least 60s")
}

func TestValidateCreate_hardWithoutCiliumFailsClosed(t *testing.T) {
	cases := []struct {
		name    string
		store   *cnidetect.Store
		wantErr bool
	}{
		{"no store", nil, true},
		{"empty store", &cnidetect.Store{}, true},
		{"flannel", ciliumStore(cnidetect.ProviderFlannel), true},
		{"cilium", ciliumStore(cnidetect.ProviderCilium), false},
		{"cilium-kubeproxy-free", ciliumStore(cnidetect.ProviderCiliumKubeProxyFree), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sb := validSandbox()
			sb.Spec.Tenancy = sandboxv1alpha1.TenancyHard
			w := &ImpSandboxWebhook{Client: fake.NewClientBuilder().WithScheme(newTestScheme(t)).Build(), CNIStore: tc.store}
			_, err := w.ValidateCreate(context.Background(), sb)
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "requires Cilium")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateCreate_floorRejectsStandardWhenHardRequired(t *testing.T) {
	cfg := &impv1alpha1.ClusterImpConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
		Spec: impv1alpha1.ClusterImpConfigSpec{
			Sandbox: &impv1alpha1.SandboxConfig{FloorTenancy: "hard"},
		},
	}
	w := &ImpSandboxWebhook{
		Client:   fake.NewClientBuilder().WithScheme(newTestScheme(t)).WithObjects(cfg).Build(),
		CNIStore: ciliumStore(cnidetect.ProviderCilium),
	}
	_, err := w.ValidateCreate(context.Background(), validSandbox())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cluster floor")

	hard := validSandbox()
	hard.Spec.Tenancy = sandboxv1alpha1.TenancyHard
	_, err = w.ValidateCreate(context.Background(), hard)
	assert.NoError(t, err)
}

func TestValidateUpdate_cannotDowngradeFromHard(t *testing.T) {
	oldSB := validSandbox()
	oldSB.Spec.Tenancy = sandboxv1alpha1.TenancyHard

	newSB := oldSB.DeepCopy()
	newSB.Spec.Tenancy = sandboxv1alpha1.TenancyStandard

	w := &ImpSandboxWebhook{CNIStore: ciliumStore(cnidetect.ProviderCilium)}
	_, err := w.ValidateUpdate(context.Background(), oldSB, newSB)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "downgraded from hard")
}

func TestValidateDelete_allowsAll(t *testing.T) {
	w := &ImpSandboxWebhook{}
	warnings, err := w.ValidateDelete(context.Background(), validSandbox())
	assert.Empty(t, warnings)
	assert.NoError(t, err)
}

var _ admission.Validator[*sandboxv1alpha1.ImpSandbox] = (*ImpSandboxWebhook)(nil)
