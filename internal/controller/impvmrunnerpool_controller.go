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
	"strings"
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

// runnerJITMinter is the subset of runner.PlatformDriver needed to register a
// runner VM. runnerQueueDepthReader already embeds the rest of the usage
// surface; drivers returned by the factory satisfy both.
type runnerJITMinter interface {
	GetJITConfig(ctx context.Context) (*runner.JITConfig, error)
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
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;delete

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

	// Delete terminal VMs - runners are single-use.
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
			log.Info("Deleted terminal runner VM", "vm", vm.Name, "phase", vm.Status.Phase)
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
			// skip - just deleted
		default:
			activeCount++
		}
	}

	scaling := resolveRunnerScaling(pool)
	desiredCount := scaling.minIdle

	queueDepth, err := r.queueDepth(ctx, pool, scaling.pollingEnabled)
	if err != nil {
		log.Info("Could not fetch runner queue depth; falling back to current desired", "pool", pool.Name, "err", err)
	} else if int32(queueDepth) > desiredCount { //nolint:gosec
		desiredCount = int32(queueDepth) //nolint:gosec
	}
	if webhookDemand := runnerDemandFromAnnotation(pool, scaling.webhookEnabled); webhookDemand > desiredCount {
		desiredCount = webhookDemand
	}

	toCreate := desiredCount - activeCount
	if toCreate < 0 {
		toCreate = 0
	}
	available := scaling.maxConcurrent - activeCount
	if available < 0 {
		available = 0
	}
	if toCreate > available {
		toCreate = available
	}
	if toCreate > scaling.scaleUpStep {
		toCreate = scaling.scaleUpStep
	}

	for i := int32(0); i < toCreate; i++ {
		if err := r.createRunnerVM(ctx, pool, tpl); err != nil {
			return ctrl.Result{}, err
		}
	}
	if toCreate > 0 {
		log.Info("Created runner VMs", "pool", pool.Name, "count", toCreate)
	}

	// Patch status.
	base := pool.DeepCopy()
	pool.Status.ActiveCount = activeCount
	pool.Status.IdleCount = 0 // per-VM idle tracking deferred; idleCount cannot be determined without runner state
	if err := r.Status().Patch(ctx, pool, client.MergeFrom(base)); err != nil {
		return ctrl.Result{}, err
	}

	requeueAfter := scaling.cooldown
	if scaling.pollingEnabled && scaling.pollingInterval > 0 {
		requeueAfter = time.Duration(scaling.pollingInterval) * time.Second
	}
	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

type runnerScalingResolved struct {
	minIdle         int32
	maxConcurrent   int32
	scaleUpStep     int32
	cooldown        time.Duration
	pollingInterval int32
	pollingEnabled  bool
	webhookEnabled  bool
}

func resolveRunnerScaling(pool *impv1alpha1.ImpVMRunnerPool) runnerScalingResolved {
	out := runnerScalingResolved{
		minIdle:       0,
		maxConcurrent: 10,
		scaleUpStep:   10,
		cooldown:      30 * time.Second,
	}

	mode := impv1alpha1.RunnerScalingMode("")
	if s := pool.Spec.Scaling; s != nil {
		mode = s.Mode
		if s.MinIdle != nil {
			out.minIdle = *s.MinIdle
		}
		if s.MaxConcurrent != nil {
			out.maxConcurrent = *s.MaxConcurrent
		}
		if s.ScaleUpStep != nil {
			out.scaleUpStep = *s.ScaleUpStep
		}
		if s.CooldownSeconds != nil {
			out.cooldown = time.Duration(*s.CooldownSeconds) * time.Second
		}
		if s.Polling != nil && s.Polling.IntervalSeconds > 0 {
			out.pollingInterval = s.Polling.IntervalSeconds
		}
	}

	if mode == "" && pool.Spec.JobDetection != nil {
		jd := pool.Spec.JobDetection
		webhook := jd.Webhook != nil && jd.Webhook.Enabled
		polling := jd.Polling != nil && jd.Polling.Enabled
		switch {
		case webhook && polling:
			mode = impv1alpha1.RunnerScalingModeHybrid
		case polling:
			mode = impv1alpha1.RunnerScalingModePolling
		case webhook:
			mode = impv1alpha1.RunnerScalingModeWebhook
		}
		if jd.Polling != nil && jd.Polling.IntervalSeconds > 0 && out.pollingInterval == 0 {
			out.pollingInterval = jd.Polling.IntervalSeconds
		}
	}

	switch mode {
	case impv1alpha1.RunnerScalingModeHybrid:
		out.webhookEnabled = true
		out.pollingEnabled = true
	case impv1alpha1.RunnerScalingModePolling:
		out.pollingEnabled = true
	case impv1alpha1.RunnerScalingModeWebhook:
		out.webhookEnabled = true
	}

	if out.scaleUpStep <= 0 {
		out.scaleUpStep = out.maxConcurrent
	}
	if out.maxConcurrent <= 0 {
		out.maxConcurrent = 1
	}
	if out.pollingEnabled && out.pollingInterval <= 0 {
		out.pollingInterval = 30
	}
	return out
}

