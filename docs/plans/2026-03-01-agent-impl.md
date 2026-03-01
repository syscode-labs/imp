# Agent Implementation Plan — Phase 1: Architecture + StubDriver

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement the node agent Phase 1 — VMDriver interface, StubDriver, ImpVMReconciler state machine, envtest suite, main.go wiring. No FirecrackerDriver, no rootfs, no networking.

**Architecture:** Clean `VMDriver` interface lets Phase 2 swap in real Firecracker with zero test changes. Agent uses controller-runtime reconciler pattern (same framework as the operator). See `docs/plans/2026-03-01-agent-design.md` for full design.

**Tech Stack:** Go 1.25, controller-runtime v0.23, Ginkgo v2 + Gomega, envtest.

---

## Task 1: VMDriver interface and VMState type

**Files:**
- Create: `internal/agent/driver.go`

**Step 1: Baseline compile check**

Run: `go build ./internal/agent/`
Expected: exits 0 (only `agent.go` stub exists).

**Step 2: Create `internal/agent/driver.go`**

```go
package agent

import (
	"context"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

// VMState is the runtime state of a VM as reported by a VMDriver.
type VMState struct {
	// Running is true if the VM process is alive.
	Running bool
	// IP is the IP address assigned to the VM. Empty until the VM is running.
	IP string
	// PID is the process ID of the VM runtime on this node.
	PID int64
}

// VMDriver abstracts the VM runtime backend.
// Implementations: StubDriver (testing/CI), FirecrackerDriver (production, Phase 2).
type VMDriver interface {
	// Start boots the VM and returns its runtime PID.
	Start(ctx context.Context, vm *impdevv1alpha1.ImpVM) (pid int64, err error)

	// Stop halts the VM. Blocks until stopped or ctx is cancelled.
	Stop(ctx context.Context, vm *impdevv1alpha1.ImpVM) error

	// Inspect returns the current runtime state of the VM.
	// Called every reconcile to detect unexpected exits.
	Inspect(ctx context.Context, vm *impdevv1alpha1.ImpVM) (VMState, error)
}
```

**Step 3: Compile check**

Run: `go build ./internal/agent/`
Expected: exits 0.

**Step 4: Commit**

```bash
git add internal/agent/driver.go
git commit -m "feat(agent): VMDriver interface and VMState type"
```

---

## Task 2: StubDriver

**Files:**
- Create: `internal/agent/stub_driver.go`
- Create: `internal/agent/stub_driver_test.go`

**Step 1: Write the failing test first**

Create `internal/agent/stub_driver_test.go`:

```go
package agent_test

import (
	"context"
	"fmt"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
	"github.com/syscode-labs/imp/internal/agent"
)

func TestStubDriver_StartInspectStop(t *testing.T) {
	ctx := context.Background()
	d := agent.NewStubDriver()

	vm := &impdevv1alpha1.ImpVM{
		ObjectMeta: metav1.ObjectMeta{Name: "test-vm", Namespace: "default"},
	}

	// Before Start: Inspect returns not-running.
	state, err := d.Inspect(ctx, vm)
	if err != nil {
		t.Fatalf("Inspect before Start: %v", err)
	}
	if state.Running {
		t.Fatal("expected not running before Start")
	}

	// Start allocates a PID and IP.
	pid, err := d.Start(ctx, vm)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if pid <= 0 {
		t.Fatalf("expected positive PID, got %d", pid)
	}

	// Inspect after Start: running=true, IP set, PID matches.
	state, err = d.Inspect(ctx, vm)
	if err != nil {
		t.Fatalf("Inspect after Start: %v", err)
	}
	if !state.Running {
		t.Fatal("expected running after Start")
	}
	if state.IP == "" {
		t.Fatal("expected non-empty IP after Start")
	}
	if state.PID != pid {
		t.Fatalf("expected PID %d, got %d", pid, state.PID)
	}

	// Stop clears state.
	if err := d.Stop(ctx, vm); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// Inspect after Stop: not running.
	state, err = d.Inspect(ctx, vm)
	if err != nil {
		t.Fatalf("Inspect after Stop: %v", err)
	}
	if state.Running {
		t.Fatal("expected not running after Stop")
	}
}

func TestStubDriver_ConcurrentSafe(t *testing.T) {
	ctx := context.Background()
	d := agent.NewStubDriver()

	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func(i int) {
			vm := &impdevv1alpha1.ImpVM{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("vm-%d", i),
					Namespace: "default",
				},
			}
			_, _ = d.Start(ctx, vm)
			_, _ = d.Inspect(ctx, vm)
			_ = d.Stop(ctx, vm)
			done <- struct{}{}
		}(i)
	}
	for i := 0; i < 10; i++ {
		<-done
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run TestStubDriver -v 2>&1 | tail -5`
Expected: compile error — `agent.NewStubDriver` undefined.

