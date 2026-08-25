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

	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

// clusterConfigWithDefs builds a ClusterImpConfig carrying the given definitions.
func clusterConfigWithDefs(defs ...impdevv1alpha1.LANAttachmentSpec) *impdevv1alpha1.ClusterImpConfig {
	return &impdevv1alpha1.ClusterImpConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
		Spec: impdevv1alpha1.ClusterImpConfigSpec{
			Networking: impdevv1alpha1.NetworkingConfig{
				LANAttachments: defs,
			},
		},
	}
}

func defUntagged() impdevv1alpha1.LANAttachmentSpec {
	return impdevv1alpha1.LANAttachmentSpec{
		Name: "lab-lan", VLANID: 0, SubnetCIDR: "192.168.77.0/24", AllowDHCP: true,
	}
}

func defTaggedNoDHCP() impdevv1alpha1.LANAttachmentSpec {
	return impdevv1alpha1.LANAttachmentSpec{
		Name: "prod-vlan100", VLANID: 100, SubnetCIDR: "10.66.100.0/24",
	}
}

func attachmentWebhook(t *testing.T, objs ...runtime.Object) *ImpNetworkAttachmentWebhook {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := impdevv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("scheme: %v", err)
	}
	return &ImpNetworkAttachmentWebhook{
		Client: fake.NewClientBuilder().
			WithScheme(scheme).
			WithRuntimeObjects(objs...).
			Build(),
	}
}

func createRequest(user string, groups ...string) context.Context {
	req := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
		Operation: admissionv1.Create,
		UserInfo:  authenticationv1.UserInfo{Username: user, Groups: groups},
	}}
	return admission.NewContextWithRequest(context.Background(), req)
}

func newAttachment(def string) *impdevv1alpha1.ImpNetworkAttachment {
	return &impdevv1alpha1.ImpNetworkAttachment{
		ObjectMeta: metav1.ObjectMeta{Name: "att", Namespace: "default"},
		Spec: impdevv1alpha1.ImpNetworkAttachmentSpec{
			VMRef:         impdevv1alpha1.LocalObjectRef{Name: "tiny-vm"},
			AttachmentRef: def,
		},
	}
}

func TestImpNetworkAttachmentWebhook_Default_StampsRequesterOnCreate(t *testing.T) {
	wh := &ImpNetworkAttachmentWebhook{}
	att := newAttachment("lab-lan")
	ctx := createRequest("alice", "platform-admins")

	if err := wh.Default(ctx, att); err != nil {
		t.Fatalf("Default returned error: %v", err)
	}
	if got := att.Annotations[impdevv1alpha1.AnnotationRequester]; got != "alice" {
		t.Errorf("requester annotation = %q, want %q", got, "alice")
	}
	if got := att.Annotations[impdevv1alpha1.AnnotationRequesterGroups]; got != "platform-admins" {
		t.Errorf("groups annotation = %q, want %q", got, "platform-admins")
	}
	if att.Spec.Mode != impdevv1alpha1.AttachmentModeAccess {
		t.Errorf("mode = %q, want access default", att.Spec.Mode)
	}
}

func TestImpNetworkAttachmentWebhook_Default_SkipsStampingOnUpdate(t *testing.T) {
	wh := &ImpNetworkAttachmentWebhook{}
	att := newAttachment("lab-lan")
	req := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
		Operation: admissionv1.Update,
		UserInfo:  authenticationv1.UserInfo{Username: "mallory"},
	}}

	if err := wh.Default(admission.NewContextWithRequest(context.Background(), req), att); err != nil {
		t.Fatalf("Default returned error: %v", err)
	}
	if _, ok := att.Annotations[impdevv1alpha1.AnnotationRequester]; ok {
		t.Error("requester annotation must not be stamped on UPDATE")
	}
}

func TestImpNetworkAttachmentWebhook_ValidateCreate_AllowDHCP(t *testing.T) {
	cfg := clusterConfigWithDefs(defUntagged())
	wh := attachmentWebhook(t, cfg)

	att := newAttachment("lab-lan")
	att.Spec.DHCP = &impdevv1alpha1.DHCPRequestSpec{Enabled: true}
	if _, err := wh.ValidateCreate(createRequest("bob"), att); err != nil {
		t.Errorf("expected DHCP allowed when allowDHCP=true, got: %v", err)
	}
}

