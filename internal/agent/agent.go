// Package agent implements the Imp node agent.
// The agent runs on each node as a DaemonSet and owns the Firecracker
// processes for ImpVMs scheduled to that node.
package agent

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"
)

var log = ctrl.Log.WithName("agent")

// Agent manages Firecracker processes on a single node.
type Agent struct {
	NodeName string
}

// Run starts the agent reconcile loop. It blocks until ctx is cancelled.
func (a *Agent) Run(ctx context.Context) error {
	log.Info("agent starting", "node", a.NodeName)
	// TODO: watch ImpVM objects where spec.nodeName == a.NodeName
	// TODO: reconcile Firecracker processes
	<-ctx.Done()
	log.Info("agent stopping", "node", a.NodeName)
	return nil
}
