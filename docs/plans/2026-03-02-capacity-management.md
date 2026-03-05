# Compute Capacity Management Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make the ImpVM scheduler respect node allocatable CPU/memory when choosing a node, using the `effectiveMax` formula from the design doc.

**Architecture:** Three layers — (1) pure math helpers (`effectiveMaxVMs`, `parseFraction`) with no external deps, (2) a `resolveClassSpec` helper that fetches the `ImpVMClass` for a VM (following either `ClassRef` or `TemplateRef → ClassRef`), and (3) wiring into the existing `schedule` function in `impvm_scheduler.go`. The scheduler fetches `ClusterImpConfig` for the global fraction and `ClusterImpNodeProfile` (already fetched) for per-node overrides. If no config exists, fraction defaults to 0.9.

**Tech Stack:** Go, `k8s.io/apimachinery/pkg/api/resource` (for `resource.Quantity`), standard `testing` package for unit tests, Ginkgo+Gomega+envtest for integration tests (same pattern as existing scheduler tests in `internal/controller/`).

---

### Task 1: effectiveMaxVMs + parseFraction helpers + unit tests

**Files:**
- Create: `internal/controller/capacity.go`
- Create: `internal/controller/capacity_test.go`

**Step 1: Write the failing tests** (`internal/controller/capacity_test.go`)

```go
package controller

import (
	"testing"
)

func TestParseFraction(t *testing.T) {
	cases := []struct {
		in   string
		want float64
	}{
		{"0.9", 0.9},
		{"1.0", 1.0},
		{"0", 0.0},
		{"0.5", 0.5},
		{"", 0.9},          // empty → default
		{"invalid", 0.9},   // bad string → default
		{"1.1", 0.9},       // out of range → default
		{"-0.1", 0.9},      // negative → default
	}
	for _, tc := range cases {
		got := parseFraction(tc.in)
		if got != tc.want {
			t.Errorf("parseFraction(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestEffectiveMaxVMs(t *testing.T) {
	cases := []struct {
		name          string
		allocCPUm     int64   // node allocatable CPU in millicores
		allocMemBytes int64   // node allocatable memory in bytes
		vcpu          int32   // VM vCPUs
		memMiB        int32   // VM memory in MiB
		fraction      float64
		want          int32
	}{
		{
			name: "cpu-bound: 4 CPUs, 0.9 fraction, 1 vcpu VMs",
			// 4000m * 0.9 / 1000m = 3.6 → floor 3
			allocCPUm: 4000, allocMemBytes: 64 * 1024 * 1024 * 1024,
			vcpu: 1, memMiB: 256, fraction: 0.9,
			want: 3,
		},
		{
			name: "memory-bound: 1GiB, 0.9 fraction, 512MiB VMs",
			// cpu: 16000m * 0.9 / 1000m = 14; mem: 1GiB * 0.9 / 512MiB = 1.8 → floor 1
			allocCPUm: 16000, allocMemBytes: 1 * 1024 * 1024 * 1024,
			vcpu: 1, memMiB: 512, fraction: 0.9,
			want: 1,
		},
		{
			name: "both equal: 4 VMs fit by both CPU and memory",
			// cpu: 4000m * 1.0 / 1000m = 4; mem: 4*512MiB * 1.0 / 512MiB = 4
			allocCPUm: 4000, allocMemBytes: 4 * 512 * 1024 * 1024,
			vcpu: 1, memMiB: 512, fraction: 1.0,
			want: 4,
		},
		{
			name: "fraction zero → 0 VMs",
			allocCPUm: 8000, allocMemBytes: 16 * 1024 * 1024 * 1024,
			vcpu: 1, memMiB: 512, fraction: 0.0,
			want: 0,
		},
		{
			name: "VM larger than node → 0",
			// node has 2 CPUs, VM wants 4 → 0
			allocCPUm: 2000, allocMemBytes: 64 * 1024 * 1024 * 1024,
			vcpu: 4, memMiB: 256, fraction: 0.9,
			want: 0,
		},
		{
			name: "8 CPUs 0.9 fraction 2vcpu VMs",
			// 8000m * 0.9 / 2000m = 3.6 → floor 3; mem plenty
			allocCPUm: 8000, allocMemBytes: 64 * 1024 * 1024 * 1024,
			vcpu: 2, memMiB: 512, fraction: 0.9,
			want: 3,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := effectiveMaxVMs(tc.allocCPUm, tc.allocMemBytes, tc.vcpu, tc.memMiB, tc.fraction)
			if got != tc.want {
				t.Errorf("effectiveMaxVMs = %d, want %d", got, tc.want)
			}
		})
	}
}
```