**Step 3: Create `internal/agent/stub_driver.go`**

```go
package agent

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

// pidCounter generates monotonically increasing fake PIDs.
var pidCounter int64

type stubVM struct {
	pid int64
	ip  string
}

// StubDriver is a VMDriver for testing and CI.
// It simulates VM start/stop/inspect without any real processes.
// Safe for concurrent use.
type StubDriver struct {
	mu     sync.Mutex
	states map[string]*stubVM // keyed by "namespace/name"
}

// NewStubDriver creates a new StubDriver with empty state.
func NewStubDriver() *StubDriver {
	return &StubDriver{states: make(map[string]*stubVM)}
}

func vmKey(vm *impdevv1alpha1.ImpVM) string {
	return fmt.Sprintf("%s/%s", vm.Namespace, vm.Name)
}

// Start allocates a fake PID and IP and marks the VM as running immediately.
func (d *StubDriver) Start(_ context.Context, vm *impdevv1alpha1.ImpVM) (int64, error) {
	pid := atomic.AddInt64(&pidCounter, 1) + 10000
	ip := fmt.Sprintf("192.168.100.%d", pid%254+1)
	d.mu.Lock()
	defer d.mu.Unlock()
	d.states[vmKey(vm)] = &stubVM{pid: pid, ip: ip}
	return pid, nil
}

// Stop removes the VM's entry.
func (d *StubDriver) Stop(_ context.Context, vm *impdevv1alpha1.ImpVM) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.states, vmKey(vm))
	return nil
}

// Inspect returns the current state. Returns running=false if not started or already stopped.
func (d *StubDriver) Inspect(_ context.Context, vm *impdevv1alpha1.ImpVM) (VMState, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	s, ok := d.states[vmKey(vm)]
	if !ok {
		return VMState{Running: false}, nil
	}
	return VMState{Running: true, IP: s.ip, PID: s.pid}, nil
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/agent/ -run TestStubDriver -v -race -count=1`
Expected: PASS (both TestStubDriver_StartInspectStop and TestStubDriver_ConcurrentSafe).

**Step 5: Compile whole repo**

Run: `go build ./...`
Expected: exits 0.

**Step 6: Commit**

```bash
git add internal/agent/stub_driver.go internal/agent/stub_driver_test.go
git commit -m "feat(agent): StubDriver — thread-safe fake VMDriver for testing"
```

---

## Task 3: Envtest suite

**Files:**
- Create: `internal/agent/suite_test.go`

**Step 1: Create `internal/agent/suite_test.go`**

This mirrors `internal/controller/suite_test.go` exactly — same envtest pattern, same CRD path.

```go
package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

var (
	ctx       context.Context
	cancel    context.CancelFunc
	testEnv   *envtest.Environment
	cfg       *rest.Config
	k8sClient client.Client
)

func TestAgent(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Agent Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))
	ctx, cancel = context.WithCancel(context.TODO())

	var err error
	err = impdevv1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}
	if dir := getFirstFoundEnvTestBinaryDir(); dir != "" {
		testEnv.BinaryAssetsDirectory = dir
	}

	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	cancel()
	Eventually(func() error {
		return testEnv.Stop()
	}, time.Minute, time.Second).Should(Succeed())
})

func getFirstFoundEnvTestBinaryDir() string {
	basePath := filepath.Join("..", "..", "bin", "k8s")
	entries, err := os.ReadDir(basePath)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() {
			return filepath.Join(basePath, entry.Name())
		}
	}
	return ""
}
```

**Step 2: Verify suite compiles and runs (0 specs — expected)**

Run: `go test ./internal/agent/ -v -count=1 2>&1 | tail -5`
Expected:
```
Ran 0 of 0 Specs in 0.000 seconds
SUCCESS!
PASS
```

**Step 3: Commit**

```bash
git add internal/agent/suite_test.go
git commit -m "feat(agent): envtest suite — mirrors controller suite pattern"
```

---

## Task 4: Reconciler tests (failing)

