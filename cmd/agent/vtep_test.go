//go:build linux

package main

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

func TestResolveVTEPIP(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := impdevv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add Imp scheme: %v", err)
	}

	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&impdevv1alpha1.ClusterImpNodeProfile{
		ObjectMeta: metav1.ObjectMeta{Name: "node-a"},
		Spec:       impdevv1alpha1.ClusterImpNodeProfileSpec{VTEPIP: "192.168.122.10"},
	}).Build()

	ip, err := resolveVTEPIP(context.Background(), client, "node-a")
	if err != nil {
		t.Fatalf("resolve VTEP IP: %v", err)
	}
	if ip != "192.168.122.10" {
		t.Fatalf("VTEP IP = %q, want 192.168.122.10", ip)
	}
}

func TestResolveVTEPIPWithoutProfile(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := impdevv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add Imp scheme: %v", err)
	}

	ip, err := resolveVTEPIP(context.Background(), fake.NewClientBuilder().WithScheme(scheme).Build(), "node-a")
	if err != nil {
		t.Fatalf("resolve VTEP IP without profile: %v", err)
	}
	if ip != "" {
		t.Fatalf("VTEP IP = %q, want empty", ip)
	}
}

func TestResolveVTEPIPRejectsInvalidAddress(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := impdevv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add Imp scheme: %v", err)
	}

	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&impdevv1alpha1.ClusterImpNodeProfile{
		ObjectMeta: metav1.ObjectMeta{Name: "node-a"},
		Spec:       impdevv1alpha1.ClusterImpNodeProfileSpec{VTEPIP: "not-an-ip"},
	}).Build()

	if _, err := resolveVTEPIP(context.Background(), client, "node-a"); err == nil {
		t.Fatal("resolve VTEP IP accepted an invalid address")
	}
}
