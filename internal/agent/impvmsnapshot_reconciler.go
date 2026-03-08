//go:build linux

package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
	"github.com/syscode-labs/imp/internal/agent/snapshot"
	"github.com/syscode-labs/imp/internal/tracing"
)

const snapshotTempDirPrefix = "imp-snapshot-"

func (r *ImpVMSnapshotReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	log := logf.FromContext(ctx)

	snap := &impdevv1alpha1.ImpVMSnapshot{}
	if err = r.Get(ctx, req.NamespacedName, snap); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Only handle child executions (those with the parent label set).
	if snap.Labels[impdevv1alpha1.LabelSnapshotParent] == "" {
		return ctrl.Result{}, nil
	}

	// Already terminal — nothing to do.
	if snap.Status.TerminatedAt != nil {
		return ctrl.Result{}, nil
	}

	// Resolve source VM and verify it is on this node.
	vm := &impdevv1alpha1.ImpVM{}
	if err = r.Get(ctx, client.ObjectKey{
		Namespace: snap.Spec.SourceVMNamespace,
		Name:      snap.Spec.SourceVMName,
	}, vm); err != nil {
		if client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, err
		}
		// VM deleted — terminal failure, unblock next execution.
		now := metav1.Now()
		base := snap.DeepCopy()
		snap.Status.Phase = "Failed"
		snap.Status.TerminatedAt = &now
		return ctrl.Result{}, r.Status().Patch(ctx, snap, client.MergeFrom(base))
	}
	if vm.Spec.NodeName != r.NodeName {
		return ctrl.Result{}, nil
	}

	ctx, span := otel.Tracer("imp.agent").Start(ctx, "agent.impvm.snapshot",
		trace.WithAttributes(
			attribute.String("vm.name", snap.Spec.SourceVMName),
			attribute.String("vm.namespace", snap.Spec.SourceVMNamespace),
		),
	)
	defer func() {
		tracing.RecordError(span, err)
		span.End()
	}()

	// Mark Running.
	if snap.Status.Phase != "Running" {
		base := snap.DeepCopy()
		snap.Status.Phase = "Running"
		if err = r.Status().Patch(ctx, snap, client.MergeFrom(base)); err != nil {
			return ctrl.Result{}, err
		}
	}

	execResult, termErr := r.executeSnapshot(ctx, snap, vm)

	// Always set TerminatedAt — even on failure — after all cleanup is complete.
	now := metav1.Now()
	base := snap.DeepCopy()
	snap.Status.TerminatedAt = &now
	if termErr != nil {
		snap.Status.Phase = "Failed"
		log.Error(termErr, "snapshot execution failed", "snap", req.NamespacedName)
	} else {
		snap.Status.Phase = "Succeeded"
		snap.Status.CompletedAt = &now
		if snap.Spec.Storage.Type == "oci-registry" {
			snap.Status.Digest = execResult.digest
		} else {
			snap.Status.SnapshotPath = execResult.path
		}
	}
	return ctrl.Result{}, r.Status().Patch(ctx, snap, client.MergeFrom(base))
}

type snapExecResult struct {
	path   string
	digest string
}

func (r *ImpVMSnapshotReconciler) executeSnapshot(ctx context.Context, snap *impdevv1alpha1.ImpVMSnapshot, vm *impdevv1alpha1.ImpVM) (snapExecResult, error) {
	tmpDir, err := os.MkdirTemp("", snapshotTempDirPrefix)
	if err != nil {
		return snapExecResult{}, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir) //nolint:errcheck

	sr, err := r.Driver.Snapshot(ctx, vm, tmpDir)
	if err != nil {
		return snapExecResult{}, fmt.Errorf("driver snapshot: %w", err)
	}

	switch snap.Spec.Storage.Type {
	case "node-local":
		return r.storeNodeLocal(snap, sr)
	case "oci-registry":
		return r.pushOCI(ctx, snap, vm, sr)
	default:
		return snapExecResult{}, fmt.Errorf("unknown storage type %q", snap.Spec.Storage.Type)
	}
}

func (r *ImpVMSnapshotReconciler) storeNodeLocal(snap *impdevv1alpha1.ImpVMSnapshot, sr SnapshotResult) (snapExecResult, error) {
	basePath := "/var/lib/imp/snapshots"
	if snap.Spec.Storage.NodeLocal != nil && snap.Spec.Storage.NodeLocal.Path != "" {
		basePath = snap.Spec.Storage.NodeLocal.Path
	}
	destDir := filepath.Join(basePath, snap.Namespace, snap.Labels[impdevv1alpha1.LabelSnapshotParent], snap.Name)
	if err := os.MkdirAll(destDir, 0o750); err != nil {
		return snapExecResult{}, fmt.Errorf("mkdir %s: %w", destDir, err)
	}
	for _, src := range []string{sr.StatePath, sr.MemPath} {
		dest := filepath.Join(destDir, filepath.Base(src))
		if err := os.Rename(src, dest); err != nil {
			return snapExecResult{}, fmt.Errorf("move %s: %w", src, err)
		}
	}
	return snapExecResult{path: destDir}, nil
}

func (r *ImpVMSnapshotReconciler) pushOCI(ctx context.Context, snap *impdevv1alpha1.ImpVMSnapshot, vm *impdevv1alpha1.ImpVM, sr SnapshotResult) (snapExecResult, error) {
	oci := snap.Spec.Storage.OCIRegistry
	if oci == nil {
		return snapExecResult{}, fmt.Errorf("oci-registry storage requires spec.storage.ociRegistry")
	}
	tag := fmt.Sprintf("%s-%s-%s", snap.Namespace, vm.Name, time.Now().UTC().Format("20060102-1504"))
	digest, err := snapshot.PushOCI(ctx, sr.StatePath, sr.MemPath, oci.Repository, tag, oci.PullSecretRef)
	if err != nil {
		return snapExecResult{}, fmt.Errorf("OCI push: %w", err)
	}
	return snapExecResult{digest: digest}, nil
}

func (r *ImpVMSnapshotReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&impdevv1alpha1.ImpVMSnapshot{}).
		Named("agent-impvmsnapshot").
		Complete(r)
}