**Files:**
- Create: `internal/agent/reconciler_test.go`

Write the 4 test cases before the reconciler exists — this is the TDD step.

**Step 1: Create `internal/agent/reconciler_test.go`**

```go
package agent

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

const testNode = "test-node"

func newReconciler(driver VMDriver) *ImpVMReconciler {
	return &ImpVMReconciler{
		Client:   k8sClient,
		NodeName: testNode,
		Driver:   driver,
	}
}

var _ = Describe("ImpVM Agent: Scheduled → Running", func() {
	ctx := context.Background()

	It("sets status.phase=Running, status.ip, status.runtimePID after Scheduled", func() {
		vm := &impdevv1alpha1.ImpVM{
			ObjectMeta: metav1.ObjectMeta{
				Name: "tc1-scheduled", Namespace: "default",
				Finalizers: []string{"imp/finalizer"},
			},
			Spec: impdevv1alpha1.ImpVMSpec{NodeName: testNode},
		}
		Expect(k8sClient.Create(ctx, vm)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, vm) }) //nolint:errcheck

		base := vm.DeepCopy()
		vm.Status.Phase = impdevv1alpha1.VMPhaseScheduled
		Expect(k8sClient.Status().Patch(ctx, vm, client.MergeFrom(base))).To(Succeed())

		_, err := newReconciler(NewStubDriver()).Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "tc1-scheduled", Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())

		updated := &impdevv1alpha1.ImpVM{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "tc1-scheduled", Namespace: "default"}, updated)).To(Succeed())
		Expect(updated.Status.Phase).To(Equal(impdevv1alpha1.VMPhaseRunning))
		Expect(updated.Status.IP).NotTo(BeEmpty())
		Expect(updated.Status.RuntimePID).To(BeNumerically(">", 0))
	})
})

var _ = Describe("ImpVM Agent: ephemeral exit → Succeeded", func() {
	ctx := context.Background()

	It("sets phase=Succeeded and clears spec.nodeName when ephemeral VM process exits", func() {
		driver := NewStubDriver()

		vm := &impdevv1alpha1.ImpVM{
			ObjectMeta: metav1.ObjectMeta{
				Name: "tc2-ephemeral", Namespace: "default",
				Finalizers: []string{"imp/finalizer"},
			},
			Spec: impdevv1alpha1.ImpVMSpec{
				NodeName:  testNode,
				Lifecycle: impdevv1alpha1.VMLifecycleEphemeral,
			},
		}
		Expect(k8sClient.Create(ctx, vm)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, vm) }) //nolint:errcheck

		// Prime driver + status as if VM was already started.
		pid, err := driver.Start(ctx, vm)
		Expect(err).NotTo(HaveOccurred())
		base := vm.DeepCopy()
		vm.Status.Phase = impdevv1alpha1.VMPhaseRunning
		vm.Status.RuntimePID = pid
		vm.Status.IP = "192.168.100.1"
		Expect(k8sClient.Status().Patch(ctx, vm, client.MergeFrom(base))).To(Succeed())

		// Simulate process exit.
		Expect(driver.Stop(ctx, vm)).To(Succeed())

		_, err = newReconciler(driver).Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "tc2-ephemeral", Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())

		updated := &impdevv1alpha1.ImpVM{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "tc2-ephemeral", Namespace: "default"}, updated)).To(Succeed())
		Expect(updated.Status.Phase).To(Equal(impdevv1alpha1.VMPhaseSucceeded))
		Expect(updated.Spec.NodeName).To(BeEmpty())
	})
})

var _ = Describe("ImpVM Agent: persistent exit → Failed", func() {
	ctx := context.Background()

	It("sets phase=Failed and keeps spec.nodeName when persistent VM process exits", func() {
		driver := NewStubDriver()

		vm := &impdevv1alpha1.ImpVM{
			ObjectMeta: metav1.ObjectMeta{
				Name: "tc3-persistent", Namespace: "default",
				Finalizers: []string{"imp/finalizer"},
			},
			Spec: impdevv1alpha1.ImpVMSpec{
				NodeName:  testNode,
				Lifecycle: impdevv1alpha1.VMLifecyclePersistent,
			},
		}
		Expect(k8sClient.Create(ctx, vm)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, vm) }) //nolint:errcheck

		pid, err := driver.Start(ctx, vm)
		Expect(err).NotTo(HaveOccurred())
		base := vm.DeepCopy()
		vm.Status.Phase = impdevv1alpha1.VMPhaseRunning
		vm.Status.RuntimePID = pid
		vm.Status.IP = "192.168.100.2"
		Expect(k8sClient.Status().Patch(ctx, vm, client.MergeFrom(base))).To(Succeed())

		Expect(driver.Stop(ctx, vm)).To(Succeed())

		_, err = newReconciler(driver).Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "tc3-persistent", Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())

		updated := &impdevv1alpha1.ImpVM{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "tc3-persistent", Namespace: "default"}, updated)).To(Succeed())
		Expect(updated.Status.Phase).To(Equal(impdevv1alpha1.VMPhaseFailed))
		Expect(updated.Spec.NodeName).To(Equal(testNode)) // persistent: keep nodeName
	})
})

var _ = Describe("ImpVM Agent: Terminating → clears nodeName", func() {
	ctx := context.Background()

	It("calls Stop, clears spec.nodeName, and clears status.ip + status.runtimePID", func() {
		driver := NewStubDriver()

		vm := &impdevv1alpha1.ImpVM{
			ObjectMeta: metav1.ObjectMeta{
				Name: "tc4-terminating", Namespace: "default",
				Finalizers: []string{"imp/finalizer"},
			},
			Spec: impdevv1alpha1.ImpVMSpec{NodeName: testNode},
		}
		Expect(k8sClient.Create(ctx, vm)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, vm) }) //nolint:errcheck

		pid, err := driver.Start(ctx, vm)
		Expect(err).NotTo(HaveOccurred())
		base := vm.DeepCopy()
		vm.Status.Phase = impdevv1alpha1.VMPhaseTerminating
		vm.Status.RuntimePID = pid
		vm.Status.IP = "192.168.100.3"
		Expect(k8sClient.Status().Patch(ctx, vm, client.MergeFrom(base))).To(Succeed())

		_, err = newReconciler(driver).Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "tc4-terminating", Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())

		updated := &impdevv1alpha1.ImpVM{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "tc4-terminating", Namespace: "default"}, updated)).To(Succeed())
		Expect(updated.Spec.NodeName).To(BeEmpty())
		Expect(updated.Status.IP).To(BeEmpty())
		Expect(updated.Status.RuntimePID).To(BeZero())
	})
})
```

