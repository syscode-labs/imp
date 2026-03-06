//go:build !linux

package agent

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"
)

// Reconcile is a no-op stub for non-linux builds.
// The real implementation lives in impvmsnapshot_reconciler.go (linux only).
func (r *ImpVMSnapshotReconciler) Reconcile(_ context.Context, _ ctrl.Request) (ctrl.Result, error) {
	return ctrl.Result{}, nil
}

// SetupWithManager is a no-op stub for non-linux builds.
func (r *ImpVMSnapshotReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return nil
}
