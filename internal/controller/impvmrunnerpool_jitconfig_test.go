package controller

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	impv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
	"github.com/syscode-labs/imp/internal/runner"
)

type fakeJITDriver struct {
	jit  *runner.JITConfig
	err  error
	mint int
}

func (f *fakeJITDriver) QueueDepth(_ context.Context) (int, error) { return 0, nil }

func (f *fakeJITDriver) GetJITConfig(_ context.Context) (*runner.JITConfig, error) {
	f.mint++
	return f.jit, f.err
}

func runnerPoolFixture() (*impv1alpha1.ImpVMRunnerPool, *impv1alpha1.ImpVMTemplate) {
	tpl := &impv1alpha1.ImpVMTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "tpl", Namespace: "ns"},
		Spec: impv1alpha1.ImpVMTemplateSpec{
			ClassRef: impv1alpha1.ClusterObjectRef{Name: "cls"},
			Image:    "ghcr.io/example/base:1",
		},
	}
	pool := &impv1alpha1.ImpVMRunnerPool{
		ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: "ns"},
		Spec: impv1alpha1.ImpVMRunnerPoolSpec{
			TemplateName: "tpl",
			Platform: impv1alpha1.RunnerPlatformSpec{
				Type:              "github-actions",
				TokenSource:       "github_app",
				CredentialsSecret: "creds",
			},
			Scaling: &impv1alpha1.RunnerScalingSpec{
				Mode:            impv1alpha1.RunnerScalingModePolling,
				MinIdle:         jitInt32Ptr(0),
				MaxConcurrent:   jitInt32Ptr(1),
				ScaleUpStep:     jitInt32Ptr(1),
				CooldownSeconds: jitInt32Ptr(30),
				Polling:         &impv1alpha1.RunnerPollingSpec{IntervalSeconds: 30},
			},
		},
	}
	return pool, tpl
}

func jitInt32Ptr(i int32) *int32 { return &i }

func jitReconciler(t *testing.T, drv *fakeJITDriver, objs ...client.Object) *ImpVMRunnerPoolReconciler {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, impv1alpha1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
	return &ImpVMRunnerPoolReconciler{
		Client: c, Scheme: scheme,
		DriverFactory: func(_ context.Context, _ client.Client, _ *impv1alpha1.ImpVMRunnerPool) (runnerQueueDepthReader, error) {
			return drv, nil
		},
	}
}

func TestCreateRunnerVMMintsJITConfigSecret(t *testing.T) {
	pool, tpl := runnerPoolFixture()
	drv := &fakeJITDriver{jit: &runner.JITConfig{EncodedConfig: "ENCODED", RunnerName: "pool-abc12"}}
	r := jitReconciler(t, drv, pool, tpl)

	require.NoError(t, r.createRunnerVM(context.Background(), pool, tpl))
	require.Equal(t, 1, drv.mint)

	var vms impv1alpha1.ImpVMList
	require.NoError(t, r.List(context.Background(), &vms))
	require.Len(t, vms.Items, 1)
	vm := vms.Items[0]
	require.NotEmpty(t, vm.Spec.RunnerConfigSecret)

	var sec corev1.Secret
	require.NoError(t, r.Get(context.Background(),
		types.NamespacedName{Namespace: "ns", Name: vm.Spec.RunnerConfigSecret}, &sec))
	require.Equal(t, []byte("ENCODED"), sec.Data[runner.JITConfigSecretKey])
	require.Equal(t, "pool-abc12", sec.Annotations[runner.JITConfigRunnerAnnotation])
	require.Equal(t, runner.JITConfigVersionV1, sec.Annotations[runner.JITConfigVersionAnnotation])
	require.True(t, metav1.IsControlledBy(&sec, &vm))
}

func TestCreateRunnerVMMintsOneJITConfigPerVM(t *testing.T) {
	pool, tpl := runnerPoolFixture()
	drv := &fakeJITDriver{jit: &runner.JITConfig{EncodedConfig: "ENCODED", RunnerName: "pool-runner"}}
	r := jitReconciler(t, drv, pool, tpl)

	require.NoError(t, r.createRunnerVM(context.Background(), pool, tpl))
	require.NoError(t, r.createRunnerVM(context.Background(), pool, tpl))
	require.Equal(t, 2, drv.mint)

	var vms impv1alpha1.ImpVMList
	require.NoError(t, r.List(context.Background(), &vms))
	require.Len(t, vms.Items, 2)
	secrets := map[string]struct{}{}
	for _, vm := range vms.Items {
		require.NotEmpty(t, vm.Spec.RunnerConfigSecret)
		secrets[vm.Spec.RunnerConfigSecret] = struct{}{}
	}
	require.Len(t, secrets, 2)
}

func TestCreateRunnerVMMintErrorReturnsError(t *testing.T) {
	pool, tpl := runnerPoolFixture()
	drv := &fakeJITDriver{err: context.DeadlineExceeded}
	r := jitReconciler(t, drv, pool, tpl)

	err := r.createRunnerVM(context.Background(), pool, tpl)
	require.Error(t, err)
	// An ambiguous/failed mint must not leave a bootable VM without a handoff.
	var vms impv1alpha1.ImpVMList
	require.NoError(t, r.List(context.Background(), &vms))
	require.Empty(t, vms.Items)
}
