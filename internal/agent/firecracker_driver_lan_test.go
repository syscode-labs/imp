//go:build linux

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

package agent

import (
	"context"
	"strings"
	"testing"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/syscode-labs/imp/internal/agent/network"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

// lanTestEnv builds a driver plus Kubernetes objects for LAN attachment tests.
type lanTestEnv struct {
	driver *FirecrackerDriver
	stub   *network.StubNetManager
}

func newLanTestEnv(t *testing.T, vlanID int32, allowDHCP bool, dhcp bool, objs ...ctrlclient.Object) *lanTestEnv {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := impdevv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("scheme: %v", err)
	}

	cfg := &impdevv1alpha1.ClusterImpConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
		Spec: impdevv1alpha1.ClusterImpConfigSpec{
			Networking: impdevv1alpha1.NetworkingConfig{
				LANAttachments: []impdevv1alpha1.LANAttachmentSpec{{
					Name:       "lab-lan",
					VLANID:     vlanID,
					SubnetCIDR: "192.168.77.0/24",
					AllowDHCP:  allowDHCP,
				}},
			},
		},
	}
	profile := &impdevv1alpha1.ClusterImpNodeProfile{
		ObjectMeta: metav1.ObjectMeta{Name: "node-a"},
		Spec: impdevv1alpha1.ClusterImpNodeProfileSpec{
			LANBindings: []impdevv1alpha1.NodeLANBinding{
				{AttachmentName: "lab-lan", ParentInterface: "br-lan"},
			},
		},
	}
	vmObj := &impdevv1alpha1.ImpVM{
		ObjectMeta: metav1.ObjectMeta{Name: "tiny-vm", Namespace: "default"},
		Status:     impdevv1alpha1.ImpVMStatus{Phase: impdevv1alpha1.VMPhaseRunning},
	}
	att := &impdevv1alpha1.ImpNetworkAttachment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "att-tiny-vm",
			Namespace: "default",
			Annotations: map[string]string{
				impdevv1alpha1.AnnotationRequester: "alice",
			},
		},
		Spec: impdevv1alpha1.ImpNetworkAttachmentSpec{
			VMRef:         impdevv1alpha1.LocalObjectRef{Name: "tiny-vm"},
			AttachmentRef: "lab-lan",
		},
	}
	if dhcp {
		att.Spec.DHCP = &impdevv1alpha1.DHCPRequestSpec{Enabled: true}
	} else {
		att.Spec.IP = "192.168.77.20"
	}
	apimeta.SetStatusCondition(&att.Status.Conditions, metav1.Condition{
		Type:   impdevv1alpha1.ConditionAttachmentAuthorized,
		Status: metav1.ConditionTrue,
		Reason: "Allowed",
	})

	builder := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cfg, profile, vmObj, att)
	for _, o := range objs {
		builder = builder.WithObjects(o)
	}

	stub := &network.StubNetManager{}
	d := &FirecrackerDriver{
		Client:   builder.Build(),
		Net:      stub,
		NodeName: "node-a",
	}
	return &lanTestEnv{driver: d, stub: stub}
}

func TestFirecrackerDriver_SetupLANAttachment_Tagged(t *testing.T) {
	env := newLanTestEnv(t, 100, false, false)
	vm := &impdevv1alpha1.ImpVM{}
	vm.Namespace = "default"
	vm.Name = "tiny-vm"

	info, err := env.driver.setupLANAttachment(context.Background(), vm)
	if err != nil {
		t.Fatalf("setupLANAttachment: %v", err)
	}
	if !info.IsLAN || info.VLANID != 100 || info.IP != "" || info.DHCP {
		t.Errorf("unexpected NetworkInfo: %+v", info)
	}
	wantBridge := network.BridgeName("lan/lab-lan")
	if len(env.stub.EnsureLANCalls) != 1 || !strings.HasSuffix(env.stub.EnsureLANCalls[0], "|br-lan|100") {
		t.Errorf("EnsureLANCalls = %v, want one tagged call on br-lan", env.stub.EnsureLANCalls)
	}
	if len(env.stub.SetupVMCalls) != 1 {
		t.Errorf("SetupVMCalls = %v, want exactly one tap", env.stub.SetupVMCalls)
	}
	if info.BridgeName != wantBridge {
		t.Errorf("bridge = %q, want %q", info.BridgeName, wantBridge)
	}
}

