package controller

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	impv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
	"github.com/syscode-labs/imp/internal/runner"
)

func newRunnerPoolTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	_ = impv1alpha1.AddToScheme(s)
	_ = corev1.AddToScheme(s)
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
			Scaling: &impv1alpha1.RunnerScalingSpec{
				Mode:            impv1alpha1.RunnerScalingModeWebhook,
				MinIdle:         ptrInt32(minIdle),
				MaxConcurrent:   ptrInt32(5),
				ScaleUpStep:     ptrInt32(5),
				CooldownSeconds: ptrInt32(30),
				Webhook:         &impv1alpha1.RunnerWebhookSpec{SecretRef: "wh"},
			},
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
	r := &ImpVMRunnerPoolReconciler{Client: c, Scheme: scheme, DriverFactory: func(_ context.Context, _ client.Client, _ *impv1alpha1.ImpVMRunnerPool) (runnerQueueDepthReader, error) {
		return &stubRunnerQueueDepthReader{}, nil
	}}

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
			Platform:     impv1alpha1.RunnerPlatformSpec{Type: "github-actions", CredentialsSecret: "gh-creds"},
			Scaling: &impv1alpha1.RunnerScalingSpec{
				Mode:            impv1alpha1.RunnerScalingModeWebhook,
				MinIdle:         ptrInt32(0),
				MaxConcurrent:   ptrInt32(5),
				ScaleUpStep:     ptrInt32(5),
				CooldownSeconds: ptrInt32(30),
				Webhook:         &impv1alpha1.RunnerWebhookSpec{SecretRef: "wh"},
			},
		},
	}
	tpl := &impv1alpha1.ImpVMTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "ubuntu-runner", Namespace: "ci"},
		Spec:       impv1alpha1.ImpVMTemplateSpec{ClassRef: impv1alpha1.ClusterObjectRef{Name: "standard"}},
	}
	doneVM := &impv1alpha1.ImpVM{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ci-pool-abc",
			Namespace: "ci",
			Labels:    map[string]string{impv1alpha1.LabelRunnerPool: "ci-pool"},
		},
	}
	doneVM.Status.Phase = impv1alpha1.VMPhaseSucceeded

	scheme := newRunnerPoolTestScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(pool, tpl, doneVM).WithStatusSubresource(pool).Build()
	r := &ImpVMRunnerPoolReconciler{Client: c, Scheme: scheme, DriverFactory: func(_ context.Context, _ client.Client, _ *impv1alpha1.ImpVMRunnerPool) (runnerQueueDepthReader, error) {
		return &stubRunnerQueueDepthReader{}, nil
	}}

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
	pool := &impv1alpha1.ImpVMRunnerPool{
		ObjectMeta: metav1.ObjectMeta{Name: "ci-pool", Namespace: "ci"},
		Spec: impv1alpha1.ImpVMRunnerPoolSpec{
			TemplateName: "ubuntu-runner",
			Platform:     impv1alpha1.RunnerPlatformSpec{Type: "github-actions", CredentialsSecret: "gh-creds"},
			Scaling: &impv1alpha1.RunnerScalingSpec{
				Mode:            impv1alpha1.RunnerScalingModeWebhook,
				MinIdle:         ptrInt32(3),
				MaxConcurrent:   ptrInt32(2),
				ScaleUpStep:     ptrInt32(5),
				CooldownSeconds: ptrInt32(30),
				Webhook:         &impv1alpha1.RunnerWebhookSpec{SecretRef: "wh"},
			},
		},
	}
	tpl := &impv1alpha1.ImpVMTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "ubuntu-runner", Namespace: "ci"},
		Spec:       impv1alpha1.ImpVMTemplateSpec{ClassRef: impv1alpha1.ClusterObjectRef{Name: "standard"}},
	}

	scheme := newRunnerPoolTestScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(pool, tpl).WithStatusSubresource(pool).Build()
	r := &ImpVMRunnerPoolReconciler{Client: c, Scheme: scheme, DriverFactory: func(_ context.Context, _ client.Client, _ *impv1alpha1.ImpVMRunnerPool) (runnerQueueDepthReader, error) {
		return &stubRunnerQueueDepthReader{}, nil
	}}

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

