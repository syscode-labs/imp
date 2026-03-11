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
	"fmt"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	impv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
	"github.com/syscode-labs/imp/internal/runner"
)

// ImpVMRunnerPoolReconciler reconciles ImpVMRunnerPool objects.
type ImpVMRunnerPoolReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	DriverFactory RunnerDriverFactory
}

type runnerQueueDepthReader interface {
	QueueDepth(ctx context.Context) (int, error)
}

type RunnerDriverFactory func(
	ctx context.Context,
	c client.Client,
	pool *impv1alpha1.ImpVMRunnerPool,
) (runnerQueueDepthReader, error)

const (
	// AnnotationRunnerDemand is an optional immediate demand signal set by a webhook
	// handler or external controller. Value is desired queued jobs as an int.
	AnnotationRunnerDemand = "imp.dev/runner-demand"
)

// +kubebuilder:rbac:groups=imp.dev,resources=impvmrunnerpools,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=imp.dev,resources=impvmrunnerpools/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=imp.dev,resources=impvmtemplates,verbs=get;list;watch
// +kubebuilder:rbac:groups=imp.dev,resources=impvms,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get

func (r *ImpVMRunnerPoolReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	pool := &impv1alpha1.ImpVMRunnerPool{}
	if err := r.Get(ctx, req.NamespacedName, pool); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Fetch template.
	tpl := &impv1alpha1.ImpVMTemplate{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: pool.Namespace, Name: pool.Spec.TemplateName}, tpl); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("ImpVMTemplate not found, requeueing", "template", pool.Spec.TemplateName)
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}
		return ctrl.Result{}, err
	}

	// List pool members.
	vmList := &impv1alpha1.ImpVMList{}
	if err := r.List(ctx, vmList,
		client.InNamespace(pool.Namespace),
		client.MatchingLabels{impv1alpha1.LabelRunnerPool: pool.Name},
	); err != nil {
		return ctrl.Result{}, err
	}

	// Delete terminal VMs — runners are single-use.
	for i := range vmList.Items {
		vm := &vmList.Items[i]
		if vm.DeletionTimestamp != nil {
			continue // already terminating
		}
		switch vm.Status.Phase {
		case impv1alpha1.VMPhaseSucceeded, impv1alpha1.VMPhaseFailed, impv1alpha1.VMPhaseRetryExhausted:
			if err := r.Delete(ctx, vm); err != nil && !apierrors.IsNotFound(err) {
				return ctrl.Result{}, err
			}
			log.Info("deleted terminal runner VM", "vm", vm.Name, "phase", vm.Status.Phase)
		}
	}

	// Re-list: deletion may not yet be reflected in the cache; filter terminal VMs defensively.
	if err := r.List(ctx, vmList,
		client.InNamespace(pool.Namespace),
		client.MatchingLabels{impv1alpha1.LabelRunnerPool: pool.Name},
	); err != nil {
		return ctrl.Result{}, err
	}

	// Count active (non-terminal) VMs.
	var activeCount int32
	for i := range vmList.Items {
		switch vmList.Items[i].Status.Phase {
		case impv1alpha1.VMPhaseSucceeded, impv1alpha1.VMPhaseFailed, impv1alpha1.VMPhaseRetryExhausted:
			// skip — just deleted
		default:
			activeCount++
		}
	}

	// Determine how many VMs to create.
	// When Scaling is nil, apply conservative defaults: no idle VMs (minIdle=0) and
	// a maxConcurrent ceiling of 10. With minIdle=0, no VMs are proactively created;
	// VMs will only be created when a job event triggers a reconcile with toCreate > 0.
	minIdle := int32(0)
	maxConcurrent := int32(10)
	if pool.Spec.Scaling != nil {
		minIdle = pool.Spec.Scaling.MinIdle
		maxConcurrent = pool.Spec.Scaling.MaxConcurrent
	}

	desiredCount := minIdle
	queueDepth, err := r.queueDepth(ctx, pool)
	if err != nil {
		log.Info("could not fetch runner queue depth; falling back to minIdle", "pool", pool.Name, "err", err)
	} else if int32(queueDepth) > desiredCount { //nolint:gosec
		desiredCount = int32(queueDepth) //nolint:gosec
	}
	if webhookDemand := runnerDemandFromAnnotation(pool); webhookDemand > desiredCount {
		desiredCount = webhookDemand
	}

	toCreate := desiredCount - activeCount
	if toCreate < 0 {
		toCreate = 0
	}
	available := maxConcurrent - activeCount
	if available < 0 {
		available = 0
	}
	if toCreate > available {
		toCreate = available
	}

	for i := int32(0); i < toCreate; i++ {
		if err := r.createRunnerVM(ctx, pool, tpl); err != nil {
			return ctrl.Result{}, err
		}
	}
	if toCreate > 0 {
		log.Info("created runner VMs", "pool", pool.Name, "count", toCreate)
	}

	// Patch status.
	base := pool.DeepCopy()
	pool.Status.ActiveCount = activeCount
	pool.Status.IdleCount = 0 // per-VM idle tracking deferred; idleCount cannot be determined without runner state
	if err := r.Status().Patch(ctx, pool, client.MergeFrom(base)); err != nil {
		return ctrl.Result{}, err
	}

	// Requeue.
	requeueAfter := 30 * time.Second
	if pool.Spec.JobDetection != nil &&
		pool.Spec.JobDetection.Polling != nil &&
		pool.Spec.JobDetection.Polling.Enabled &&
		pool.Spec.JobDetection.Polling.IntervalSeconds > 0 {
		requeueAfter = time.Duration(pool.Spec.JobDetection.Polling.IntervalSeconds) * time.Second
	}
	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