func TestFirecrackerDriver_SetupLANAttachment_UntaggedDHCPBridgesOntoParent(t *testing.T) {
	env := newLanTestEnv(t, 0, true, true)
	vm := &impdevv1alpha1.ImpVM{}
	vm.Namespace = "default"
	vm.Name = "tiny-vm"

	info, err := env.driver.setupLANAttachment(context.Background(), vm)
	if err != nil {
		t.Fatalf("setupLANAttachment: %v", err)
	}
	if !info.IsLAN || !info.DHCP || info.VLANID != 0 {
		t.Errorf("unexpected NetworkInfo: %+v", info)
	}
	if info.BridgeName != "br-lan" {
		t.Errorf("bridge = %q, want parent bridge br-lan directly", info.BridgeName)
	}
}

func TestFirecrackerDriver_SetupLANAttachment_NoAttachmentIsNoop(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := impdevv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("scheme: %v", err)
	}
	d := &FirecrackerDriver{
		Client: fake.NewClientBuilder().WithScheme(scheme).Build(),
		Net:    &network.StubNetManager{},
	}
	vm := &impdevv1alpha1.ImpVM{}
	vm.Namespace = "default"
	vm.Name = "lonely"

	info, err := d.setupLANAttachment(context.Background(), vm)
	if err != nil {
		t.Fatalf("expected nil error without attachments, got: %v", err)
	}
	if info != nil {
		t.Errorf("expected nil NetworkInfo, got %+v", info)
	}
}

func TestFirecrackerDriver_Stop_TeardownsLANBridge(t *testing.T) {
	stub := &network.StubNetManager{}
	d := &FirecrackerDriver{
		Net:   stub,
		Alloc: network.NewAllocator(),
		procs: make(map[string]*fcProc),
	}

	vm := &impdevv1alpha1.ImpVM{}
	vm.Namespace = "default"
	vm.Name = "lan-stop"

	d.procs[vmKey(vm)] = &fcProc{
		pid: 99999,
		netInfo: &network.NetworkInfo{
			TAPName:         "imptap-deadbeef",
			BridgeName:      "impbr-12345678",
			NetworkKey:      "lan/lab-lan",
			Subnet:          "192.168.77.0/24",
			IsLAN:           true,
			VLANID:          100,
			ParentInterface: "br-lan",
		},
	}

	if err := d.Stop(context.Background(), vm); err != nil {
		t.Fatalf("Stop error: %v", err)
	}
	if len(stub.TeardownVMCalls) != 1 {
		t.Errorf("TeardownVMCalls = %v, want tap teardown", stub.TeardownVMCalls)
	}
	if len(stub.TeardownLANCalls) != 1 || !strings.HasSuffix(stub.TeardownLANCalls[0], "|br-lan|100") {
		t.Errorf("TeardownLANCalls = %v, want one call for br-lan/100", stub.TeardownLANCalls)
	}
}

func TestVLANIfaceName(t *testing.T) {
	if got := network.VLANIfaceName("enp3s0", 100); got != "enp3s0.100" {
		t.Errorf("direct name = %q, want enp3s0.100", got)
	}
	long := "enp129s0f1np1" // 13 chars — .4094 would overflow IFNAMSIZ
	got := network.VLANIfaceName(long, 4094)
	if len(got) > 15 {
		t.Errorf("name %q exceeds IFNAMSIZ limit", got)
	}
	if got == long+".4094" {
		t.Errorf("long parent must get deterministic short name, got %q", got)
	}
	if again := network.VLANIfaceName(long, 4094); again != got {
		t.Errorf("name not deterministic: %q vs %q", again, got)
	}
}