func TestRunnerPoolReconciler_propagatesCompositeLayers(t *testing.T) {
	pool := &impv1alpha1.ImpVMRunnerPool{
		ObjectMeta: metav1.ObjectMeta{Name: "ci-pool", Namespace: "ci"},
		Spec: impv1alpha1.ImpVMRunnerPoolSpec{
			TemplateName: "ubuntu-runner",
			Platform:     impv1alpha1.RunnerPlatformSpec{Type: "github-actions", CredentialsSecret: "gh-creds"},
			RunnerLayer:  "ghcr.io/syscode-labs/imp-runners/github-actions:v1",
			Scaling: &impv1alpha1.RunnerScalingSpec{
				Mode:            impv1alpha1.RunnerScalingModeWebhook,
				MinIdle:         ptrInt32(1),
				MaxConcurrent:   ptrInt32(5),
				ScaleUpStep:     ptrInt32(5),
				CooldownSeconds: ptrInt32(30),
				Webhook:         &impv1alpha1.RunnerWebhookSpec{SecretRef: "wh"},
			},
		},
	}
	tpl := &impv1alpha1.ImpVMTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "ubuntu-runner", Namespace: "ci"},
		Spec: impv1alpha1.ImpVMTemplateSpec{
			ClassRef:    impv1alpha1.ClusterObjectRef{Name: "standard"},
			Image:       "ubuntu:22.04",
			CiliumLayer: "ghcr.io/syscode-labs/imp-layers/cilium:v1",
		},
	}

	scheme := newRunnerPoolTestScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(pool, tpl).WithStatusSubresource(pool).Build()
	r := &ImpVMRunnerPoolReconciler{Client: c, Scheme: scheme, DriverFactory: func(_ context.Context, _ client.Client, _ *impv1alpha1.ImpVMRunnerPool) (runnerQueueDepthReader, error) {
		return &stubRunnerQueueDepthReader{}, nil
	}}

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
	if len(vmList.Items) != 1 {
		t.Fatalf("expected 1 VM, got %d", len(vmList.Items))
	}
	got := vmList.Items[0].Spec
	if got.RunnerLayer != "ghcr.io/syscode-labs/imp-runners/github-actions:v1" {
		t.Fatalf("runnerLayer = %q", got.RunnerLayer)
	}
	if got.CiliumLayer != "ghcr.io/syscode-labs/imp-layers/cilium:v1" {
		t.Fatalf("ciliumLayer = %q", got.CiliumLayer)
	}
}

func TestRunnerPoolReconciler_scalingWebhookUsesStepLimit(t *testing.T) {
	pool := &impv1alpha1.ImpVMRunnerPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ci-pool",
			Namespace: "ci",
			Annotations: map[string]string{
				AnnotationRunnerDemand: "5",
			},
		},
		Spec: impv1alpha1.ImpVMRunnerPoolSpec{
			TemplateName: "ubuntu-runner",
			Platform:     impv1alpha1.RunnerPlatformSpec{Type: "github-actions", CredentialsSecret: "gh-creds"},
			Scaling: &impv1alpha1.RunnerScalingSpec{
				Mode:            impv1alpha1.RunnerScalingModeWebhook,
				MinIdle:         ptrInt32(0),
				MaxConcurrent:   ptrInt32(10),
				ScaleUpStep:     ptrInt32(2),
				CooldownSeconds: ptrInt32(30),
				Webhook:         &impv1alpha1.RunnerWebhookSpec{SecretRef: "gh-webhook"},
			},
		},
	}
	tpl := &impv1alpha1.ImpVMTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "ubuntu-runner", Namespace: "ci"},
		Spec:       impv1alpha1.ImpVMTemplateSpec{ClassRef: impv1alpha1.ClusterObjectRef{Name: "standard"}, Image: "ubuntu:22.04"},
	}

	scheme := newRunnerPoolTestScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(pool, tpl).WithStatusSubresource(pool).Build()
	r := &ImpVMRunnerPoolReconciler{Client: c, Scheme: scheme, DriverFactory: func(_ context.Context, _ client.Client, _ *impv1alpha1.ImpVMRunnerPool) (runnerQueueDepthReader, error) {
		return &stubRunnerQueueDepthReader{}, nil
	}}

	result, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "ci-pool", Namespace: "ci"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RequeueAfter != 30*time.Second {
		t.Fatalf("expected requeueAfter=30s, got %v", result.RequeueAfter)
	}

	vmList := &impv1alpha1.ImpVMList{}
	_ = c.List(context.Background(), vmList,
		client.InNamespace("ci"),
		client.MatchingLabels{impv1alpha1.LabelRunnerPool: "ci-pool"})
	if len(vmList.Items) != 2 {
		t.Fatalf("expected 2 VMs due to scaleUpStep, got %d", len(vmList.Items))
	}
}

