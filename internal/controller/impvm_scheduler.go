package controller

import (
	"context"
	"sort"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

const labelImpEnabled = "imp/enabled"

// schedule selects a node for vm. Returns "" if no suitable node exists.
func (r *ImpVMReconciler) schedule(ctx context.Context, vm *impdevv1alpha1.ImpVM) (string, error) {
	// 1. List nodes with imp/enabled=true
	nodeList := &corev1.NodeList{}
	if err := r.List(ctx, nodeList, client.MatchingLabels{labelImpEnabled: "true"}); err != nil {
		return "", err
	}

	// 2. Filter by spec.nodeSelector
	eligible := filterByNodeSelector(nodeList.Items, vm.Spec.NodeSelector)
	if len(eligible) == 0 {
		return "", nil
	}

	// 3. Count running VMs per node
	allVMs := &impdevv1alpha1.ImpVMList{}
	if err := r.List(ctx, allVMs); err != nil {
		return "", err
	}
	runningPerNode := countRunningVMs(allVMs.Items)

	// 4. Apply capacity cap from ClusterImpNodeProfile (if present)
	type candidate struct {
		name    string
		running int
	}
	var candidates []candidate
	for _, node := range eligible {
		profile := &impdevv1alpha1.ClusterImpNodeProfile{}
		err := r.Get(ctx, client.ObjectKey{Name: node.Name}, profile)
		if err != nil {
			if !apierrors.IsNotFound(err) {
				// Transient API error — propagate so controller-runtime retries.
				return "", err
			}
			// No profile → no hard cap
			candidates = append(candidates, candidate{name: node.Name, running: runningPerNode[node.Name]})
			continue
		}
		if profile.Spec.MaxImpVMs > 0 && int32(runningPerNode[node.Name]) >= profile.Spec.MaxImpVMs { //nolint:gosec
			continue // at capacity
		}
		candidates = append(candidates, candidate{name: node.Name, running: runningPerNode[node.Name]})
	}

	if len(candidates) == 0 {
		return "", nil
	}

	// 5. Least-loaded first; alphabetical tie-break
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].running != candidates[j].running {
			return candidates[i].running < candidates[j].running
		}
		return candidates[i].name < candidates[j].name
	})
	return candidates[0].name, nil
}

func filterByNodeSelector(nodes []corev1.Node, selector map[string]string) []corev1.Node {
	if len(selector) == 0 {
		return nodes
	}
	var result []corev1.Node
	for _, node := range nodes {
		match := true
		for k, v := range selector {
			if node.Labels[k] != v {
				match = false
				break
			}
		}
		if match {
			result = append(result, node)
		}
	}
	return result
}

// countRunningVMs counts VMs per node that are actively occupying capacity.
// Excludes Failed, Succeeded, and Terminating — all of which are vacating or already gone.
func countRunningVMs(vms []impdevv1alpha1.ImpVM) map[string]int {
	counts := make(map[string]int)
	for _, vm := range vms {
		switch vm.Status.Phase {
		case impdevv1alpha1.VMPhaseFailed,
			impdevv1alpha1.VMPhaseSucceeded,
			impdevv1alpha1.VMPhaseTerminating:
			continue
		}
		if vm.Spec.NodeName != "" {
			counts[vm.Spec.NodeName]++
		}
	}
	return counts
}
