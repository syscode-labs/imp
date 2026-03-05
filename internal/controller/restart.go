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
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

// effectiveMaxRetries returns the configured MaxRetries, defaulting to 5 when the
// field is zero (kubebuilder default may not apply at runtime).
func effectiveMaxRetries(policy *impdevv1alpha1.RestartPolicy) int32 {
	if policy == nil {
		return 0
	}
	if policy.Backoff.MaxRetries == 0 {
		return 5
	}
	return policy.Backoff.MaxRetries
}

// shouldRestart returns true when the VM should be restarted according to policy.
// restartCount is the number of restarts already performed (pre-increment).
func shouldRestart(policy *impdevv1alpha1.RestartPolicy, restartCount int32) bool {
	if policy == nil {
		return false
	}
	return restartCount < effectiveMaxRetries(policy)
}

// computeBackoffDelay returns the wait duration before the next restart attempt.
// delay = min(initialDelay * 2^restartCount, maxDelay)
func computeBackoffDelay(backoff impdevv1alpha1.RestartBackoff, restartCount int32) time.Duration {
	initial := parseDurationOrDefault(backoff.InitialDelay, 10*time.Second)
	max := parseDurationOrDefault(backoff.MaxDelay, 5*time.Minute)

	delay := initial
	for i := int32(0); i < restartCount; i++ {
		delay *= 2
		if delay > max {
			delay = max
			break
		}
	}
	return delay
}

func parseDurationOrDefault(s string, def time.Duration) time.Duration {
	if s == "" {
		return def
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return def
	}
	return d
}

// handleFailed processes a VM in the Failed phase, applying restart policy if set.
//
// Note: VMPhaseFailed is only reachable after a node was assigned and the VM started.
// Spec.NodeName is therefore always set when this function is called.
func (r *ImpVMReconciler) handleFailed(ctx context.Context, vm *impdevv1alpha1.ImpVM) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	policy := vm.Spec.RestartPolicy

	if !shouldRestart(policy, vm.Status.RestartCount) {
		// Exhausted — apply onExhaustion behaviour
		if policy != nil && policy.OnExhaustion == "manual-reset" {
			if vm.Status.Phase != impdevv1alpha1.VMPhaseRetryExhausted {
				base := vm.DeepCopy()
				now := metav1.Now()
				vm.Status.Phase = impdevv1alpha1.VMPhaseRetryExhausted
				vm.Status.ExhaustedAt = &now
				return ctrl.Result{}, r.Status().Patch(ctx, vm, client.MergeFrom(base))
			}
		}
		// TODO(Task 4): cool-down onExhaustion not yet handled
		return ctrl.Result{}, nil // stay Failed (or RetryExhausted)
	}

	delay := computeBackoffDelay(policy.Backoff, vm.Status.RestartCount)
	nextRetry := metav1.NewTime(time.Now().Add(delay))

	// Check if it's time yet
	if vm.Status.NextRetryAfter != nil && time.Now().Before(vm.Status.NextRetryAfter.Time) {
		remaining := time.Until(vm.Status.NextRetryAfter.Time)
		log.V(1).Info("waiting for restart window", "vm", vm.Name, "remaining", remaining)
		return ctrl.Result{RequeueAfter: remaining}, nil
	}

	log.Info("restarting VM", "vm", vm.Name, "restartCount", vm.Status.RestartCount,
		"mode", policy.Mode, "nextDelay", delay)

	base := vm.DeepCopy()
	vm.Status.Phase = impdevv1alpha1.VMPhasePending
	vm.Status.RestartCount++
	vm.Status.NextRetryAfter = &nextRetry

	if err := r.Status().Patch(ctx, vm, client.MergeFrom(base)); err != nil {
		return ctrl.Result{}, err
	}

	// For reschedule mode, clear NodeName so scheduler re-runs
	if policy.Mode == "reschedule" {
		specBase := vm.DeepCopy()
		vm.Spec.NodeName = ""
		if err := r.Patch(ctx, vm, client.MergeFrom(specBase)); err != nil {
			return ctrl.Result{}, err
		}
	}

	r.Recorder.Eventf(vm, corev1.EventTypeNormal, "Restarting",
		"Restarting VM (attempt %d/%d, delay %s)", vm.Status.RestartCount,
		effectiveMaxRetries(policy), delay)

	return ctrl.Result{RequeueAfter: delay}, nil
}