func TestRunnerPoolReconciler_scalingPollingUsesQueueDepth(t *testing.T) {
	pool := &impv1alpha1.ImpVMRunnerPool{
		ObjectMeta: metav1.ObjectMeta{Name: "ci-pool", Namespace: "ci"},
		Spec: impv1alpha1.ImpVMRunnerPoolSpec{
			TemplateName: "ubuntu-runner",
			Platform:     impv1alpha1.RunnerPlatformSpec{Type: "github-actions", CredentialsSecret: "gh-creds"},
			Scaling: &impv1alpha1.RunnerScalingSpec{
				Mode:            impv1alpha1.RunnerScalingModePolling,
				MinIdle:         ptrInt32(0),
				MaxConcurrent:   ptrInt32(10),
				ScaleUpStep:     ptrInt32(4),
				CooldownSeconds: ptrInt32(45),
				Polling:         &impv1alpha1.RunnerPollingSpec{IntervalSeconds: 15},
			},
		},
	}
	tpl := &impv1alpha1.ImpVMTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "ubuntu-runner", Namespace: "ci"},
		Spec:       impv1alpha1.ImpVMTemplateSpec{ClassRef: impv1alpha1.ClusterObjectRef{Name: "standard"}, Image: "ubuntu:22.04"},
	}

	scheme := newRunnerPoolTestScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(pool, tpl).WithStatusSubresource(pool).Build()
	r := &ImpVMRunnerPoolReconciler{
		Client: c,
		Scheme: scheme,
		DriverFactory: func(
			_ context.Context, _ client.Client, _ *impv1alpha1.ImpVMRunnerPool,
		) (runnerQueueDepthReader, error) {
			return &stubRunnerQueueDepthReader{queueDepth: 3}, nil
		},
	}

	result, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "ci-pool", Namespace: "ci"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RequeueAfter != 15*time.Second {
		t.Fatalf("expected requeueAfter=15s from polling interval, got %v", result.RequeueAfter)
	}

	vmList := &impv1alpha1.ImpVMList{}
	_ = c.List(context.Background(), vmList,
		client.InNamespace("ci"),
		client.MatchingLabels{impv1alpha1.LabelRunnerPool: "ci-pool"})
	if len(vmList.Items) != 3 {
		t.Fatalf("expected 3 VMs from polling queue depth, got %d", len(vmList.Items))
	}
}

func TestRunnerPoolReconciler_webhookOnlyMintsPerVMAndSkipsQueue(t *testing.T) {
	pool := &impv1alpha1.ImpVMRunnerPool{
		ObjectMeta: metav1.ObjectMeta{Name: "webhook-pool", Namespace: "ci", Annotations: map[string]string{AnnotationRunnerDemand: "2"}},
		Spec: impv1alpha1.ImpVMRunnerPoolSpec{
			TemplateName: "webhook-template",
			Platform:     impv1alpha1.RunnerPlatformSpec{Type: "github-actions", CredentialsSecret: "unused"},
			Scaling:      &impv1alpha1.RunnerScalingSpec{Mode: impv1alpha1.RunnerScalingModeWebhook, MinIdle: ptrInt32(0), MaxConcurrent: ptrInt32(5), ScaleUpStep: ptrInt32(2), CooldownSeconds: ptrInt32(30), Webhook: &impv1alpha1.RunnerWebhookSpec{Enabled: true, SecretRef: "webhook"}},
		},
	}
	tpl := &impv1alpha1.ImpVMTemplate{ObjectMeta: metav1.ObjectMeta{Name: "webhook-template", Namespace: "ci"}, Spec: impv1alpha1.ImpVMTemplateSpec{ClassRef: impv1alpha1.ClusterObjectRef{Name: "standard"}, Image: "ubuntu:22.04"}}
	driver := &stubRunnerQueueDepthReader{jitConfig: &runner.JITConfig{EncodedConfig: "encoded", RunnerName: "runner"}}
	scheme := newRunnerPoolTestScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool, tpl).WithStatusSubresource(pool).Build()
	r := &ImpVMRunnerPoolReconciler{Client: c, Scheme: scheme, DriverFactory: func(context.Context, client.Client, *impv1alpha1.ImpVMRunnerPool) (runnerQueueDepthReader, error) {
		return driver, nil
	}}
	if _, err := r.Reconcile(context.Background(), reconcile.Request{NamespacedName: types.NamespacedName{Name: pool.Name, Namespace: pool.Namespace}}); err != nil {
		t.Fatal(err)
	}
	if driver.queueCalls != 0 {
		t.Fatalf("webhook-only reconcile polled queue %d times", driver.queueCalls)
	}
	if driver.jitCalls != 2 {
		t.Fatalf("minted %d JIT configs, want 2", driver.jitCalls)
	}
	var vms impv1alpha1.ImpVMList
	if err := c.List(context.Background(), &vms, client.InNamespace("ci"), client.MatchingLabels{impv1alpha1.LabelRunnerPool: pool.Name}); err != nil {
		t.Fatal(err)
	}
	if len(vms.Items) != 2 {
		t.Fatalf("got %d VMs, want 2", len(vms.Items))
	}
	refs := map[string]bool{}
	for i := range vms.Items {
		vm := &vms.Items[i]
		if vm.Spec.RunnerConfigSecret == "" || refs[vm.Spec.RunnerConfigSecret] {
			t.Fatalf("invalid JIT Secret reference %q", vm.Spec.RunnerConfigSecret)
		}
		refs[vm.Spec.RunnerConfigSecret] = true
		var secret corev1.Secret
		if err := c.Get(context.Background(), types.NamespacedName{Namespace: vm.Namespace, Name: vm.Spec.RunnerConfigSecret}, &secret); err != nil {
			t.Fatal(err)
		}
		if !metav1.IsControlledBy(&secret, vm) {
			t.Fatalf("Secret %s is not owned by VM %s", secret.Name, vm.Name)
		}
	}
}