**Step 2: Run tests to verify they fail**

```bash
cd /Users/giovanni/syscode/git/imp
go test ./internal/controller/ -run "TestParseFraction|TestEffectiveMaxVMs" -v
```
Expected: FAIL — `undefined: parseFraction`, `undefined: effectiveMaxVMs`

**Step 3: Create `internal/controller/capacity.go`**

```go
package controller

import (
	"strconv"
)

// parseFraction parses a fraction string (e.g. "0.9") into a float64 in [0,1].
// Returns 0.9 for empty, unparseable, or out-of-range input.
func parseFraction(s string) float64 {
	if s == "" {
		return 0.9
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v < 0 || v > 1 {
		return 0.9
	}
	return v
}

// effectiveMaxVMs returns the maximum number of VMs that fit on a node given
// the node's allocatable resources, the VM's compute requirements, and the
// capacity fraction.
//
//   effectiveMax = min(
//     floor(allocCPUMillicores * fraction / vmCPUMillicores),
//     floor(allocMemBytes      * fraction / vmMemBytes),
//   )
//
// Returns 0 if either dimension cannot fit even one VM.
func effectiveMaxVMs(allocCPUMillis, allocMemBytes int64, vcpu, memMiB int32, fraction float64) int32 {
	vmCPUMillis := int64(vcpu) * 1000
	cpuMax := int64(float64(allocCPUMillis)*fraction) / vmCPUMillis

	vmMemBytes := int64(memMiB) * 1024 * 1024
	memMax := int64(float64(allocMemBytes)*fraction) / vmMemBytes

	result := cpuMax
	if memMax < cpuMax {
		result = memMax
	}
	if result < 0 {
		return 0
	}
	return int32(result) //nolint:gosec
}
```

**Step 4: Run tests to verify they pass**

```bash
go test ./internal/controller/ -run "TestParseFraction|TestEffectiveMaxVMs" -v
```
Expected: PASS (all 8 parseFraction + 6 effectiveMaxVMs cases)

**Step 5: Commit**

```bash
git add internal/controller/capacity.go internal/controller/capacity_test.go
git commit -m "feat(controller): effectiveMaxVMs + parseFraction capacity helpers"
```

---

### Task 2: resolveClassSpec helper + unit tests

**Files:**
- Modify: `internal/controller/capacity.go` (add `resolveClassSpec`)
- Modify: `internal/controller/capacity_test.go` (add tests)

**Step 1: Write the failing tests** (add to `capacity_test.go`)

Add this import block and test function. The test file is `package controller` (white-box) and uses `sigs.k8s.io/controller-runtime/pkg/client/fake`:

