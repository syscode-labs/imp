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
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/syscode-labs/imp/internal/agent/network"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

// Attachment event reasons.
const (
	EventReasonAttachmentAuthorized = "AttachmentAuthorized"
	EventReasonAttachmentDenied     = "AttachmentDenied"
)

// Denial reasons recorded on the Authorized condition.
const (
	DenyReasonDefinitionMissing = "DefinitionMissing"
	DenyReasonNodeBindingMiss   = "NodeBindingMissing"
	DenyReasonSubjectNotAllowed = "SubjectNotAllowed"
)

// ImpNetworkAttachmentReconciler authorizes ImpNetworkAttachments against the
// cluster allowlist and the referenced VM's assigned node. It never touches
// host networking; the node agent provisions and tears down host resources for
// attachments whose Authorized condition it observes on its own node.
type ImpNetworkAttachmentReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=imp.dev,resources=impnetworkattachments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=imp.dev,resources=impnetworkattachments/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=imp.dev,resources=clusterimpconfigs,verbs=get;list;watch
// +kubebuilder:rbac:groups=imp.dev,resources=clusterimpnodeprofiles,verbs=get;list;watch

// Reconcile moves an ImpNetworkAttachment to Authorized or Denied. It is safe
// to run repeatedly: authorization is revalidated on every pass so deleted
// definitions or changed bindings are caught.
func (r *ImpNetworkAttachmentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx).WithValues("attachment", req.NamespacedName)

	att := &impdevv1alpha1.ImpNetworkAttachment{}
	if err := r.Get(ctx, req.NamespacedName, att); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	vm := &impdevv1alpha1.ImpVM{}
	err := r.Get(ctx, client.ObjectKey{Namespace: att.Namespace, Name: att.Spec.VMRef.Name}, vm)
	switch {
	case client.IgnoreNotFound(err) != nil:
		return ctrl.Result{}, err
	case err != nil:
		return r.decide(ctx, att, decision{
			phase:   impdevv1alpha1.AttachmentPhasePending,
			reason:  "WaitingForVM",
			message: "Referenced ImpVM does not exist yet",
		})
	case vm.Status.NodeName == "":
		return r.decide(ctx, att, decision{
			phase:   impdevv1alpha1.AttachmentPhasePending,
			reason:  "WaitingForSchedule",
			message: "Referenced ImpVM is not scheduled yet",
		})
	}
	nodeName := vm.Status.NodeName

	def := r.resolveDefinition(ctx, att.Spec.AttachmentRef)
	if def == nil {
		return r.decide(ctx, att, decision{
			node:    nodeName,
			phase:   impdevv1alpha1.AttachmentPhaseDenied,
			reason:  DenyReasonDefinitionMissing,
			message: "Attachment definition " + att.Spec.AttachmentRef + " is missing from ClusterImpConfig",
		})
	}

	if !def.Permits(attachmentRequester(att), attachmentRequesterGroups(att)) {
		return r.decide(ctx, att, decision{
			node:    nodeName,
			phase:   impdevv1alpha1.AttachmentPhaseDenied,
			reason:  DenyReasonSubjectNotAllowed,
			message: "Requester is not in the definition subject allowlist",
		})
	}

	if _, ok := r.resolveBinding(ctx, nodeName, def.Name); !ok {
		return r.decide(ctx, att, decision{
			node:    nodeName,
			phase:   impdevv1alpha1.AttachmentPhaseDenied,
			reason:  DenyReasonNodeBindingMiss,
			message: "Node profile for " + nodeName + " has no LAN binding for definition " + def.Name,
		})
	}

	assignedIP := ""
	if att.Spec.DHCP == nil || !att.Spec.DHCP.Enabled {
		assignedIP = att.Spec.IP // DHCP leases are observed later via guest reporting
	}
	d := decision{
		node:       nodeName,
		phase:      impdevv1alpha1.AttachmentPhaseAuthorized,
		reason:     "Allowed",
		message:    "Attachment authorized on node " + nodeName,
		macAddr:    network.MACAddr(vm.Namespace + "/" + vm.Name),
		assignedIP: assignedIP,
	}
	log.V(1).Info("Authorization evaluated", "phase", d.phase)
	return r.decide(ctx, att, d)
}

// decision captures a reconciliation outcome before it is written to status.
type decision struct {
	node       string
	phase      string
	reason     string
	message    string
	macAddr    string
	assignedIP string
}

// decide writes the decision into status/Events when something changed.
func (r *ImpNetworkAttachmentReconciler) decide(ctx context.Context, att *impdevv1alpha1.ImpNetworkAttachment, d decision) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	orig := att.DeepCopy()

	changed := false
	if att.Status.Requester == "" && attachmentRequester(att) != "" {
		att.Status.Requester = attachmentRequester(att)
		changed = true
	}
	if d.node != "" && att.Status.Node != d.node {
		att.Status.Node = d.node
		changed = true
	}
	if d.macAddr != "" && att.Status.MACAddress != d.macAddr {
		att.Status.MACAddress = d.macAddr
		changed = true
	}
	if att.Status.AssignedIP == "" && d.assignedIP != "" {
		att.Status.AssignedIP = d.assignedIP
		changed = true
	}
	setAttachmentCondition(att, d.phase, d.reason, d.message)
	if attachmentConditionChanged(orig, att) || changed {
		changed = true
	}
	if !changed {
		return ctrl.Result{}, nil
	}

	prevPhase := orig.Status.Phase
	att.Status.Phase = d.phase
	if err := r.Status().Update(ctx, att); err != nil {
		return ctrl.Result{}, err
	}

	if r.Recorder != nil && prevPhase != d.phase {
		switch d.phase {
		case impdevv1alpha1.AttachmentPhaseAuthorized:
			r.Recorder.Eventf(att, corev1.EventTypeNormal, EventReasonAttachmentAuthorized,
				"Authorized attachment %q for VM %q on node %q", att.Name, att.Spec.VMRef.Name, d.node)
		case impdevv1alpha1.AttachmentPhaseDenied:
			r.Recorder.Eventf(att, corev1.EventTypeWarning, EventReasonAttachmentDenied,
				"Denied attachment %q: %s", att.Name, d.message)
		}
	}
	log.Info("ImpNetworkAttachment state updated", "phase", d.phase, "reason", d.reason)
	return ctrl.Result{}, nil
}