**Step 2: Confirm it fails to compile**

Run: `go build ./internal/agent/`
Expected: `undefined: ImpVMReconciler`

**Step 3: Commit the failing tests**

```bash
git add internal/agent/reconciler_test.go
git commit -m "test(agent): 4 reconciler test cases — failing, ImpVMReconciler not yet defined"
```

---

## Task 5: ImpVMReconciler — full state machine

**Files:**
- Create: `internal/agent/reconciler.go`

**Step 1: Create `internal/agent/reconciler.go`**

```go
package agent

import (
	"context"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

// ImpVMReconciler watches ImpVM objects and drives VM lifecycle on this node.
// It filters to objects where spec.nodeName == NodeName — all others are ignored.
type ImpVMReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	NodeName string
	Driver   VMDriver
}

// +kubebuilder:rbac:groups=imp.dev,resources=impvms,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=imp.dev,resources=impvms/status,verbs=get;update;patch

func (r *ImpVMReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx).WithValues("node", r.NodeName)

	vm := &impdevv1alpha1.ImpVM{}
	if err := r.Get(ctx, req.NamespacedName, vm); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Filter: only reconcile VMs assigned to this node.
	if vm.Spec.NodeName != r.NodeName {
		return ctrl.Result{}, nil
	}

	log = log.WithValues("vm", req.NamespacedName, "phase", vm.Status.Phase)

	switch vm.Status.Phase {
	case impdevv1alpha1.VMPhaseTerminating:
		return r.handleTerminating(ctx, vm)
	case impdevv1alpha1.VMPhaseScheduled:
		return r.handleScheduled(ctx, vm)
	case impdevv1alpha1.VMPhaseRunning:
		return r.handleRunning(ctx, vm)
	case impdevv1alpha1.VMPhaseStarting:
		log.Info("VM is Starting — requeuing")
		return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
	default:
		// Pending, Succeeded, Failed — not our concern.
		return ctrl.Result{}, nil
	}
}

func (r *ImpVMReconciler) handleScheduled(ctx context.Context, vm *impdevv1alpha1.ImpVM) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Set phase=Starting before calling driver to make concurrent reconciles idempotent.
	base := vm.DeepCopy()
	vm.Status.Phase = impdevv1alpha1.VMPhaseStarting
	if err := r.Status().Patch(ctx, vm, client.MergeFrom(base)); err != nil {
		if apierrors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, err
	}

	pid, err := r.Driver.Start(ctx, vm)
	if err != nil {
		log.Error(err, "Driver Start failed")
		return ctrl.Result{}, err
	}

	state, err := r.Driver.Inspect(ctx, vm)
	if err != nil {
		log.Error(err, "Driver Inspect after Start failed")
		return ctrl.Result{}, err
	}

	base = vm.DeepCopy()
	vm.Status.Phase = impdevv1alpha1.VMPhaseRunning
	vm.Status.IP = state.IP
	vm.Status.RuntimePID = pid
	if err := r.Status().Patch(ctx, vm, client.MergeFrom(base)); err != nil {
		if apierrors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, err
	}

	log.Info("VM started", "pid", pid, "ip", state.IP)
	return ctrl.Result{}, nil
}

func (r *ImpVMReconciler) handleRunning(ctx context.Context, vm *impdevv1alpha1.ImpVM) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	state, err := r.Driver.Inspect(ctx, vm)
	if err != nil {
		log.Error(err, "Driver Inspect failed")
		return ctrl.Result{}, err
	}

	if state.Running {
		return ctrl.Result{}, nil // watch-driven steady state
	}

	log.Info("VM process exited", "lifecycle", vm.Spec.Lifecycle)
	if vm.Spec.Lifecycle == impdevv1alpha1.VMLifecycleEphemeral {
		return r.finishSucceeded(ctx, vm)
	}
	return r.finishFailed(ctx, vm)
}

func (r *ImpVMReconciler) handleTerminating(ctx context.Context, vm *impdevv1alpha1.ImpVM) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	if err := r.Driver.Stop(ctx, vm); err != nil {
		log.Error(err, "Driver Stop failed — will retry")
		return ctrl.Result{RequeueAfter: 2 * time.Second}, err
	}
	return r.clearOwnership(ctx, vm)
}

// finishSucceeded clears spec.nodeName (triggers operator finalizer) + sets phase=Succeeded.
func (r *ImpVMReconciler) finishSucceeded(ctx context.Context, vm *impdevv1alpha1.ImpVM) (ctrl.Result, error) {
	// Spec patch first — spec.nodeName is a spec field, not a status field.
	specBase := vm.DeepCopy()
	vm.Spec.NodeName = ""
	if err := r.Patch(ctx, vm, client.MergeFrom(specBase)); err != nil {
		return ctrl.Result{}, err
	}
	// Status patch — take base AFTER spec patch so resourceVersion is current.
	base := vm.DeepCopy()
	vm.Status.Phase = impdevv1alpha1.VMPhaseSucceeded
	vm.Status.IP = ""
	vm.Status.RuntimePID = 0
	if err := r.Status().Patch(ctx, vm, client.MergeFrom(base)); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// finishFailed sets phase=Failed; keeps spec.nodeName (operator handles cleanup for persistent VMs).
func (r *ImpVMReconciler) finishFailed(ctx context.Context, vm *impdevv1alpha1.ImpVM) (ctrl.Result, error) {
	base := vm.DeepCopy()
	vm.Status.Phase = impdevv1alpha1.VMPhaseFailed
	if err := r.Status().Patch(ctx, vm, client.MergeFrom(base)); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// clearOwnership clears spec.nodeName + status ip/pid after Terminating stop.
func (r *ImpVMReconciler) clearOwnership(ctx context.Context, vm *impdevv1alpha1.ImpVM) (ctrl.Result, error) {
	specBase := vm.DeepCopy()
	vm.Spec.NodeName = ""
	if err := r.Patch(ctx, vm, client.MergeFrom(specBase)); err != nil {
		return ctrl.Result{}, err
	}
	base := vm.DeepCopy()
	vm.Status.IP = ""
	vm.Status.RuntimePID = 0
	if err := r.Status().Patch(ctx, vm, client.MergeFrom(base)); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// SetupWithManager registers the reconciler with the controller-runtime manager.
func (r *ImpVMReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&impdevv1alpha1.ImpVM{}).
		Named("agent-impvm").
		Complete(r)
}
```