```go
import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

func TestResolveClassSpec_DirectClassRef(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := impdevv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("scheme: %v", err)
	}

	class := &impdevv1alpha1.ImpVMClass{}
	class.Name = "small"
	class.Spec.VCPU = 2
	class.Spec.MemoryMiB = 512

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(class).Build()

	vm := &impdevv1alpha1.ImpVM{}
	vm.Namespace = "default"
	vm.Name = "test-vm"
	vm.Spec.ClassRef = &impdevv1alpha1.ClusterObjectRef{Name: "small"}

	spec, err := resolveClassSpec(context.Background(), c, vm)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.VCPU != 2 {
		t.Errorf("VCPU = %d, want 2", spec.VCPU)
	}
	if spec.MemoryMiB != 512 {
		t.Errorf("MemoryMiB = %d, want 512", spec.MemoryMiB)
	}
}

func TestResolveClassSpec_ViaTemplateRef(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := impdevv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("scheme: %v", err)
	}

	class := &impdevv1alpha1.ImpVMClass{}
	class.Name = "large"
	class.Spec.VCPU = 4
	class.Spec.MemoryMiB = 2048

	tmpl := &impdevv1alpha1.ImpVMTemplate{}
	tmpl.Namespace = "default"
	tmpl.Name = "ubuntu-tmpl"
	tmpl.Spec.ClassRef = impdevv1alpha1.ClusterObjectRef{Name: "large"}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(class, tmpl).Build()

	vm := &impdevv1alpha1.ImpVM{}
	vm.Namespace = "default"
	vm.Name = "tmpl-vm"
	vm.Spec.TemplateRef = &impdevv1alpha1.LocalObjectRef{Name: "ubuntu-tmpl"}

	spec, err := resolveClassSpec(context.Background(), c, vm)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.VCPU != 4 {
		t.Errorf("VCPU = %d, want 4", spec.VCPU)
	}
}

func TestResolveClassSpec_NoRef(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := impdevv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("scheme: %v", err)
	}
	c := fake.NewClientBuilder().WithScheme(scheme).Build()

	vm := &impdevv1alpha1.ImpVM{}
	vm.Namespace = "default"
	vm.Name = "no-ref"

	_, err := resolveClassSpec(context.Background(), c, vm)
	if err == nil {
		t.Fatal("expected error for VM with no classRef or templateRef")
	}
}

func TestResolveClassSpec_ClassNotFound(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := impdevv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("scheme: %v", err)
	}
	c := fake.NewClientBuilder().WithScheme(scheme).Build()

	vm := &impdevv1alpha1.ImpVM{}
	vm.Namespace = "default"
	vm.Name = "missing-class"
	vm.Spec.ClassRef = &impdevv1alpha1.ClusterObjectRef{Name: "nonexistent"}

	_, err := resolveClassSpec(context.Background(), c, vm)
	if err == nil {
		t.Fatal("expected error when class does not exist")
	}
}
```

**Step 2: Run tests to verify they fail**

```bash
go test ./internal/controller/ -run "TestResolveClassSpec" -v
```
Expected: FAIL — `undefined: resolveClassSpec`

**Step 3: Add `resolveClassSpec` to `internal/controller/capacity.go`**

Add to the bottom of `capacity.go` (new imports needed: `"context"`, `"fmt"`, `"sigs.k8s.io/controller-runtime/pkg/client"`, and the imp API):

```go
import (
	"context"
	"fmt"
	"strconv"

	"sigs.k8s.io/controller-runtime/pkg/client"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

// resolveClassSpec returns the ImpVMClassSpec for vm by following either
// vm.Spec.ClassRef (direct) or vm.Spec.TemplateRef → template.Spec.ClassRef.
// Returns an error if neither ref is set or if the referenced objects are missing.
func resolveClassSpec(ctx context.Context, c client.Client, vm *impdevv1alpha1.ImpVM) (*impdevv1alpha1.ImpVMClassSpec, error) {
	if vm.Spec.ClassRef != nil {
		var class impdevv1alpha1.ImpVMClass
		if err := c.Get(ctx, client.ObjectKey{Name: vm.Spec.ClassRef.Name}, &class); err != nil {
			return nil, fmt.Errorf("get class %q: %w", vm.Spec.ClassRef.Name, err)
		}
		return &class.Spec, nil
	}
	if vm.Spec.TemplateRef != nil {
		var tmpl impdevv1alpha1.ImpVMTemplate
		if err := c.Get(ctx, client.ObjectKey{Namespace: vm.Namespace, Name: vm.Spec.TemplateRef.Name}, &tmpl); err != nil {
			return nil, fmt.Errorf("get template %q: %w", vm.Spec.TemplateRef.Name, err)
		}
		var class impdevv1alpha1.ImpVMClass
		if err := c.Get(ctx, client.ObjectKey{Name: tmpl.Spec.ClassRef.Name}, &class); err != nil {
			return nil, fmt.Errorf("get class %q (via template %q): %w", tmpl.Spec.ClassRef.Name, tmpl.Name, err)
		}
		return &class.Spec, nil
	}
	return nil, fmt.Errorf("vm %s/%s has neither classRef nor templateRef", vm.Namespace, vm.Name)
}
```

