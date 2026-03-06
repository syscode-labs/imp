package controller

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	impv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

func newRunnerPoolTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	_ = impv1alpha1.AddToScheme(s)
	return s
}

func TestRunnerPoolReconciler_createsMinIdleVMs(t *testing.T) {
	minIdle := int32(2)
	pool := &impv1alpha1.ImpVMRunnerPool{
		ObjectMeta: metav1.ObjectMeta{Name: "ci-pool", Namespace: "ci"},
		Spec: impv1alpha1.ImpVMRunnerPoolSpec{
			TemplateName: "ubuntu-runner",
			Platform: impv1alpha1.RunnerPlatformSpec{
				Type:              "github-actions",
				CredentialsSecret: "gh-creds",
			},
			Scaling: &impv1alpha1.RunnerScalingSpec{MinIdle: minIdle, MaxConcurrent: 5},
		},
	}
	tpl := &impv1alpha1.ImpVMTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "ubuntu-runner", Namespace: "ci"},
		Spec: impv1alpha1.ImpVMTemplateSpec{
			ClassRef: impv1alpha1.ClusterObjectRef{Name: "standard"},
			Image:    "ubuntu:22.04",
		},
	}

	scheme := newRunnerPoolTestScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(pool, tpl).WithStatusSubresource(pool).Build()
	r := &ImpVMRunnerPoolReconciler{Client: c, Scheme: scheme}

	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "ci-pool", Namespace: "ci"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	vmList := &impv1alpha1.ImpVMList{}
	_ = c.List(context.Background(), vmList,
		client.InNamespace("ci"),
		client.MatchingLabels{impv1alpha1.LabelRunnerPool: "ci-pool"})
	if int32(len(vmList.Items)) != minIdle {
		t.Errorf("expected %d VMs (minIdle), got %d", minIdle, len(vmList.Items))
	}
}

func TestRunnerPoolReconciler_deletesTerminalVMs(t *testing.T) {
	pool := &impv1alpha1.ImpVMRunnerPool{
		ObjectMeta: metav1.ObjectMeta{Name: "ci-pool", Namespace: "ci"},
		Spec: impv1alpha1.ImpVMRunnerPoolSpec{
			TemplateName: "ubuntu-runner",
			Platform: impv1alpha1.RunnerPlatformSpec{
				Type:              "github-actions",
				CredentialsSecret: "gh-creds",
			},
			Scaling: &impv1alpha1.RunnerScalingSpec{MinIdle: 0, MaxConcurrent: 5},
		},
	}
	tpl := &impv1alpha1.ImpVMTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "ubuntu-runner", Namespace: "ci"},
		Spec:       impv1alpha1.ImpVMTemplateSpec{ClassRef: impv1alpha1.ClusterObjectRef{Name: "standard"}},
	}
	doneVM := &impv1alpha1.ImpVM{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ci-pool-abc", Namespace: "ci",
			Labels: map[string]string{impv1alpha1.LabelRunnerPool: "ci-pool"},
		},
	}
	doneVM.Status.Phase = impv1alpha1.VMPhaseSucceeded

	scheme := newRunnerPoolTestScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(pool, tpl, doneVM).WithStatusSubresource(pool).Build()
	r := &ImpVMRunnerPoolReconciler{Client: c, Scheme: scheme}

	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "ci-pool", Namespace: "ci"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	vmList := &impv1alpha1.ImpVMList{}
	_ = c.List(context.Background(), vmList,
		client.InNamespace("ci"),
		client.MatchingLabels{impv1alpha1.LabelRunnerPool: "ci-pool"})
	if len(vmList.Items) != 0 {
		t.Errorf("expected 0 VMs after terminal cleanup, got %d", len(vmList.Items))
	}
}

func TestRunnerPoolReconciler_respectsMaxConcurrent(t *testing.T) {
	// minIdle=3, maxConcurrent=2 → only 2 VMs should be created
	pool := &impv1alpha1.ImpVMRunnerPool{
		ObjectMeta: metav1.ObjectMeta{Name: "ci-pool", Namespace: "ci"},
		Spec: impv1alpha1.ImpVMRunnerPoolSpec{
			TemplateName: "ubuntu-runner",
			Platform: impv1alpha1.RunnerPlatformSpec{
				Type:              "github-actions",
				CredentialsSecret: "gh-creds",
			},
			Scaling: &impv1alpha1.RunnerScalingSpec{MinIdle: 3, MaxConcurrent: 2},
		},
	}
	tpl := &impv1alpha1.ImpVMTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "ubuntu-runner", Namespace: "ci"},
		Spec:       impv1alpha1.ImpVMTemplateSpec{ClassRef: impv1alpha1.ClusterObjectRef{Name: "standard"}},
	}

	scheme := newRunnerPoolTestScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(pool, tpl).WithStatusSubresource(pool).Build()
	r := &ImpVMRunnerPoolReconciler{Client: c, Scheme: scheme}

	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "ci-pool", Namespace: "ci"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	vmList := &impv1alpha1.ImpVMList{}
	_ = c.List(context.Background(), vmList,
		client.InNamespace("ci"),
		client.MatchingLabels{impv1alpha1.LabelRunnerPool: "ci-pool"})
	if len(vmList.Items) != 2 {
		t.Errorf("expected exactly 2 VMs (maxConcurrent=2, minIdle=3), got %d", len(vmList.Items))
	}
}
