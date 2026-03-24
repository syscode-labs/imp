package controller

import (
	"errors"
	"sort"

	"github.com/go-logr/logr"
)

// ErrUnschedulable is returned by Schedule when no node has sufficient capacity.
var ErrUnschedulable = errors.New("no node has sufficient capacity")

// NodeInfo holds capacity and current load for a candidate node.
type NodeInfo struct {
	NodeName      string
	VCPUCapacity  int32
	MemoryMiB     int64
	UsedVCPU      int32
	UsedMemoryMiB int64
}

// Schedule picks the best-fit node for a VM requiring vcpu vCPUs and memMiB MiB of RAM.
// Selection criteria:
//  1. Filter: freeVCPU >= vcpu AND freeMemMiB >= memMiB
//  2. Log each candidate with fit result at debug level (V(1))
//  3. Tie-break: highest free memory (bin-packing — most-loaded node that still fits)
//
// Returns ("", ErrUnschedulable) when no node has sufficient capacity.
func Schedule(log logr.Logger, vcpu int32, memMiB int64, nodes []NodeInfo) (string, error) {
	type candidate struct {
		name       string
		freeVCPU   int32
		freeMemMiB int64
	}

	var candidates []candidate
	for _, n := range nodes {
		freeVCPU := n.VCPUCapacity - n.UsedVCPU
		freeMemMiB := n.MemoryMiB - n.UsedMemoryMiB
		fits := freeVCPU >= vcpu && freeMemMiB >= memMiB
		log.V(1).Info("Scheduling candidate",
			"node", n.NodeName,
			"freeVCPU", freeVCPU,
			"freeMemMiB", freeMemMiB,
			"required.vcpu", vcpu,
			"required.memMiB", memMiB,
			"fits", fits,
		)
		if fits {
			candidates = append(candidates, candidate{
				name:       n.NodeName,
				freeVCPU:   freeVCPU,
				freeMemMiB: freeMemMiB,
			})
		}
	}

	if len(candidates) == 0 {
		return "", ErrUnschedulable
	}

	// Tie-break: highest free memory (bin-packing).
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].freeMemMiB > candidates[j].freeMemMiB
	})
	return candidates[0].name, nil
}
