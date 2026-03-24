package controller

import (
	"context"
	"fmt"
	"net/http"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

// resolveHTTPCheck returns the effective HTTPCheckSpec for vm (VM spec overrides global default).
// Returns nil if the HTTP check is disabled for this VM.
func resolveHTTPCheck(vm *impdevv1alpha1.ImpVM, globalDefault *impdevv1alpha1.HTTPCheckSpec) *impdevv1alpha1.HTTPCheckSpec {
	if vm.Spec.Probes != nil && vm.Spec.Probes.HTTPCheck != nil {
		if vm.Spec.Probes.HTTPCheck.Enabled {
			return vm.Spec.Probes.HTTPCheck
		}
		return nil // explicitly disabled at VM level
	}
	if globalDefault != nil && globalDefault.Enabled {
		return globalDefault
	}
	return nil
}

// applyHTTPCheck runs the HTTP probe and updates the Ready condition + emits events.
// Failure count is tracked in the annotation "imp/httpcheck-failures".
// Returns true if vm.Annotations were mutated (so the caller knows to issue a separate spec patch).
func (r *ImpVMReconciler) applyHTTPCheck(ctx context.Context, vm *impdevv1alpha1.ImpVM, spec *impdevv1alpha1.HTTPCheckSpec) bool {
	if vm.Status.IP == "" {
		setReadyFromPhase(vm) // no IP yet, fall back to phase-derived Ready
		return false
	}

	healthy, msg := doHTTPCheck(ctx, vm.Status.IP, spec)

	threshold := spec.FailureThreshold
	if threshold == 0 {
		threshold = 3
	}

	if vm.Annotations == nil {
		vm.Annotations = make(map[string]string)
	}

	prev := vm.Annotations["imp/httpcheck-failures"]

	if healthy {
		wasFailure := prev != "" && prev != "0"
		if wasFailure {
			r.Recorder.Event(vm, corev1.EventTypeNormal, EventReasonHealthCheckRecovered,
				"HTTP probe passing again")
		}
		vm.Annotations["imp/httpcheck-failures"] = "0"
		setCondition(vm, impdevv1alpha1.ConditionReady, metav1.ConditionTrue, "Running", "VM is running and HTTP probe passing")
		return prev != "0" // annotation changed if it wasn't already "0"
	}

	var failures int32
	fmt.Sscan(prev, &failures) //nolint:errcheck
	failures++
	vm.Annotations["imp/httpcheck-failures"] = fmt.Sprintf("%d", failures)

	if failures >= threshold {
		setCondition(vm, impdevv1alpha1.ConditionReady, metav1.ConditionFalse, "HealthCheckFailed", msg)
		r.Recorder.Event(vm, corev1.EventTypeWarning, EventReasonHealthCheckFailed,
			fmt.Sprintf("HTTP probe failed %d consecutive times: %s", failures, msg))
	} else {
		setReadyFromPhase(vm) // not yet at threshold
	}
	return true // failure counter incremented
}

// doHTTPCheck performs a single HTTP GET. Returns (healthy, message).
func doHTTPCheck(ctx context.Context, ip string, spec *impdevv1alpha1.HTTPCheckSpec) (bool, string) {
	path := spec.Path
	if path == "" {
		path = "/healthz"
	}
	url := fmt.Sprintf("http://%s:%d%s", ip, spec.Port, path)

	// Request timeout is fixed at 5s regardless of IntervalSeconds.
	// IntervalSeconds is the *polling* frequency, not a request deadline.
	hc := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, err.Error()
	}
	resp, err := hc.Do(req) //nolint:gosec
	if err != nil {
		return false, err.Error()
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return true, "OK"
	}
	return false, fmt.Sprintf("HTTP %d", resp.StatusCode)
}