**Step 4: Run tests to verify they pass**

```bash
go test ./internal/controller/ -run "TestParseFraction|TestEffectiveMaxVMs|TestResolveClassSpec" -v
```
Expected: PASS (all tests)

**Step 5: Commit**

```bash
git add internal/controller/capacity.go internal/controller/capacity_test.go
git commit -m "feat(controller): resolveClassSpec — follows ClassRef or TemplateRef chain"
```

---

### Task 3: Wire capacity into scheduler + integration tests

**Files:**
- Modify: `internal/controller/impvm_scheduler.go`
- Create: `internal/controller/impvm_capacity_test.go`

**Step 1: Write the failing integration tests** (`internal/controller/impvm_capacity_test.go`)

These use Ginkgo+Gomega+envtest (same as other controller tests in this package).

```go
package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

// makeNode creates a node with imp/enabled=true and the given allocatable resources,
// setting status via a patch. Defers deletion.
func makeNode(ctx context.Context, name string, cpu, memory string) *corev1.Node {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: map[string]string{labelImpEnabled: "true"},
		},
	}
	Expect(k8sClient.Create(ctx, node)).To(Succeed())
	DeferCleanup(func() { k8sClient.Delete(ctx, node) }) //nolint:errcheck

	// Set allocatable resources on node status.
	patch := client.MergeFrom(node.DeepCopy())
	node.Status.Allocatable = corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse(cpu),
		corev1.ResourceMemory: resource.MustParse(memory),
	}
	Expect(k8sClient.Status().Patch(ctx, node, patch)).To(Succeed())
	return node
}

// makeClass creates an ImpVMClass with the given vcpu/mem. Defers deletion.
func makeClass(ctx context.Context, name string, vcpu int32, memMiB int32) *impdevv1alpha1.ImpVMClass {
	class := &impdevv1alpha1.ImpVMClass{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: impdevv1alpha1.ImpVMClassSpec{
			VCPU:      vcpu,
			MemoryMiB: memMiB,
			DiskGiB:   10,
		},
	}
	Expect(k8sClient.Create(ctx, class)).To(Succeed())
	DeferCleanup(func() { k8sClient.Delete(ctx, class) }) //nolint:errcheck
	return class
}

var _ = Describe("ImpVM Capacity Scheduler", func() {
	ctx := context.Background()

	reconcileVM := func(name string) error {
		r := &ImpVMReconciler{
			Client:   k8sClient,
			Scheme:   k8sClient.Scheme(),
			Recorder: record.NewFakeRecorder(32),
		}
		// First reconcile: adds finalizer.
		_, err := r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: name, Namespace: "default"},
		})
		if err != nil {
			return err
		}
		// Second reconcile: schedules.
		_, err = r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: name, Namespace: "default"},
		})
		return err
	}

	It("schedules VM when node has sufficient allocatable resources", func() {
		// Node: 4 CPUs, 8GiB memory; Class: 1 vcpu, 512MiB; fraction 0.9
		// effectiveMax = min(floor(4000*0.9/1000), floor(8GiB*0.9/512MiB)) = min(3, 14) = 3
		makeNode(ctx, "cap-node-ok", "4", "8Gi")
		makeClass(ctx, "cap-small", 1, 512)

		vm := &impdevv1alpha1.ImpVM{
			ObjectMeta: metav1.ObjectMeta{Name: "cap-vm-ok", Namespace: "default"},
			Spec: impdevv1alpha1.ImpVMSpec{
				ClassRef: &impdevv1alpha1.ClusterObjectRef{Name: "cap-small"},
			},
		}
		Expect(k8sClient.Create(ctx, vm)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, vm) }) //nolint:errcheck

		Expect(reconcileVM("cap-vm-ok")).To(Succeed())

		updated := &impdevv1alpha1.ImpVM{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "cap-vm-ok", Namespace: "default"}, updated)).To(Succeed())
		Expect(updated.Spec.NodeName).To(Equal("cap-node-ok"))
		Expect(updated.Status.Phase).To(Equal(impdevv1alpha1.VMPhaseScheduled))
	})

	It("refuses to schedule when VM class exceeds node allocatable CPU", func() {
		// Node: 1 CPU, 64GiB; Class: 4 vcpu → 0 fit; should be Unschedulable
		makeNode(ctx, "cap-node-small-cpu", "1", "64Gi")
		makeClass(ctx, "cap-big-cpu", 4, 256)

		vm := &impdevv1alpha1.ImpVM{
			ObjectMeta: metav1.ObjectMeta{Name: "cap-vm-no-cpu", Namespace: "default"},
			Spec: impdevv1alpha1.ImpVMSpec{
				ClassRef: &impdevv1alpha1.ClusterObjectRef{Name: "cap-big-cpu"},
			},
		}
		Expect(k8sClient.Create(ctx, vm)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, vm) }) //nolint:errcheck

		Expect(reconcileVM("cap-vm-no-cpu")).To(Succeed())

		updated := &impdevv1alpha1.ImpVM{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "cap-vm-no-cpu", Namespace: "default"}, updated)).To(Succeed())
		Expect(updated.Spec.NodeName).To(BeEmpty())
		Expect(updated.Status.Phase).To(Equal(impdevv1alpha1.VMPhasePending))
	})

	It("refuses to schedule when VM class exceeds node allocatable memory", func() {
		// Node: 16 CPUs, 256MiB; Class: 1 vcpu, 512MiB → 0 fit by memory
		makeNode(ctx, "cap-node-small-mem", "16", "256Mi")
		makeClass(ctx, "cap-big-mem", 1, 512)

		vm := &impdevv1alpha1.ImpVM{
			ObjectMeta: metav1.ObjectMeta{Name: "cap-vm-no-mem", Namespace: "default"},
			Spec: impdevv1alpha1.ImpVMSpec{
				ClassRef: &impdevv1alpha1.ClusterObjectRef{Name: "cap-big-mem"},
			},
		}
		Expect(k8sClient.Create(ctx, vm)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, vm) }) //nolint:errcheck

		Expect(reconcileVM("cap-vm-no-mem")).To(Succeed())

		updated := &impdevv1alpha1.ImpVM{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "cap-vm-no-mem", Namespace: "default"}, updated)).To(Succeed())
		Expect(updated.Spec.NodeName).To(BeEmpty())
		Expect(updated.Status.Phase).To(Equal(impdevv1alpha1.VMPhasePending))
	})

	It("respects per-node capacityFraction from ClusterImpNodeProfile", func() {
		// Node: 4 CPUs, 8GiB; fraction 0.1 → floor(4000*0.1/1000)=0 → unschedulable
		makeNode(ctx, "cap-node-low-frac", "4", "8Gi")
		makeClass(ctx, "cap-tiny", 1, 256)

		profile := &impdevv1alpha1.ClusterImpNodeProfile{
			ObjectMeta: metav1.ObjectMeta{Name: "cap-node-low-frac"},
			Spec:       impdevv1alpha1.ClusterImpNodeProfileSpec{CapacityFraction: "0.1"},
		}
		Expect(k8sClient.Create(ctx, profile)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, profile) }) //nolint:errcheck

		vm := &impdevv1alpha1.ImpVM{
			ObjectMeta: metav1.ObjectMeta{Name: "cap-vm-low-frac", Namespace: "default"},
			Spec: impdevv1alpha1.ImpVMSpec{
				ClassRef: &impdevv1alpha1.ClusterObjectRef{Name: "cap-tiny"},
			},
		}
		Expect(k8sClient.Create(ctx, vm)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, vm) }) //nolint:errcheck

		Expect(reconcileVM("cap-vm-low-frac")).To(Succeed())

		updated := &impdevv1alpha1.ImpVM{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "cap-vm-low-frac", Namespace: "default"}, updated)).To(Succeed())
		Expect(updated.Spec.NodeName).To(BeEmpty())
		Expect(updated.Status.Phase).To(Equal(impdevv1alpha1.VMPhasePending))
	})

	It("falls back to 0.9 when no ClusterImpConfig exists", func() {
		// No ClusterImpConfig created; node 4 CPUs / 8GiB; class 1vcpu/512MiB
		// effectiveMax with 0.9 = min(3, 14) = 3 → should schedule
		makeNode(ctx, "cap-node-no-cfg", "4", "8Gi")
		makeClass(ctx, "cap-def-frac", 1, 512)

		vm := &impdevv1alpha1.ImpVM{
			ObjectMeta: metav1.ObjectMeta{Name: "cap-vm-no-cfg", Namespace: "default"},
			Spec: impdevv1alpha1.ImpVMSpec{
				ClassRef: &impdevv1alpha1.ClusterObjectRef{Name: "cap-def-frac"},
			},
		}
		Expect(k8sClient.Create(ctx, vm)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, vm) }) //nolint:errcheck

		Expect(reconcileVM("cap-vm-no-cfg")).To(Succeed())

		updated := &impdevv1alpha1.ImpVM{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "cap-vm-no-cfg", Namespace: "default"}, updated)).To(Succeed())
		Expect(updated.Spec.NodeName).To(Equal("cap-node-no-cfg"))
	})
})
```