func TestRunnerPoolReconciler_defaultFactoryAuthFailureLeavesNoVM(t *testing.T) {
	pool := &impv1alpha1.ImpVMRunnerPool{ObjectMeta: metav1.ObjectMeta{Name: "auth-failure", Namespace: "ci", Annotations: map[string]string{AnnotationRunnerDemand: "1"}}, Spec: impv1alpha1.ImpVMRunnerPoolSpec{TemplateName: "template", Platform: impv1alpha1.RunnerPlatformSpec{Type: "github-actions", TokenSource: "github_app", CredentialsSecret: "missing-credentials"}, Scaling: &impv1alpha1.RunnerScalingSpec{Mode: impv1alpha1.RunnerScalingModeWebhook, MinIdle: ptrInt32(0), MaxConcurrent: ptrInt32(1), ScaleUpStep: ptrInt32(1), CooldownSeconds: ptrInt32(30), Webhook: &impv1alpha1.RunnerWebhookSpec{Enabled: true, SecretRef: "webhook"}}}}
	tpl := &impv1alpha1.ImpVMTemplate{ObjectMeta: metav1.ObjectMeta{Name: "template", Namespace: "ci"}, Spec: impv1alpha1.ImpVMTemplateSpec{ClassRef: impv1alpha1.ClusterObjectRef{Name: "standard"}, Image: "ubuntu:22.04"}}
	scheme := newRunnerPoolTestScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool, tpl).WithStatusSubresource(pool).Build()
	r := &ImpVMRunnerPoolReconciler{Client: c, Scheme: scheme}
	if _, err := r.Reconcile(context.Background(), reconcile.Request{NamespacedName: types.NamespacedName{Name: pool.Name, Namespace: pool.Namespace}}); err == nil {
		t.Fatal("expected default factory authentication failure")
	}
	var vms impv1alpha1.ImpVMList
	if err := c.List(context.Background(), &vms, client.InNamespace(pool.Namespace), client.MatchingLabels{impv1alpha1.LabelRunnerPool: pool.Name}); err != nil {
		t.Fatal(err)
	}
	if len(vms.Items) != 0 {
		t.Fatalf("retained %d VM(s) after auth failure", len(vms.Items))
	}
}

type stubRunnerQueueDepthReader struct {
	queueDepth int
	err        error
	queueCalls int
	jitCalls   int
	jitConfig  *runner.JITConfig
}

func (s *stubRunnerQueueDepthReader) QueueDepth(_ context.Context) (int, error) {
	s.queueCalls++
	return s.queueDepth, s.err
}

func (s *stubRunnerQueueDepthReader) GetJITConfig(_ context.Context) (*runner.JITConfig, error) {
	s.jitCalls++
	if s.jitConfig != nil {
		return s.jitConfig, nil
	}
	return &runner.JITConfig{EncodedConfig: "test", RunnerName: "test"}, nil
}

func ptrInt32(v int32) *int32 { return &v }
