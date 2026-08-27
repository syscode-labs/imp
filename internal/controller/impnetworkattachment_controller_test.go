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

package controller

import (
	"context"
	"testing"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/syscode-labs/imp/internal/agent/network"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

func attConfigDef(name string, vlanID int32, subjects ...string) *impdevv1alpha1.ClusterImpConfig {
	return &impdevv1alpha1.ClusterImpConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
		Spec: impdevv1alpha1.ClusterImpConfigSpec{
			Networking: impdevv1alpha1.NetworkingConfig{
				LANAttachments: []impdevv1alpha1.LANAttachmentSpec{{
					Name:            name,
					VLANID:          vlanID,
					SubnetCIDR:      "192.168.77.0/24",
					AllowDHCP:       true,
					AllowedSubjects: subjects,
				}},
			},
		},
	}
}

func attNodeProfile(node string) *impdevv1alpha1.ClusterImpNodeProfile {
	return &impdevv1alpha1.ClusterImpNodeProfile{
		ObjectMeta: metav1.ObjectMeta{Name: node},
		Spec: impdevv1alpha1.ClusterImpNodeProfileSpec{
			LANBindings: []impdevv1alpha1.NodeLANBinding{
				{AttachmentName: "lab-lan", ParentInterface: "br-lan"},
			},
		},
	}
}

func scheduledVM(name string) *impdevv1alpha1.ImpVM {
	return &impdevv1alpha1.ImpVM{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Status:     impdevv1alpha1.ImpVMStatus{NodeName: "node-a", Phase: impdevv1alpha1.VMPhaseRunning},
	}
}

func attRequest(vmName string) *impdevv1alpha1.ImpNetworkAttachment {
	att := &impdevv1alpha1.ImpNetworkAttachment{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "att-" + vmName,
			Namespace:   "default",
			Annotations: map[string]string{impdevv1alpha1.AnnotationRequester: "alice"},
		},
		Spec: impdevv1alpha1.ImpNetworkAttachmentSpec{
			VMRef:         impdevv1alpha1.LocalObjectRef{Name: vmName},
			AttachmentRef: "lab-lan",
			IP:            "192.168.77.20",
		},
	}
	return att
}

func authorizedCondition(att *impdevv1alpha1.ImpNetworkAttachment) *metav1.Condition {
	return apimeta.FindStatusCondition(att.Status.Conditions, impdevv1alpha1.ConditionAttachmentAuthorized)
}

func runReconcile(t *testing.T, r *ImpNetworkAttachmentReconciler, att *impdevv1alpha1.ImpNetworkAttachment) {
	t.Helper()
	req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(att)}
	if _, err := r.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
}

func TestImpNetworkAttachment_AuthorizesOnBoundNode(t *testing.T) {
	att := attRequest("tiny-vm")
	cfg := attConfigDef("lab-lan", 100)
	vm := scheduledVM("tiny-vm")
	profile := attNodeProfile("node-a")
	scheme := runtime.NewScheme()
	_ = impdevv1alpha1.AddToScheme(scheme)
	r := &ImpNetworkAttachmentReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).
			WithStatusSubresource(&impdevv1alpha1.ImpNetworkAttachment{}).
			WithObjects(att, cfg, vm, profile).Build(),
		Scheme: scheme,
	}

	runReconcile(t, r, att)

	got := &impdevv1alpha1.ImpNetworkAttachment{}
	if err := r.Get(context.Background(), client.ObjectKeyFromObject(att), got); err != nil {
		t.Fatalf("get attachment: %v", err)
	}
	if got.Status.Phase != impdevv1alpha1.AttachmentPhaseAuthorized {
		t.Errorf("phase = %q, want Authorized", got.Status.Phase)
	}
	cond := authorizedCondition(got)
	if cond == nil || cond.Status != metav1.ConditionTrue {
		t.Fatalf("Authorized condition missing or false: %+v", cond)
	}
	if got.Status.Node != "node-a" || got.Status.Requester != "alice" {
		t.Errorf("audit status incomplete: node=%q requester=%q", got.Status.Node, got.Status.Requester)
	}
	wantMAC := network.MACAddr("default/tiny-vm")
	if got.Status.MACAddress != wantMAC {
		t.Errorf("mac = %q, want %q", got.Status.MACAddress, wantMAC)
	}
	if got.Status.AssignedIP != "192.168.77.20" {
		t.Errorf("assignedIP = %q, want static ip recorded", got.Status.AssignedIP)
	}
}