**Step 2: Compile check**

Run: `go build ./internal/agent/`
Expected: exits 0. Fix any import errors.

**Step 3: Run the 4 reconciler tests**

Run: `go test ./internal/agent/ -v -count=1 2>&1 | tail -20`
Expected: all 4 Describe blocks PASS.

**Step 4: Run full test suite**

Run: `make test`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/agent/reconciler.go
git commit -m "feat(agent): ImpVMReconciler — full state machine, all 4 tests passing"
```

---

## Task 6: Update `cmd/agent/main.go`

**Files:**
- Modify: `cmd/agent/main.go`

**Step 1: Replace `cmd/agent/main.go`**

```go
package main

import (
	"flag"
	"os"

	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
	"github.com/syscode-labs/imp/internal/agent"
)

func main() {
	opts := zap.Options{Development: true}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	log := ctrl.Log.WithName("agent")

	nodeName := os.Getenv("NODE_NAME")
	if nodeName == "" {
		log.Error(nil, "NODE_NAME env var not set — run as DaemonSet with fieldRef downward API")
		os.Exit(1)
	}

	// IMP_STUB_DRIVER=true: StubDriver (CI, test clusters, no KVM needed).
	// Otherwise: FirecrackerDriver (Phase 2 — falls back to StubDriver until implemented).
	var driver agent.VMDriver
	if os.Getenv("IMP_STUB_DRIVER") == "true" {
		log.Info("Using StubDriver (IMP_STUB_DRIVER=true)")
		driver = agent.NewStubDriver()
	} else {
		// Phase 2 will replace this with FirecrackerDriver.
		log.Info("FirecrackerDriver not yet implemented — using StubDriver (set IMP_STUB_DRIVER=true to suppress this warning)")
		driver = agent.NewStubDriver()
	}

	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		log.Error(err, "Unable to add client-go scheme")
		os.Exit(1)
	}
	if err := impdevv1alpha1.AddToScheme(scheme); err != nil {
		log.Error(err, "Unable to add imp scheme")
		os.Exit(1)
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		HealthProbeBindAddress: ":8081",
		LeaderElection:         false, // DaemonSet: one instance per node, no election needed.
	})
	if err != nil {
		log.Error(err, "Unable to start manager")
		os.Exit(1)
	}

	if err := (&agent.ImpVMReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		NodeName: nodeName,
		Driver:   driver,
	}).SetupWithManager(mgr); err != nil {
		log.Error(err, "Unable to set up ImpVMReconciler")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		log.Error(err, "Unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		log.Error(err, "Unable to set up ready check")
		os.Exit(1)
	}

	log.Info("Agent starting", "node", nodeName)
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		log.Error(err, "Problem running agent manager")
		os.Exit(1)
	}
}
```

**Step 2: Compile check**

Run: `go build ./cmd/agent/`
Expected: exits 0.

**Step 3: Run full test suite**

Run: `make test`
Expected: PASS.

**Step 4: Commit**

```bash
git add cmd/agent/main.go
git commit -m "feat(agent): wire ImpVMReconciler + StubDriver into controller-runtime manager"
```

---

## Task 7: Delete `internal/agent/agent.go`

**Files:**
- Delete: `internal/agent/agent.go`

**Step 1: Confirm no remaining references to the old `Agent` type**

Run: `grep -r "agent\.Agent\b" . --include="*.go"`
Expected: 0 results (main.go was replaced in Task 6).

**Step 2: Delete the file**

Run: `git rm internal/agent/agent.go`

**Step 3: Compile check**

Run: `go build ./...`
Expected: exits 0.

**Step 4: Run full test suite**

Run: `make test`
Expected: PASS.

**Step 5: Commit**

```bash
git commit -m "refactor(agent): delete agent.go placeholder — superseded by ImpVMReconciler"
```

---

## Task 8: Final validation

**Step 1: Full build**

Run: `make build`
Expected: exits 0, produces `bin/operator` and `bin/agent`.

**Step 2: Lint + test**

Run: `make lint test`
Expected: all PASS, 0 lint errors.

**Step 3: Smoke test the binary**

Run (Ctrl-C after seeing the log line):
```bash
NODE_NAME=local-test IMP_STUB_DRIVER=true ./bin/agent 2>&1 | head -3
```
Expected log line: `"Using StubDriver (IMP_STUB_DRIVER=true)"`

**Step 4: Verify git log**

Run: `git log --oneline -8`
Expected: 7 commits from Tasks 1–7 in order.
