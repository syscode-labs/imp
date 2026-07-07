package controller

import (
	"errors"
	"testing"

	"github.com/go-logr/logr"
)

func TestSchedule_singleNodeFits(t *testing.T) {
	nodes := []NodeInfo{{
		NodeName:          "node1",
		VCPUCapacity:      8,
		MemoryMiB:         8192,
		ResidentVCPU:      2,
		ResidentMemoryMiB: 1024,
	}}
	got, err := Schedule(logr.Discard(), 4, 2048, nodes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "node1" {
		t.Errorf("got %q, want %q", got, "node1")
	}
}

func TestSchedule_noFit_returnsUnschedulable(t *testing.T) {
	nodes := []NodeInfo{{
		NodeName:          "node1",
		VCPUCapacity:      4,
		MemoryMiB:         4096,
		ResidentVCPU:      3,
		ResidentMemoryMiB: 4000,
	}}
	_, err := Schedule(logr.Discard(), 2, 200, nodes)
	if !errors.Is(err, ErrUnschedulable) {
		t.Errorf("expected ErrUnschedulable, got %v", err)
	}
}

func TestSchedule_emptyNodeList_returnsUnschedulable(t *testing.T) {
	_, err := Schedule(logr.Discard(), 1, 128, nil)
	if !errors.Is(err, ErrUnschedulable) {
		t.Errorf("expected ErrUnschedulable, got %v", err)
	}
}

func TestSchedule_tieBreak_picksHighestFreeMemory(t *testing.T) {
	nodes := []NodeInfo{
		{NodeName: "node-a", VCPUCapacity: 8, MemoryMiB: 8192, ResidentVCPU: 2, ResidentMemoryMiB: 2048}, // free: 6 cpu, 6144 mem
		{NodeName: "node-b", VCPUCapacity: 8, MemoryMiB: 8192, ResidentVCPU: 2, ResidentMemoryMiB: 1024}, // free: 6 cpu, 7168 mem — wins
		{NodeName: "node-c", VCPUCapacity: 8, MemoryMiB: 8192, ResidentVCPU: 2, ResidentMemoryMiB: 4096}, // free: 6 cpu, 4096 mem
	}
	got, err := Schedule(logr.Discard(), 2, 512, nodes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "node-b" {
		t.Errorf("got %q, want %q (highest free memory)", got, "node-b")
	}
}

func TestSchedule_vcpuConstraintFiltersNode(t *testing.T) {
	nodes := []NodeInfo{
		{NodeName: "small", VCPUCapacity: 4, MemoryMiB: 8192, ResidentVCPU: 3, ResidentMemoryMiB: 0}, // only 1 free VCPU
		{NodeName: "large", VCPUCapacity: 8, MemoryMiB: 8192, ResidentVCPU: 2, ResidentMemoryMiB: 0}, // 6 free VCPUs
	}
	got, err := Schedule(logr.Discard(), 4, 512, nodes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "large" {
		t.Errorf("got %q, want %q", got, "large")
	}
}

func TestSchedule_memoryConstraintFiltersNode(t *testing.T) {
	nodes := []NodeInfo{
		{NodeName: "low-mem", VCPUCapacity: 8, MemoryMiB: 2048, ResidentVCPU: 0, ResidentMemoryMiB: 1900}, // only 148 MiB free
		{NodeName: "hi-mem", VCPUCapacity: 8, MemoryMiB: 8192, ResidentVCPU: 0, ResidentMemoryMiB: 1024},  // 7168 MiB free
	}
	got, err := Schedule(logr.Discard(), 1, 512, nodes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "hi-mem" {
		t.Errorf("got %q, want %q", got, "hi-mem")
	}
}