func (r *ImpVMRunnerPoolReconciler) queueDepth(
	ctx context.Context,
	pool *impv1alpha1.ImpVMRunnerPool,
	enabled bool,
) (int, error) {
	if !enabled {
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
	if err := r.Create(ctx, vm); err != nil {
		return err
	}
	if err := r.mintRunnerConfig(ctx, pool, vm); err != nil {
		// GitHub minting and Kubernetes persistence are non-atomic. Never leave a
		// VM that may boot without a one-time handoff, and never blind-remint it.
		_ = r.Delete(ctx, vm)
		return err
	}
	return nil
}

// mintRunnerConfig exchanges the pool credential for a one-time JIT config
// and stores it in a Secret owned by vm. It sets vm.Spec.RunnerConfigSecret.
func (r *ImpVMRunnerPoolReconciler) mintRunnerConfig(
	ctx context.Context,
	pool *impv1alpha1.ImpVMRunnerPool,
	vm *impv1alpha1.ImpVM,
) error {
	if vm.Spec.RunnerConfigSecret != "" {
		return r.validateRunnerSecret(ctx, vm, vm.Spec.RunnerConfigSecret)
	}
	secretName := runnerSecretName(vm)
	var existing corev1.Secret
	if err := r.Get(ctx, client.ObjectKey{Namespace: vm.Namespace, Name: secretName}, &existing); err == nil {
		if err := validateRunnerSecretOwner(&existing, vm); err != nil {
			return err
		}
		return r.patchRunnerSecretReference(ctx, vm, secretName)
	} else if !apierrors.IsNotFound(err) {
		return err
	}

	factory := r.DriverFactory
	if factory == nil {
		factory = defaultRunnerDriverFactory
	}
	d, err := factory(ctx, r.Client, pool)
	if err != nil {
		return fmt.Errorf("build platform driver: %w", err)
	}
	minter, ok := d.(runnerJITMinter)
	if !ok {
		return fmt.Errorf("platform driver for %s does not support JIT runner registration", pool.Spec.Platform.Type)
	}
	jit, err := minter.GetJITConfig(ctx)
	if err != nil {
		return fmt.Errorf("mint runner JIT config: %w", err)
	}
	payload, err := jit.MarshalSecretValue()
	if err != nil {
		return fmt.Errorf("serialize runner JIT config: %w", err)
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: vm.Namespace,
			Labels:    map[string]string{impv1alpha1.LabelRunnerPool: pool.Name},
			Annotations: map[string]string{
				runner.JITConfigRunnerAnnotation:  jit.RunnerName,
				runner.JITConfigVersionAnnotation: runner.JITConfigVersionV1,
			},
		},
		Data: map[string][]byte{runner.JITConfigSecretKey: payload},
	}
	if err := ctrl.SetControllerReference(vm, secret, r.Scheme); err != nil {
		return err
	}
	if err := r.Create(ctx, secret); err != nil {
		if apierrors.IsAlreadyExists(err) {
			var recovered corev1.Secret
			if getErr := r.Get(ctx, client.ObjectKeyFromObject(secret), &recovered); getErr != nil {
				return getErr
			}
			if ownerErr := validateRunnerSecretOwner(&recovered, vm); ownerErr != nil {
				return ownerErr
			}
			return r.patchRunnerSecretReference(ctx, vm, secretName)
		}
		return fmt.Errorf("create runner JIT Secret: %w", err)
	}
	return r.patchRunnerSecretReference(ctx, vm, secretName)
}

