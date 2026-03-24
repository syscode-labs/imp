package controller

import (
	"context"
	"sort"
	"time"

	"github.com/robfig/cron/v3"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	impv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
	"github.com/syscode-labs/imp/internal/tracing"
)

// ImpVMSnapshotReconciler reconciles ImpVMSnapshot objects.
type ImpVMSnapshotReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=imp.dev,resources=impvmsnapshots,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=imp.dev,resources=impvmsnapshots/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=imp.dev,resources=impvmsnapshots/finalizers,verbs=update

var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

func (r *ImpVMSnapshotReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	log := logf.FromContext(ctx)

	snap := &impv1alpha1.ImpVMSnapshot{}
	if err := r.Get(ctx, req.NamespacedName, snap); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	ctx, span := otel.Tracer("imp.operator").Start(ctx, "operator.impvmsnapshot.reconcile",
		trace.WithAttributes(
			attribute.String("snap.name", req.Name),
			attribute.String("snap.namespace", req.Namespace),
		))
	defer func() { tracing.RecordError(span, err); span.End() }()

	// Design rule 1: only reconcile parent objects (no LabelSnapshotParent label).
	if _, isChild := snap.Labels[impv1alpha1.LabelSnapshotParent]; isChild {
		return ctrl.Result{}, nil
	}

	// Set initial phase to Pending if not yet set.
	if snap.Status.Phase == "" {
		base := snap.DeepCopy()
		snap.Status.Phase = "Pending"
		if err := r.Status().Patch(ctx, snap, client.MergeFrom(base)); err != nil {
			return ctrl.Result{}, err
		}
		log.Info("ImpVMSnapshot created, set to Pending", "name", snap.Name)
	}

	// List all child executions.
	childList := &impv1alpha1.ImpVMSnapshotList{}
	if err := r.List(ctx, childList,
		client.InNamespace(snap.Namespace),
		client.MatchingLabels{impv1alpha1.LabelSnapshotParent: snap.Name},
	); err != nil {
		return ctrl.Result{}, err
	}
	children := childList.Items

	// Design rule 2: serialization gate — if any child has TerminatedAt == nil, requeue.
	for i := range children {
		if children[i].Status.TerminatedAt == nil {
			log.Info("Active child exists, requeueing", "parent", snap.Name)
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}
	}

	// Design rule 5: retention pruning — sort by creationTimestamp ascending, prune oldest beyond retention.
	{
		retCtx, retSpan := otel.Tracer("imp.operator").Start(ctx, "operator.impvmsnapshot.retention",
			trace.WithAttributes(
				attribute.String("snap.name", req.Name),
				attribute.String("snap.namespace", req.Namespace),
			))
		retention := int(snap.Spec.Retention)
		if retention == 0 {
			retention = 3 // default
		}
		if len(children) > retention {
			sort.Slice(children, func(i, j int) bool {
				return children[i].CreationTimestamp.Before(&children[j].CreationTimestamp)
			})
			toDelete := children[:len(children)-retention]
			for i := range toDelete {
				// Design rule 5: never delete the baseSnapshot child.
				if snap.Spec.BaseSnapshot != "" && toDelete[i].Name == snap.Spec.BaseSnapshot {
					continue
				}
				if err := r.Delete(retCtx, &toDelete[i]); client.IgnoreNotFound(err) != nil {
					tracing.RecordError(retSpan, err)
					retSpan.End()
					return ctrl.Result{}, err
				}
				log.Info("Pruned old child", "parent", snap.Name, "child", toDelete[i].Name)
			}
			// Re-list from API server to get an accurate post-prune view (accounts for
			// baseSnapshot children that were skipped during deletion).
			postPrune := &impv1alpha1.ImpVMSnapshotList{}
			if err := r.List(retCtx, postPrune,
				client.InNamespace(snap.Namespace),
				client.MatchingLabels{impv1alpha1.LabelSnapshotParent: snap.Name},
			); err != nil {
				tracing.RecordError(retSpan, err)
				retSpan.End()
				return ctrl.Result{}, err
			}
			children = postPrune.Items
		}
		retSpan.End()
	}

	// Design rule 6: BaseSnapshot validation.
	if snap.Spec.BaseSnapshot != "" {
		for i := range children {
			if children[i].Name == snap.Spec.BaseSnapshot &&
				children[i].Status.Phase == "Succeeded" {
				if snap.Status.BaseSnapshot != snap.Spec.BaseSnapshot {
					base := snap.DeepCopy()
					snap.Status.BaseSnapshot = snap.Spec.BaseSnapshot
					if err := r.Status().Patch(ctx, snap, client.MergeFrom(base)); err != nil {
						return ctrl.Result{}, err
					}
					log.Info("Set status.baseSnapshot", "parent", snap.Name, "child", snap.Spec.BaseSnapshot)
				}
				break
			}
		}
	}

	// Design rule 3 & 4: decide whether to create a new child.
	if snap.Spec.Schedule == "" {
		// One-shot: create child only if no children exist at all.
		if len(children) == 0 {
			if err := r.createChild(ctx, snap); err != nil {
				return ctrl.Result{}, err
			}
		}
		// If children exist and all terminated, do nothing.
		return ctrl.Result{}, nil
	}

	// Scheduled: parse cron and decide whether to create.
	sched, err := cronParser.Parse(snap.Spec.Schedule)
	if err != nil {
		log.Error(err, "Invalid cron schedule", "schedule", snap.Spec.Schedule)
		return ctrl.Result{}, nil // non-retriable config error
	}
	now := time.Now()
	next := sched.Next(now)
	untilNext := time.Until(next)

	// If next tick is within 5s of now, create a child.
	if untilNext <= 5*time.Second {
		cronCtx, cronSpan := otel.Tracer("imp.operator").Start(ctx, "operator.impvmsnapshot.cron_trigger",
			trace.WithAttributes(
				attribute.String("snap.name", req.Name),
				attribute.String("snap.namespace", req.Namespace),
			))
		cronErr := r.createChild(cronCtx, snap)
		tracing.RecordError(cronSpan, cronErr)
		cronSpan.End()
		if cronErr != nil {
			return ctrl.Result{}, cronErr
		}
		// Requeue after the next scheduled slot.
		now = time.Now()
		next = sched.Next(now)
		untilNext = time.Until(next)
	}

	// Update nextScheduledAt in status.
	nextTime := metav1.NewTime(next)
	if snap.Status.NextScheduledAt == nil || !snap.Status.NextScheduledAt.Equal(&nextTime) {
		base := snap.DeepCopy()
		snap.Status.NextScheduledAt = &nextTime
		if err := r.Status().Patch(ctx, snap, client.MergeFrom(base)); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{RequeueAfter: untilNext}, nil
}

// createChild creates a new child execution ImpVMSnapshot for the given parent.
func (r *ImpVMSnapshotReconciler) createChild(ctx context.Context, parent *impv1alpha1.ImpVMSnapshot) error {
	log := logf.FromContext(ctx)

	childName := parent.Name + "-" + time.Now().UTC().Format("20060102-150405")

	childSpec := parent.Spec.DeepCopy()
	childSpec.Schedule = "" // clear schedule on child

	child := &impv1alpha1.ImpVMSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name:      childName,
			Namespace: parent.Namespace,
			Labels: map[string]string{
				impv1alpha1.LabelSnapshotParent: parent.Name,
			},
		},
		Spec: *childSpec,
	}

	if err := ctrl.SetControllerReference(parent, child, r.Scheme); err != nil {
		return err
	}

	if err := r.Create(ctx, child); err != nil {
		return err
	}

	log.Info("Created child execution", "parent", parent.Name, "child", childName)

	// Update lastExecutionRef on parent.
	base := parent.DeepCopy()
	parent.Status.LastExecutionRef = &corev1.LocalObjectReference{Name: childName}
	if err := r.Status().Patch(ctx, parent, client.MergeFrom(base)); err != nil {
		return err
	}

	return nil
}

func (r *ImpVMSnapshotReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&impv1alpha1.ImpVMSnapshot{}).
		Owns(&impv1alpha1.ImpVMSnapshot{}).
		Complete(r)
}