func (r *ImpVMRunnerPoolReconciler) queueDepth(ctx context.Context, pool *impv1alpha1.ImpVMRunnerPool) (int, error) {
	if pool.Spec.JobDetection == nil ||
		pool.Spec.JobDetection.Polling == nil ||
		!pool.Spec.JobDetection.Polling.Enabled {
		return 0, nil
	}

	factory := r.DriverFactory
	if factory == nil {
		factory = defaultRunnerDriverFactory
	}
	d, err := factory(ctx, r.Client, pool)
	if err != nil {
		return 0, err
	}
	return d.QueueDepth(ctx)
}

func (r *ImpVMRunnerPoolReconciler) createRunnerVM(ctx context.Context, pool *impv1alpha1.ImpVMRunnerPool, tpl *impv1alpha1.ImpVMTemplate) error {
	classRef := tpl.Spec.ClassRef
	vm := &impv1alpha1.ImpVM{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: pool.Name + "-",
			Namespace:    pool.Namespace,
			Labels:       map[string]string{impv1alpha1.LabelRunnerPool: pool.Name},
		},
		Spec: impv1alpha1.ImpVMSpec{
			ClassRef:  &classRef,
			Image:     tpl.Spec.Image,
			Lifecycle: impv1alpha1.VMLifecycleEphemeral,
		},
	}
	if tpl.Spec.NetworkRef != nil {
		vm.Spec.NetworkRef = tpl.Spec.NetworkRef
	}
	if tpl.Spec.Probes != nil {
		vm.Spec.Probes = tpl.Spec.Probes
	}
	if tpl.Spec.GuestAgent != nil {
		vm.Spec.GuestAgent = tpl.Spec.GuestAgent
	}
	if tpl.Spec.NetworkGroup != "" {
		vm.Spec.NetworkGroup = tpl.Spec.NetworkGroup
	}
	if pool.Spec.RunnerLayer != "" {
		vm.Spec.RunnerLayer = pool.Spec.RunnerLayer
	} else if tpl.Spec.RunnerLayer != "" {
		vm.Spec.RunnerLayer = tpl.Spec.RunnerLayer
	}
	if tpl.Spec.CiliumLayer != "" {
		vm.Spec.CiliumLayer = tpl.Spec.CiliumLayer
	}
	if pool.Spec.ExpireAfter != nil {
		d := *pool.Spec.ExpireAfter
		vm.Spec.ExpireAfter = &d
	} else if tpl.Spec.ExpireAfter != nil {
		d := *tpl.Spec.ExpireAfter
		vm.Spec.ExpireAfter = &d
	}
	if err := ctrl.SetControllerReference(pool, vm, r.Scheme); err != nil {
		return err
	}
	return r.Create(ctx, vm)
}

func (r *ImpVMRunnerPoolReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&impv1alpha1.ImpVMRunnerPool{}).
		Owns(&impv1alpha1.ImpVM{}).
		Complete(r)
}

func defaultRunnerDriverFactory(
	ctx context.Context,
	c client.Client,
	pool *impv1alpha1.ImpVMRunnerPool,
) (runnerQueueDepthReader, error) {
	var creds corev1.Secret
	if err := c.Get(ctx, client.ObjectKey{
		Namespace: pool.Namespace,
		Name:      pool.Spec.Platform.CredentialsSecret,
	}, &creds); err != nil {
		return nil, err
	}
	token := pickSecretValue(creds.Data, "token")
	if token == "" {
		return nil, fmt.Errorf("credentials secret %s/%s has no token value", pool.Namespace, creds.Name)
	}

	scope, err := platformScope(pool)
	if err != nil {
		return nil, err
	}

	switch pool.Spec.Platform.Type {
	case "github-actions":
		return runner.NewGitHubDriver(token, scope, nil)
	case "forgejo":
		return runner.NewForgejoDriver(token, pool.Spec.Platform.ServerURL, scope, nil)
	case "gitlab":
		return runner.NewGitLabDriver(token, pool.Spec.Platform.ServerURL, scope, nil)
	default:
		return nil, fmt.Errorf("unsupported platform type %q", pool.Spec.Platform.Type)
	}
}

func platformScope(pool *impv1alpha1.ImpVMRunnerPool) (string, error) {
	if pool.Spec.Platform.Scope == nil {
		return "", fmt.Errorf("platform.scope is required")
	}
	scope := pool.Spec.Platform.Scope
	switch pool.Spec.Platform.Type {
	case "github-actions", "forgejo":
		if scope.Org != "" {
			return "org:" + scope.Org, nil
		}
		if scope.Repo != "" {
			return "repo:" + scope.Repo, nil
		}
	case "gitlab":
		if scope.Org != "" {
			return "group:" + scope.Org, nil
		}
		if scope.Repo != "" {
			return "project:" + scope.Repo, nil
		}
	}
	return "", fmt.Errorf("invalid platform.scope for type %q", pool.Spec.Platform.Type)
}

func pickSecretValue(m map[string][]byte, preferredKey string) string {
	if len(m) == 0 {
		return ""
	}
	if v, ok := m[preferredKey]; ok && len(v) > 0 {
		return string(v)
	}
	for _, v := range m {
		if len(v) > 0 {
			return string(v)
		}
	}
	return ""
}

func runnerDemandFromAnnotation(pool *impv1alpha1.ImpVMRunnerPool) int32 {
	if pool.Spec.JobDetection == nil ||
		pool.Spec.JobDetection.Webhook == nil ||
		!pool.Spec.JobDetection.Webhook.Enabled {
		return 0
	}
	raw := pool.Annotations[AnnotationRunnerDemand]
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0
	}
	return int32(n) //nolint:gosec
}