func runnerSecretName(vm *impv1alpha1.ImpVM) string {
	identity := string(vm.UID)
	if identity == "" {
		identity = vm.Name
	}
	return "impvm-" + strings.ToLower(strings.ReplaceAll(identity, "_", "-")) + "-jit"
}

func validateRunnerSecretOwner(secret *corev1.Secret, vm *impv1alpha1.ImpVM) error {
	for _, ref := range secret.OwnerReferences {
		if ref.Kind == "ImpVM" && ref.Name == vm.Name && ref.UID == vm.UID && ref.Controller != nil && *ref.Controller {
			return nil
		}
	}
	return fmt.Errorf("runner JIT Secret owner does not match VM identity")
}

func (r *ImpVMRunnerPoolReconciler) validateRunnerSecret(ctx context.Context, vm *impv1alpha1.ImpVM, name string) error {
	var secret corev1.Secret
	if err := r.Get(ctx, client.ObjectKey{Namespace: vm.Namespace, Name: name}, &secret); err != nil {
		return fmt.Errorf("runner JIT Secret missing for VM: %w", err)
	}
	return validateRunnerSecretOwner(&secret, vm)
}

func (r *ImpVMRunnerPoolReconciler) patchRunnerSecretReference(ctx context.Context, vm *impv1alpha1.ImpVM, name string) error {
	base := vm.DeepCopy()
	vm.Spec.RunnerConfigSecret = name
	if err := r.Patch(ctx, vm, client.MergeFrom(base)); err != nil {
		return fmt.Errorf("patch runner JIT Secret reference: %w", err)
	}
	return nil
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

	scope, err := platformScope(pool)
	if err != nil {
		return nil, err
	}
	auth, err := runner.ResolvePlatformAuth(pool.Spec.Platform.Type, pool.Spec.Platform.TokenSource, scope, pool.Spec.Platform.RunnerGroup, creds.Data)
	if err != nil {
		return nil, err
	}

	switch auth.Provider {
	case "github-actions":
		if auth.Source == "github_app" {
			appCreds, err := githubAppCredsFromSecret(auth.Credentials)
			if err != nil {
				return nil, err
			}
			return runner.NewGitHubAppDriver(runner.GitHubConfig{
				Authentication: appCreds,
				Scope:          auth.Scope,
				RunnerGroup:    auth.RunnerGroup,
				Labels:         pool.Spec.Labels,
			}, nil)
		}
		log := logf.FromContext(ctx)
		log.Info("runner pool uses deprecated PAT token source; migrate to tokenSource: github_app",
			"pool", pool.Name, "namespace", pool.Namespace)
		return runner.NewGitHubDriverWithGroupAndLabels(string(auth.Credentials["token"]), auth.Scope, auth.RunnerGroup, pool.Spec.Labels, nil)
	case "forgejo":
		return runner.NewForgejoDriver(string(auth.Credentials["token"]), pool.Spec.Platform.ServerURL, auth.Scope, nil)
	case "gitlab":
		return runner.NewGitLabDriver(string(auth.Credentials["token"]), pool.Spec.Platform.ServerURL, auth.Scope, nil)
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

func githubAppCredsFromSecret(data map[string][]byte) (runner.GitHubAppCredentials, error) {
	appID, err := runner.ParseNumericCredential(data, "github-app-id")
	if err != nil {
		return runner.GitHubAppCredentials{}, err
	}
	instID, err := runner.ParseNumericCredential(data, "github-app-installation-id")
	if err != nil {
		return runner.GitHubAppCredentials{}, err
	}
	pemBytes := data["github-app-private-key"]
	if len(pemBytes) == 0 {
		return runner.GitHubAppCredentials{}, &runner.AuthResolutionError{Reason: "required GitHub App credential key is missing"}
	}
	return runner.GitHubAppCredentials{PrivateKeyPEM: string(pemBytes), AppID: appID, Installation: instID}, nil
}

func runnerDemandFromAnnotation(pool *impv1alpha1.ImpVMRunnerPool, enabled bool) int32 {
	if !enabled {
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