func TestImpNetworkAttachmentWebhook_ValidateCreate_DenyDHCPNotAllowed(t *testing.T) {
	cfg := clusterConfigWithDefs(defTaggedNoDHCP())
	wh := attachmentWebhook(t, cfg)

	att := newAttachment("prod-vlan100")
	att.Spec.DHCP = &impdevv1alpha1.DHCPRequestSpec{Enabled: true}
	_, err := wh.ValidateCreate(createRequest("bob"), att)
	if err == nil {
		t.Fatal("expected rejection when definition forbids DHCP")
	}
}

func TestImpNetworkAttachmentWebhook_ValidateCreate_StaticIPRequiredAndValidated(t *testing.T) {
	cfg := clusterConfigWithDefs(defTaggedNoDHCP())
	wh := attachmentWebhook(t, cfg)

	noIP := newAttachment("prod-vlan100")
	if _, err := wh.ValidateCreate(createRequest("bob"), noIP); err == nil {
		t.Error("expected rejection when neither DHCP nor static IP is set")
	}

	outside := newAttachment("prod-vlan100")
	outside.Spec.IP = "192.168.77.5" // outside prod-vlan100 subnet
	if _, err := wh.ValidateCreate(createRequest("bob"), outside); err == nil {
		t.Error("expected rejection for static IP outside the definition subnet")
	}

	valid := newAttachment("prod-vlan100")
	valid.Spec.IP = "10.66.100.23"
	if _, err := wh.ValidateCreate(createRequest("bob"), valid); err != nil {
		t.Errorf("expected valid static IP accepted, got: %v", err)
	}
}

func TestImpNetworkAttachmentWebhook_ValidateCreate_UnknownDefinition(t *testing.T) {
	cfg := clusterConfigWithDefs(defUntagged())
	wh := attachmentWebhook(t, cfg)

	if _, err := wh.ValidateCreate(createRequest("bob"), newAttachment("nope")); err == nil {
		t.Error("expected rejection for unknown attachment definition")
	}
}

func TestImpNetworkAttachmentWebhook_ValidateCreate_SubjectAllowlist(t *testing.T) {
	restricted := defUntagged()
	restricted.AllowedSubjects = []string{"group:platform-admins"}
	cfg := clusterConfigWithDefs(restricted)
	wh := attachmentWebhook(t, cfg)

	allowed := newAttachment("lab-lan")
	allowed.Spec.DHCP = &impdevv1alpha1.DHCPRequestSpec{Enabled: true}
	if _, err := wh.ValidateCreate(createRequest("alice", "platform-admins"), allowed); err != nil {
		t.Errorf("expected group member allowed, got: %v", err)
	}

	denied := newAttachment("lab-lan")
	denied.Spec.DHCP = &impdevv1alpha1.DHCPRequestSpec{Enabled: true}
	if _, err := wh.ValidateCreate(createRequest("eve", "randoms"), denied); err == nil {
		t.Error("expected non-member rejected by subject allowlist")
	}
}

func TestImpNetworkAttachmentWebhook_ValidateUpdate_Immutable(t *testing.T) {
	wh := &ImpNetworkAttachmentWebhook{}

	oldAtt := newAttachment("lab-lan")
	oldAtt.Annotations = map[string]string{
		impdevv1alpha1.AnnotationRequester: "alice",
	}
	newAtt := oldAtt.DeepCopy()
	newAtt.Spec.AttachmentRef = "prod-vlan100"
	if _, err := wh.ValidateUpdate(context.Background(), oldAtt, newAtt); err == nil {
		t.Error("expected spec mutation rejected after creation")
	}

	newAtt2 := oldAtt.DeepCopy()
	newAtt2.Annotations[impdevv1alpha1.AnnotationRequester] = "mallory"
	if _, err := wh.ValidateUpdate(context.Background(), oldAtt, newAtt2); err == nil {
		t.Error("expected requester annotation mutation rejected")
	}
}

func TestImpNetworkAttachmentWebhook_ValidateDelete_RunningVMDenied(t *testing.T) {
	vm := &impdevv1alpha1.ImpVM{
		ObjectMeta: metav1.ObjectMeta{Name: "tiny-vm", Namespace: "default"},
		Status:     impdevv1alpha1.ImpVMStatus{Phase: impdevv1alpha1.VMPhaseRunning},
	}
	wh := attachmentWebhook(t, vm)

	if _, err := wh.ValidateDelete(context.Background(), newAttachment("lab-lan")); err == nil {
		t.Error("expected deletion denied while VM is running")
	}

	forced := newAttachment("lab-lan")
	forced.Annotations = map[string]string{impdevv1alpha1.AnnotationForceDelete: "true"}
	if _, err := wh.ValidateDelete(context.Background(), forced); err != nil {
		t.Errorf("expected force-delete override allowed, got: %v", err)
	}
}