func TestImpNetworkAttachment_DeniesMissingNodeBinding(t *testing.T) {
	att := attRequest("tiny-vm")
	cfg := attConfigDef("lab-lan", 100)
	vm := scheduledVM("tiny-vm")
	scheme := runtime.NewScheme()
	_ = impdevv1alpha1.AddToScheme(scheme)
	r := &ImpNetworkAttachmentReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).
			WithStatusSubresource(&impdevv1alpha1.ImpNetworkAttachment{}).
			WithObjects(att, cfg, vm).Build(), // profile without bindings
		Scheme: scheme,
	}

	runReconcile(t, r, att)

	got := &impdevv1alpha1.ImpNetworkAttachment{}
	_ = r.Get(context.Background(), client.ObjectKeyFromObject(att), got)
	if got.Status.Phase != impdevv1alpha1.AttachmentPhaseDenied {
		t.Fatalf("phase = %q, want Denied", got.Status.Phase)
	}
	if cond := authorizedCondition(got); cond == nil || cond.Reason != DenyReasonNodeBindingMiss {
		t.Errorf("reason = %+v, want %s", cond, DenyReasonNodeBindingMiss)
	}
}

func TestImpNetworkAttachment_DeniesSubjectNotInAllowlist(t *testing.T) {
	att := attRequest("tiny-vm") // requester alice
	cfg := attConfigDef("lab-lan", 100, "group:admins")
	vm := scheduledVM("tiny-vm")
	profile := attNodeProfile("node-a")
	scheme := runtime.NewScheme()
	_ = impdevv1alpha1.AddToScheme(scheme)
	r := &ImpNetworkAttachmentReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).
			WithStatusSubresource(&impdevv1alpha1.ImpNetworkAttachment{}).
			WithObjects(att, cfg, vm, profile).Build(),
		Scheme: scheme,
	}

	runReconcile(t, r, att)

	got := &impdevv1alpha1.ImpNetworkAttachment{}
	_ = r.Get(context.Background(), client.ObjectKeyFromObject(att), got)
	if cond := authorizedCondition(got); cond == nil || cond.Reason != DenyReasonSubjectNotAllowed {
		t.Errorf("reason = %+v, want %s", cond, DenyReasonSubjectNotAllowed)
	}
}

func TestImpNetworkAttachment_DowngradesWhenDefinitionDeleted(t *testing.T) {
	att := attRequest("tiny-vm")
	cfg := attConfigDef("lab-lan", 100)
	vm := scheduledVM("tiny-vm")
	profile := attNodeProfile("node-a")
	scheme := runtime.NewScheme()
	_ = impdevv1alpha1.AddToScheme(scheme)
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&impdevv1alpha1.ImpNetworkAttachment{}).
		WithObjects(att, cfg, vm, profile).Build()
	r := &ImpNetworkAttachmentReconciler{Client: c, Scheme: scheme}

	runReconcile(t, r, att)

	// Definition removed from the allowlist — next reconcile must deny.
	_ = c.Delete(context.Background(), cfg)
	runReconcile(t, r, att)

	got := &impdevv1alpha1.ImpNetworkAttachment{}
	_ = r.Get(context.Background(), client.ObjectKeyFromObject(att), got)
	if got.Status.Phase != impdevv1alpha1.AttachmentPhaseDenied {
		t.Fatalf("phase = %q, want Denied after definition deletion", got.Status.Phase)
	}
	if cond := authorizedCondition(got); cond == nil || cond.Reason != DenyReasonDefinitionMissing {
		t.Errorf("reason = %+v, want %s", cond, DenyReasonDefinitionMissing)
	}
}

func TestImpNetworkAttachment_PendsUntilVMScheduled(t *testing.T) {
	att := attRequest("tiny-vm")
	cfg := attConfigDef("lab-lan", 100)
	vm := scheduledVM("tiny-vm")
	vm.Status.NodeName = "" // not yet scheduled
	profile := attNodeProfile("node-a")
	scheme := runtime.NewScheme()
	_ = impdevv1alpha1.AddToScheme(scheme)
	r := &ImpNetworkAttachmentReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).
			WithStatusSubresource(&impdevv1alpha1.ImpNetworkAttachment{}).
			WithObjects(att, cfg, vm, profile).Build(),
		Scheme: scheme,
	}

	runReconcile(t, r, att)

	got := &impdevv1alpha1.ImpNetworkAttachment{}
	_ = r.Get(context.Background(), client.ObjectKeyFromObject(att), got)
	if got.Status.Phase != impdevv1alpha1.AttachmentPhasePending {
		t.Fatalf("phase = %q, want Pending", got.Status.Phase)
	}
	if cond := authorizedCondition(got); cond == nil || cond.Reason != "WaitingForSchedule" {
		t.Errorf("reason = %+v, want WaitingForSchedule", cond)
	}
}