Note: `makeNode` uses `client` package — add `"sigs.k8s.io/controller-runtime/pkg/client"` to imports.

**Step 2: Run tests to verify they fail**

```bash
go test ./internal/controller/... -v -run "ImpVM Capacity Scheduler" 2>&1 | tail -20
```
Expected: tests compile but fail — nodes schedule when they shouldn't, or compile error on `makeNode`.

**Step 3: Modify `internal/controller/impvm_scheduler.go`**

Replace the entire `schedule` method body with the capacity-aware version. Here is the complete new file content for `impvm_scheduler.go`:

```go
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
```

**Step 4: Run full test suite**

```bash
go test ./internal/controller/... -v 2>&1 | tail -30
```
Expected: all existing tests still pass + the 5 new capacity tests pass

**Step 5: Commit**

```bash
git add internal/controller/impvm_scheduler.go internal/controller/impvm_capacity_test.go
git commit -m "feat(controller): capacity-aware scheduling using node allocatable CPU/memory"
```

---

## Test Command Reference

```bash
# Unit tests only (fast, no envtest):
go test ./internal/controller/ -run "TestParseFraction|TestEffectiveMaxVMs|TestResolveClassSpec" -v

# Full controller suite (includes envtest):
go test ./internal/controller/...

# All tests:
go test ./...
```

## Notes

- **No allocatable resources on node**: `Allocatable.Cpu().MilliValue()` returns 0 if the field is absent. `effectiveMaxVMs` with 0 CPU → 0 max → the node is excluded. This is safe — nodes without declared allocatable resources should not receive VMs until a kubelet populates the field.
- **VM with no ClassRef/TemplateRef**: if `resolveClassSpec` returns an error, the capacity check is skipped entirely (best-effort). The existing `MaxImpVMs` hard cap still applies. The webhook prevents this state from reaching the scheduler normally.
- **Fraction string format**: `ClusterImpConfig.spec.capacity.defaultFraction` is a string (e.g. `"0.9"`) due to kubebuilder JSON schema validation requiring a regex pattern — hence `parseFraction` converts it.
