package controller

import (
	"time"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

func setCondition(vm *impdevv1alpha1.ImpVM, condType string, status metav1.ConditionStatus, reason, message string) {
	apimeta.SetStatusCondition(&vm.Status.Conditions, metav1.Condition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.NewTime(time.Now()),
	})
}

func setScheduled(vm *impdevv1alpha1.ImpVM, nodeName string) {
	setCondition(vm, ConditionScheduled, metav1.ConditionTrue, "NodeAssigned",
		"VM scheduled to node "+nodeName)
}

func setUnscheduled(vm *impdevv1alpha1.ImpVM) {
	setCondition(vm, ConditionScheduled, metav1.ConditionFalse, "NoNodeAvailable",
		"No eligible node with available capacity")
}

func setNodeHealthy(vm *impdevv1alpha1.ImpVM) {
	setCondition(vm, ConditionNodeHealthy, metav1.ConditionTrue, "NodeReady", "Assigned node is Ready")
}

func setNodeUnhealthy(vm *impdevv1alpha1.ImpVM, reason string) {
	setCondition(vm, ConditionNodeHealthy, metav1.ConditionFalse, "NodeNotReady", reason)
}

func setReadyFromPhase(vm *impdevv1alpha1.ImpVM) {
	switch vm.Status.Phase {
	case impdevv1alpha1.VMPhaseRunning:
		setCondition(vm, ConditionReady, metav1.ConditionTrue, "Running", "VM is running")
	case impdevv1alpha1.VMPhaseSucceeded:
		setCondition(vm, ConditionReady, metav1.ConditionFalse, "Completed", "VM completed successfully")
	case impdevv1alpha1.VMPhaseFailed:
		setCondition(vm, ConditionReady, metav1.ConditionFalse, "Failed", "VM failed")
	default:
		setCondition(vm, ConditionReady, metav1.ConditionUnknown, "Waiting", "Waiting for VM to start")
	}
}
