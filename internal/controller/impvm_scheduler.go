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
	"sort"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

const labelImpEnabled = "imp/enabled"

// sumUsedResources returns (usedVCPU, usedMemMiB) per node for active VMs.
// VMs in Failed, Succeeded, or Terminating phase are excluded.
// VMs whose class cannot be resolved are skipped (best-effort).
func sumUsedResources(ctx context.Context, c client.Client, vms []impdevv1alpha1.ImpVM) map[string][2]int64 {
	result := make(map[string][2]int64)
	for _, vm := range vms {
		switch vm.Status.Phase {
		case impdevv1alpha1.VMPhaseFailed,
			impdevv1alpha1.VMPhaseSucceeded,
			impdevv1alpha1.VMPhaseTerminating:
			continue
		}
		if vm.Spec.NodeName == "" {
			continue
		}
		spec, err := resolveClassSpec(ctx, c, &vm)
		if err != nil {
			continue // best-effort
		}
		cur := result[vm.Spec.NodeName]
		cur[0] += int64(spec.VCPU)
		cur[1] += int64(spec.MemoryMiB)
		result[vm.Spec.NodeName] = cur
	}
	return result
}

// schedule selects a node for vm using a capacity-aware least-loaded strategy.
// Returns "" and no error when no suitable node is available.
func (r *ImpVMReconciler) schedule(ctx context.Context, vm *impdevv1alpha1.ImpVM) (string, error) {
	log := logf.FromContext(ctx)

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

	// 2b. Filter out unready / unschedulable nodes
	eligible = filterSchedulable(eligible)
	if len(eligible) == 0 {
		return "", nil
	}

	// 3. Count running VMs per node
	allVMs := &impdevv1alpha1.ImpVMList{}
	if err := r.List(ctx, allVMs); err != nil {
		return "", err
	}
	runningPerNode := countRunningVMs(allVMs.Items)

	// 4. Resolve VM compute class (best-effort: skip capacity check if unresolvable)
	var vmVCPU, vmMemMiB int32
	if classSpec, err := resolveClassSpec(ctx, r.Client, vm); err != nil {
		log.V(1).Info("could not resolve class spec for capacity check; skipping compute limit",
			"vm", vm.Name, "err", err)
	} else {
		vmVCPU = classSpec.VCPU
		vmMemMiB = classSpec.MemoryMiB
	}

	// 5a. Explicit-capacity scheduling: use Schedule() for nodes that have VCPUCapacity set on profile.
	usedResources := sumUsedResources(ctx, r.Client, allVMs.Items)
	var explicitNodes []NodeInfo
	for _, node := range eligible {
		profile := &impdevv1alpha1.ClusterImpNodeProfile{}
		if err := r.Get(ctx, client.ObjectKey{Name: node.Name}, profile); err != nil || profile.Spec.VCPUCapacity == 0 {
			continue
		}
		used := usedResources[node.Name]
		explicitNodes = append(explicitNodes, NodeInfo{
			NodeName:      node.Name,
			VCPUCapacity:  profile.Spec.VCPUCapacity,
			MemoryMiB:     profile.Spec.MemoryMiB,
			UsedVCPU:      int32(used[0]), //nolint:gosec
			UsedMemoryMiB: used[1],
		})
	}
	if len(explicitNodes) > 0 && vmVCPU > 0 {
		chosen, err := Schedule(log, vmVCPU, int64(vmMemMiB), explicitNodes)
		if err == nil {
			return chosen, nil
		}
		// ErrUnschedulable from explicit-capacity nodes — fall through to fraction-based
		// logic for any nodes without explicit profiles.
	}

	// 5. Fetch global default fraction from ClusterImpConfig (best-effort)
	globalFraction := 0.9
	cfg := &impdevv1alpha1.ClusterImpConfig{}
	if err := r.Get(ctx, client.ObjectKey{Name: "cluster"}, cfg); err == nil {
		globalFraction = parseFraction(cfg.Spec.Capacity.DefaultFraction)
	}

	// 6. Apply capacity caps
	type candidate struct {
		name    string
		running int
	}
	var candidates []candidate
	for _, node := range eligible {
		running := runningPerNode[node.Name]

		// Fetch per-node profile (may be absent)
		profile := &impdevv1alpha1.ClusterImpNodeProfile{}
		err := r.Get(ctx, client.ObjectKey{Name: node.Name}, profile)
		if err != nil && !apierrors.IsNotFound(err) {
			return "", err
		}

		// Hard count cap from profile.
		if err == nil && profile.Spec.MaxImpVMs > 0 && int32(running) >= profile.Spec.MaxImpVMs { //nolint:gosec
			continue
		}

		// Compute-based cap (only when class was resolved and node has allocatable).
		if vmVCPU > 0 {
			fraction := globalFraction
			if err == nil && profile.Spec.CapacityFraction != "" {
				fraction = parseFraction(profile.Spec.CapacityFraction)
			}
			allocCPU := node.Status.Allocatable.Cpu().MilliValue()
			allocMem := node.Status.Allocatable.Memory().Value()
			maxVMs := effectiveMaxVMs(allocCPU, allocMem, vmVCPU, vmMemMiB, fraction)
			if int32(running) >= maxVMs { //nolint:gosec
				continue
			}
		}

		candidates = append(candidates, candidate{name: node.Name, running: running})
	}

	if len(candidates) == 0 {
		return "", nil
	}

	// 7. Least-loaded first; alphabetical tie-break
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

// nodeIsSchedulable returns false when the node should not receive new workloads.
// Composes isNodeReady and additionally checks Spec.Unschedulable and pressure conditions.
func nodeIsSchedulable(node corev1.Node) bool {
	if node.Spec.Unschedulable {
		return false
	}
	if !isNodeReady(&node) {
		return false
	}
	for _, c := range node.Status.Conditions {
		switch c.Type {
		case corev1.NodeMemoryPressure, corev1.NodeDiskPressure, corev1.NodePIDPressure:
			if c.Status == corev1.ConditionTrue {
				return false
			}
		}
	}
	return true
}

func filterSchedulable(nodes []corev1.Node) []corev1.Node {
	var result []corev1.Node
	for _, n := range nodes {
		if nodeIsSchedulable(n) {
			result = append(result, n)
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