// resolveDefinition returns the allowlisted definition by name, or nil.
func (r *ImpNetworkAttachmentReconciler) resolveDefinition(ctx context.Context, name string) *impdevv1alpha1.LANAttachmentSpec {
	cfg := &impdevv1alpha1.ClusterImpConfig{}
	if err := r.Get(ctx, client.ObjectKey{Name: "cluster"}, cfg); err != nil {
		return nil
	}
	for i := range cfg.Spec.Networking.LANAttachments {
		if cfg.Spec.Networking.LANAttachments[i].Name == name {
			return &cfg.Spec.Networking.LANAttachments[i]
		}
	}
	return nil
}

// resolveBinding returns the parent-interface binding for definition on node.
func (r *ImpNetworkAttachmentReconciler) resolveBinding(ctx context.Context, nodeName, defName string) (*impdevv1alpha1.NodeLANBinding, bool) {
	profile := &impdevv1alpha1.ClusterImpNodeProfile{}
	if err := r.Get(ctx, client.ObjectKey{Name: nodeName}, profile); err != nil {
		return nil, false
	}
	for i := range profile.Spec.LANBindings {
		if profile.Spec.LANBindings[i].AttachmentName == defName {
			return &profile.Spec.LANBindings[i], true
		}
	}
	return nil, false
}

// SetupWithManager registers the reconciler plus watches that re-trigger
// authorization when the referenced VM or the allowlist changes.
func (r *ImpNetworkAttachmentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&impdevv1alpha1.ImpNetworkAttachment{}).
		Watches(&impdevv1alpha1.ImpVM{}, handler.EnqueueRequestsFromMapFunc(attachmentsForVM(r.Client))).
		Watches(&impdevv1alpha1.ClusterImpConfig{}, handler.EnqueueRequestsFromMapFunc(attachmentsForAllowlist(r.Client))).
		Named("impnetworkattachment").
		Complete(r)
}

// attachmentsForVM maps an ImpVM change to attachments referencing it.
func attachmentsForVM(c client.Client) func(ctx context.Context, obj client.Object) []reconcile.Request {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		list := &impdevv1alpha1.ImpNetworkAttachmentList{}
		if err := c.List(ctx, list, client.InNamespace(obj.GetNamespace())); err != nil {
			return nil
		}
		var reqs []reconcile.Request
		for _, att := range list.Items {
			if att.Spec.VMRef.Name == obj.GetName() {
				reqs = append(reqs, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&att)})
			}
		}
		return reqs
	}
}

// attachmentsForAllowlist maps a ClusterImpConfig change to all attachments.
func attachmentsForAllowlist(c client.Client) func(ctx context.Context, obj client.Object) []reconcile.Request {
	return func(ctx context.Context, _ client.Object) []reconcile.Request {
		list := &impdevv1alpha1.ImpNetworkAttachmentList{}
		if err := c.List(ctx, list); err != nil {
			return nil
		}
		reqs := make([]reconcile.Request, 0, len(list.Items))
		for i := range list.Items {
			reqs = append(reqs, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&list.Items[i])})
		}
		return reqs
	}
}

// attachmentRequester reads the admission-stamped requester annotation.
func attachmentRequester(att *impdevv1alpha1.ImpNetworkAttachment) string {
	return att.Annotations[impdevv1alpha1.AnnotationRequester]
}

// attachmentRequesterGroups splits the admission-stamped groups annotation.
func attachmentRequesterGroups(att *impdevv1alpha1.ImpNetworkAttachment) []string {
	raw := att.Annotations[impdevv1alpha1.AnnotationRequesterGroups]
	if raw == "" {
		return nil
	}
	return strings.Split(raw, ",")
}

// setAttachmentCondition upserts the Authorized condition from a phase.
func setAttachmentCondition(att *impdevv1alpha1.ImpNetworkAttachment, phase, reason, message string) {
	status := metav1.ConditionFalse
	switch phase {
	case impdevv1alpha1.AttachmentPhaseAuthorized:
		status = metav1.ConditionTrue
	case impdevv1alpha1.AttachmentPhasePending:
		status = metav1.ConditionUnknown
	}
	apimeta.SetStatusCondition(&att.Status.Conditions, metav1.Condition{
		Type:               impdevv1alpha1.ConditionAttachmentAuthorized,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: att.Generation,
		LastTransitionTime: metav1.NewTime(time.Now()),
	})
}

// attachmentConditionChanged reports whether the Authorized condition differs
// between the two snapshots.
func attachmentConditionChanged(oldAtt, newAtt *impdevv1alpha1.ImpNetworkAttachment) bool {
	oldC := apimeta.FindStatusCondition(oldAtt.Status.Conditions, impdevv1alpha1.ConditionAttachmentAuthorized)
	newC := apimeta.FindStatusCondition(newAtt.Status.Conditions, impdevv1alpha1.ConditionAttachmentAuthorized)
	if oldC == nil || newC == nil {
		return oldC != newC
	}
	return oldC.Status != newC.Status || oldC.Reason != newC.Reason || oldC.Message != newC.Message
}
