package agent

import (
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ImpVMSnapshotReconciler handles child ImpVMSnapshot execution objects on this node.
// It filters to children whose source VM is scheduled on NodeName.
type ImpVMSnapshotReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	NodeName string
	Driver   VMDriver
}
