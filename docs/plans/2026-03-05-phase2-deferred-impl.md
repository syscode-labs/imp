# Phase 2 Deferred Items Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task.

**Goal:** Implement three deferred Phase 1 items: NAT teardown reference counting, probe condition patching, and resource-aware VM scheduling with autoscaler-friendly signalling.

**Architecture:** Three self-contained changes. NAT teardown adds a VM reference count to `Allocator` and a `RemoveNAT` method to `NetManager`. Probe patching replaces the no-op patcher closure with a direct status patch via `d.Client`. Scheduling extracts a pure `Schedule()` function and integrates it into the existing `schedule()` method when explicit capacity fields are present on `ClusterImpNodeProfile`.

**Tech Stack:** Go 1.25, controller-runtime v0.20, kubebuilder markers, nft/iptables, `k8s.io/apimachinery/pkg/api/meta`

---

## Context

- Worktree: `/Users/giovanni/.config/superpowers/worktrees/imp/phase2-deferred`
- Branch: `feature/phase2-deferred`
- Run all tests with: `KUBEBUILDER_ASSETS=$(go run sigs.k8s.io/controller-runtime/tools/setup-envtest@latest use -p path 2>/dev/null) go test ./...`
- Build check: `GOOS=linux go build ./...`
- Lint: `golangci-lint run ./...`
- All commits: no "Co-Authored-By: Claude" lines; no AI mention in messages

---

## Task 1: Allocator VM reference count

Add a per-network VM count to `Allocator`. `Allocate` increments it; `Release` decrements and returns `wasLast bool`. This enables the caller to know when the last VM on a network has been torn down.

**Files:**
- Modify: `internal/agent/network/ipallocator.go`
- Modify: `internal/agent/network/ipallocator_test.go`

**Step 1: Write the failing tests**

Add to `ipallocator_test.go`:

```go
func TestAllocator_release_wasLast_true(t *testing.T) {
	a := network.NewAllocator()
	ip, _ := a.Allocate("ns/net", "10.0.0.0/24", "")
	wasLast := a.Release("ns/net", ip)
	if !wasLast {
		t.Error("expected wasLast=true when last VM released")
	}
}

func TestAllocator_release_wasLast_false(t *testing.T) {
	a := network.NewAllocator()
	ip1, _ := a.Allocate("ns/net", "10.0.0.0/24", "")
	_, _ = a.Allocate("ns/net", "10.0.0.0/24", "")
	wasLast := a.Release("ns/net", ip1)
	if wasLast {
		t.Error("expected wasLast=false when one VM still allocated")
	}
}

func TestAllocator_release_unknown_key(t *testing.T) {
	a := network.NewAllocator()
	// Releasing an unknown key must not panic and must return true.
	wasLast := a.Release("ns/notexist", "10.0.0.2")
	if !wasLast {
		t.Error("expected wasLast=true for unknown key (no VMs remain)")
	}
}
```

**Step 2: Run tests to verify they fail**

```bash
cd /Users/giovanni/.config/superpowers/worktrees/imp/phase2-deferred
go test ./internal/agent/network/ -run TestAllocator_release -v
```

Expected: compile error — `Release` returns nothing.

**Step 3: Update `Allocator` in `ipallocator.go`**

Add `vmCount map[string]int` field to `Allocator`:

```go
type Allocator struct {
	mu        sync.Mutex
	allocated map[string]map[string]struct{} // netKey → set of allocated IPs
	vmCount   map[string]int                 // netKey → number of active VMs
}
```

Update `NewAllocator`:

```go
func NewAllocator() *Allocator {
	return &Allocator{
		allocated: make(map[string]map[string]struct{}),
		vmCount:   make(map[string]int),
	}
}
```

Update `Allocate` — increment `vmCount` after successfully allocating:

```go
func (a *Allocator) Allocate(netKey, subnet, gateway string) (string, error) {
	// ... existing logic unchanged up to the final `return s, nil` ...
	// Right before returning the allocated IP, increment vmCount:
	a.vmCount[netKey]++
	return s, nil
	// ... rest unchanged ...
}
```

The full updated `Allocate` method (replace entire function body):

```go
func (a *Allocator) Allocate(netKey, subnet, gateway string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	_, cidr, err := net.ParseCIDR(subnet)
	if err != nil {
		return "", fmt.Errorf("parse subnet %q: %w", subnet, err)
	}

	var gwIP net.IP
	if gateway == "" {
		gwIP = nextIP(cidr.IP.To4())
	} else {
		gwIP = net.ParseIP(gateway).To4()
	}

	set := a.allocated[netKey]
	if set == nil {
		set = make(map[string]struct{})
		a.allocated[netKey] = set
	}

	bcast := broadcastIP(cidr)
	ip := nextIP(cidr.IP.To4()) // skip network address
	for cidr.Contains(ip) {
		if ip.Equal(bcast) {
			break
		}
		s := ip.String()
		if !ip.Equal(gwIP) {
			if _, used := set[s]; !used {
				set[s] = struct{}{}
				a.vmCount[netKey]++
				return s, nil
			}
		}
		ip = nextIP(ip)
	}
	return "", fmt.Errorf("no free IPs in subnet %s", subnet)
}
```

Update `Release` to return `wasLast bool`:

```go
// Release frees a previously allocated IP so it can be reused.
// Returns true when this was the last allocated IP for netKey (VM count reached zero).
func (a *Allocator) Release(netKey, ip string) (wasLast bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if set, ok := a.allocated[netKey]; ok {
		delete(set, ip)
	}
	a.vmCount[netKey]--
	if a.vmCount[netKey] <= 0 {
		delete(a.vmCount, netKey)
		return true
	}
	return false
}
```

**Step 4: Run tests to verify they pass**

```bash
go test ./internal/agent/network/ -run TestAllocator -v
```

Expected: all `TestAllocator_*` tests pass.

**Step 5: Fix compile error in `firecracker_driver.go`**

`Stop()` calls `d.Alloc.Release(...)` without capturing the return value. Temporarily capture it as `_` to restore compilation:

In `internal/agent/firecracker_driver.go`, find the line:
```go
d.Alloc.Release(proc.netInfo.NetworkKey, proc.netInfo.IP)
```

Replace with:
```go
_ = d.Alloc.Release(proc.netInfo.NetworkKey, proc.netInfo.IP)
```

(Task 3 will use the return value properly.)

**Step 6: Full test suite passes**

```bash
KUBEBUILDER_ASSETS=$(go run sigs.k8s.io/controller-runtime/tools/setup-envtest@latest use -p path 2>/dev/null) go test ./...
```

Expected: all packages pass.

**Step 7: Commit**

```bash
git add internal/agent/network/ipallocator.go internal/agent/network/ipallocator_test.go internal/agent/firecracker_driver.go
git commit -m "feat(network): Allocator.Release returns wasLast bool"
```

---

## Task 2: RemoveNAT — interface, stub, and Linux implementation

Add `RemoveNAT` to the `NetManager` interface, implement it on `StubNetManager` (no-op + record calls), and implement on `LinuxNetManager` (nftables handle lookup + iptables `-D`).

**Files:**
- Modify: `internal/agent/network/net.go`
- Modify: `internal/agent/network/linux.go`
- Modify: `internal/agent/network/linux_test.go`

**Step 1: Write the failing test**

In `linux_test.go`, the existing `TestLinuxNetManager_implementsInterface` will fail when we add `RemoveNAT` to the interface before implementing it. That test is the failing test. Also add to `net.go`'s StubNetManager tests (in `net.go` itself — it has a compile-time `var _ NetManager = (*StubNetManager)(nil)`).

Run to confirm the current baseline passes:

```bash
go test ./internal/agent/network/ -v
```

Expected: PASS.

**Step 2: Add `RemoveNAT` to `NetManager` interface in `net.go`**

Add after `EnsureNAT`:

```go
// RemoveNAT removes the MASQUERADE rule for subnet.
// Idempotent — no error if the rule does not exist.
// If egressIface is empty, the default-route interface is used.
RemoveNAT(ctx context.Context, subnet, egressIface string) error
```

Add to `StubNetManager` struct:

```go
RemoveNATCalls []string // subnet
RemoveNATErr   error
```

Add `StubNetManager.RemoveNAT` method:

```go
func (s *StubNetManager) RemoveNAT(_ context.Context, subnet, _ string) error {
	s.RemoveNATCalls = append(s.RemoveNATCalls, subnet)
	return s.RemoveNATErr
}
```

**Step 3: Run to verify compile failure on LinuxNetManager**

```bash
go build ./internal/agent/network/
```

Expected: `LinuxNetManager` does not implement `NetManager` (missing `RemoveNAT`).

**Step 4: Implement `RemoveNAT` on `LinuxNetManager` in `linux.go`**

Add after `EnsureNAT`:

```go
// RemoveNAT removes the MASQUERADE rule for subnet on egressIface.
// Idempotent — no-op if the rule does not exist. Uses the same backend
// (nftables or iptables) chosen at construction time.
func (m *LinuxNetManager) RemoveNAT(_ context.Context, subnet, egressIface string) error {
	if egressIface == "" {
		iface, err := defaultRouteIface()
		if err != nil {
			return fmt.Errorf("detect egress interface: %w", err)
		}
		egressIface = iface
	}
	if m.natBackend == "nftables" {
		return removeNATNftables(subnet, egressIface)
	}
	return removeNATIptables(subnet, egressIface)
}

// removeNATNftables removes the per-subnet MASQUERADE rule from the imp_nat chain.
// Uses handle-based deletion to avoid removing unrelated rules.
func removeNATNftables(subnet, _ string) error {
	//nolint:gosec
	out, err := exec.Command("nft", "-a", "list", "chain", "ip", "imp_nat", "postrouting").Output()
	if err != nil {
		return nil // chain doesn't exist — idempotent
	}
	handle := findNftHandle(string(out), subnet)
	if handle == "" {
		return nil // rule not found — idempotent
	}
	//nolint:gosec // G204: handle is a number parsed from nft output
	if out2, err := exec.Command("nft", "delete", "rule", "ip", "imp_nat", "postrouting", "handle", handle).CombinedOutput(); err != nil {
		return fmt.Errorf("nft delete rule: %w: %s", err, out2)
	}
	return nil
}

// findNftHandle returns the handle number for the first rule in nft -a list output
// that contains subnet. Returns "" if not found.
func findNftHandle(output, subnet string) string {
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, subnet) {
			if idx := strings.Index(line, "# handle "); idx >= 0 {
				rest := strings.TrimSpace(line[idx+len("# handle "):])
				if fields := strings.Fields(rest); len(fields) > 0 {
					return fields[0]
				}
			}
		}
	}
	return ""
}

// removeNATIptables deletes the MASQUERADE rule via iptables -D.
// Idempotent: treats "not found" (exit 1) as success.
func removeNATIptables(subnet, egressIface string) error {
	//nolint:gosec // G204: subnet and egressIface are controlled values
	_ = exec.Command("iptables", "-t", "nat", "-D", "POSTROUTING",
		"-s", subnet, "-o", egressIface, "-j", "MASQUERADE").Run()
	return nil
}
```

**Step 5: Add `findNftHandle` unit test to `linux_test.go`**

```go
func TestFindNftHandle(t *testing.T) {
	output := `table ip imp_nat {
	chain postrouting {
		type nat hook postrouting priority srcnat; policy accept;
		ip saddr 10.0.1.0/24 oifname "eth0" masquerade # handle 3
		ip saddr 10.0.2.0/24 oifname "eth0" masquerade # handle 7
	}
}`
	tests := []struct {
		subnet string
		want   string
	}{
		{"10.0.1.0/24", "3"},
		{"10.0.2.0/24", "7"},
		{"10.0.3.0/24", ""},
	}
	for _, tc := range tests {
		got := network.FindNftHandle(output, tc.subnet)
		if got != tc.want {
			t.Errorf("FindNftHandle(_, %q) = %q, want %q", tc.subnet, got, tc.want)
		}
	}
}
```

> **Note:** `findNftHandle` is unexported. For testing, export it as `FindNftHandle` in `linux.go` (add `var FindNftHandle = findNftHandle` at the bottom of `linux.go`, or rename the function).

Simpler: just add `var FindNftHandle = findNftHandle` at the end of `linux.go` inside a `//go:build linux` block — same file, no new file.

```go
// FindNftHandle is exported for testing.
var FindNftHandle = findNftHandle
```

**Step 6: Run tests**

```bash
go test ./internal/agent/network/ -v
```

Expected: all pass including `TestFindNftHandle`.

**Step 7: Commit**

```bash
git add internal/agent/network/net.go internal/agent/network/linux.go internal/agent/network/linux_test.go
git commit -m "feat(network): add RemoveNAT to NetManager interface and LinuxNetManager"
```

---

## Task 3: Wire NAT teardown in `firecracker_driver.go`

Add `EgressInterface` and `NATEnabled` to `NetworkInfo`. Populate them in `setupNetwork()`. In `Stop()`, use the `wasLast` return from `Release` to call `RemoveNAT` when the last VM on a network is torn down.

**Files:**
- Modify: `internal/agent/network/net.go` (NetworkInfo struct)
- Modify: `internal/agent/firecracker_driver.go` (setupNetwork + Stop)
- Modify: `internal/agent/firecracker_driver_test.go` (new test)

**Step 1: Write the failing test**

In `firecracker_driver_test.go`, add a test that verifies `RemoveNAT` is called when the last VM stops:

```go
func TestFirecrackerDriver_Stop_callsRemoveNATOnLastVM(t *testing.T) {
	stub := &network.StubNetManager{}
	alloc := network.NewAllocator()
	d := &FirecrackerDriver{
		Net:   stub,
		Alloc: alloc,
		procs: make(map[string]*fcProc),
	}

	// Pre-populate a proc with NATEnabled=true and one allocated IP.
	_, _ = alloc.Allocate("ns/net", "10.0.0.0/24", "10.0.0.1")
	d.procs["ns/vm"] = &fcProc{
		netInfo: &network.NetworkInfo{
			TAPName:        "imptap-00000001",
			IP:             "10.0.0.2",
			NetworkKey:     "ns/net",
			Subnet:         "10.0.0.0/24",
			NATEnabled:     true,
			EgressInterface: "eth0",
		},
	}

	vm := &impdevv1alpha1.ImpVM{}
	vm.Namespace = "ns"
	vm.Name = "vm"

	ctx := context.Background()
	if err := d.Stop(ctx, vm); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}

	if len(stub.RemoveNATCalls) != 1 || stub.RemoveNATCalls[0] != "10.0.0.0/24" {
		t.Errorf("expected RemoveNAT called with %q, got %v", "10.0.0.0/24", stub.RemoveNATCalls)
	}
}
```

**Step 2: Run to verify it fails**

```bash
cd /Users/giovanni/.config/superpowers/worktrees/imp/phase2-deferred
go test ./internal/agent/ -run TestFirecrackerDriver_Stop_callsRemoveNATOnLastVM -v
```

Expected: compile error — `NATEnabled` and `EgressInterface` fields do not exist.

**Step 3: Add `NATEnabled` and `EgressInterface` to `NetworkInfo` in `net.go`**

```go
type NetworkInfo struct {
	TAPName         string
	BridgeName      string
	MACAddr         string
	IP              string
	PrefixLen       int
	Gateway         string
	DNS             []string
	Subnet          string
	NetworkKey      string
	NATEnabled      bool   // true when NAT was enabled for this network
	EgressInterface string // egress interface used for NAT (may be "" for auto-detect)
}
```

**Step 4: Populate the new fields in `setupNetwork()` in `firecracker_driver.go`**

In the `return &network.NetworkInfo{...}` at the end of `setupNetwork()`, add:

```go
return &network.NetworkInfo{
	TAPName:         tapName,
	BridgeName:      bridgeName,
	MACAddr:         macAddr,
	IP:              ip,
	PrefixLen:       prefixLen,
	Gateway:         gateway,
	DNS:             impNet.Spec.DNS,
	Subnet:          impNet.Spec.Subnet,
	NetworkKey:      netKey,
	NATEnabled:      impNet.Spec.NAT.Enabled,
	EgressInterface: impNet.Spec.NAT.EgressInterface,
}, nil
```

**Step 5: Update `Stop()` to call `RemoveNAT` when `wasLast`**

Find the NAT/Alloc block in `Stop()`:

```go
// Tear down networking if this VM had a network attached.
// NOTE: NAT rules are not torn down in Phase 1 (they are shared across VMs).
if proc.netInfo != nil {
	if d.Net != nil {
		if err := d.Net.TeardownVM(ctx, proc.netInfo.TAPName); err != nil {
			logf.FromContext(ctx).Error(err, "TeardownVM failed", "tap", proc.netInfo.TAPName)
		}
	}
	if d.Alloc != nil {
		_ = d.Alloc.Release(proc.netInfo.NetworkKey, proc.netInfo.IP)
	}
}
```

Replace with:

```go
// Tear down networking if this VM had a network attached.
if proc.netInfo != nil {
	if d.Net != nil {
		if err := d.Net.TeardownVM(ctx, proc.netInfo.TAPName); err != nil {
			logf.FromContext(ctx).Error(err, "TeardownVM failed", "tap", proc.netInfo.TAPName)
		}
	}
	if d.Alloc != nil {
		wasLast := d.Alloc.Release(proc.netInfo.NetworkKey, proc.netInfo.IP)
		if wasLast && proc.netInfo.NATEnabled && d.Net != nil {
			if err := d.Net.RemoveNAT(ctx, proc.netInfo.Subnet, proc.netInfo.EgressInterface); err != nil {
				logf.FromContext(ctx).Error(err, "RemoveNAT failed", "subnet", proc.netInfo.Subnet)
			}
		}
	}
}
```

**Step 6: Run tests**

```bash
KUBEBUILDER_ASSETS=$(go run sigs.k8s.io/controller-runtime/tools/setup-envtest@latest use -p path 2>/dev/null) go test ./...
```

Expected: all pass.

**Step 7: Commit**

```bash
git add internal/agent/network/net.go internal/agent/firecracker_driver.go internal/agent/firecracker_driver_test.go
git commit -m "feat(agent): NAT teardown on last VM stop via Allocator ref-count"
```

---

## Task 4: Probe condition patching

Replace the no-op patcher closure in `runProbes` with one that directly patches `ImpVM.status.conditions` via `d.Client`.

**Files:**
- Modify: `internal/agent/firecracker_driver.go`

**Context:** `ImpVMStatus.Conditions []metav1.Condition` already exists in `api/v1alpha1/impvm_types.go`. The probe runner accepts a `func([]metav1.Condition)` patcher. We just need to replace the no-op with a real implementation.

**Step 1: Understand the existing no-op**

In `runProbes`, the patcher is:
```go
runner := probe.NewRunner(client, probes, func(conds []metav1.Condition) {
	// Best-effort patch of ImpVM status conditions.
	// Use d.Client to patch if available.
	_ = conds // no-op for now; wired in reconciler status path
})
```

We need to capture `vmNamespace` and `vmName` before the goroutine launch, then use them inside.

**Step 2: Write the failing test**

The probe patcher is a closure — we can't easily unit-test it in isolation. Instead we verify the function signature change compiles and that `runProbes` is called with the right arguments. Add to `firecracker_driver_test.go`:

```go
func TestFirecrackerDriver_runProbes_signaturesCompile(t *testing.T) {
	// Compile-time: runProbes must accept (ctx, probes, vsockPath, ns, name).
	d := &FirecrackerDriver{}
	_ = d.runProbes // if signature changed wrongly, this package won't compile
}
```

This is a compile-time check only — the test itself is trivial.

**Step 3: Update `Start()` to capture namespace and name**

Find the probe goroutine setup block in `Start()`:

```go
var probeCtx context.Context
var probes *impdevv1alpha1.ProbeSpec
var vsockPath string
if gaEnabled && vm.Spec.Probes != nil {
	vsockPath = strings.TrimSuffix(sockPath, ".sock") + ".vsock"
	probes = vm.Spec.Probes
	var probeCancel context.CancelFunc
	probeCtx, probeCancel = context.WithCancel(context.Background())
	proc.probeCancel = probeCancel
}
```

Replace with:

```go
var probeCtx context.Context
var probes *impdevv1alpha1.ProbeSpec
var vsockPath, vmNamespace, vmName string
if gaEnabled && vm.Spec.Probes != nil {
	vsockPath = strings.TrimSuffix(sockPath, ".sock") + ".vsock"
	probes = vm.Spec.Probes
	vmNamespace = vm.Namespace
	vmName = vm.Name
	var probeCancel context.CancelFunc
	probeCtx, probeCancel = context.WithCancel(context.Background())
	proc.probeCancel = probeCancel
}
```

Update the goroutine launch:

```go
if probeCtx != nil {
	go d.runProbes(probeCtx, probes, vsockPath, vmNamespace, vmName)
}
```

**Step 4: Update `runProbes` signature and implement the patcher**

Replace the entire `runProbes` method with:

```go
// runProbes dials the guest VSOCK and runs probe polling until ctx is cancelled.
// Called in a goroutine after the VM reaches Running. probes must not be nil.
// vmNamespace and vmName identify the ImpVM to patch conditions onto.
func (d *FirecrackerDriver) runProbes(ctx context.Context, probes *impdevv1alpha1.ProbeSpec, vsockPath, vmNamespace, vmName string) {
	conn, err := agentvsock.Dial(ctx, vsockPath, 10000)
	if err != nil {
		logf.Log.Error(err, "runProbes: VSOCK dial failed", "vsock", vsockPath)
		return
	}
	defer conn.Close() //nolint:errcheck
	client := pb.NewGuestAgentClient(conn)
	runner := probe.NewRunner(client, probes, func(conds []metav1.Condition) {
		nn := types.NamespacedName{Namespace: vmNamespace, Name: vmName}
		vm := &impdevv1alpha1.ImpVM{}
		if err := d.Client.Get(ctx, nn, vm); err != nil {
			logf.Log.Error(err, "probe patcher: Get failed", "vm", nn)
			return
		}
		base := vm.DeepCopy()
		for _, c := range conds {
			apimeta.SetStatusCondition(&vm.Status.Conditions, c)
		}
		if err := d.Client.Status().Patch(ctx, vm, ctrlclient.MergeFrom(base)); err != nil {
			logf.Log.Error(err, "probe patcher: Patch failed", "vm", nn)
		}
	})
	runner.Run(ctx)
}
```

**Step 5: Add missing imports to `firecracker_driver.go`**

Ensure the following imports are present (add what's missing):

```go
import (
	// ... existing imports ...
	"k8s.io/apimachinery/pkg/types"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
)
```

Check existing imports — `types` from `"k8s.io/apimachinery/pkg/types"` may already be present. `apimeta` is new. The `import` block in this file uses `ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"` which is already there.

**Step 6: Build check (linux cross-compile)**

```bash
GOOS=linux go build ./internal/agent/...
```

Expected: no errors.

**Step 7: Commit**

```bash
git add internal/agent/firecracker_driver.go
git commit -m "feat(agent): wire probe condition patcher to ImpVM status patch"
```

---

## Task 5: ClusterImpNodeProfile capacity fields + code generation

Add explicit `VCPUCapacity` and `MemoryMiB` capacity fields to `ClusterImpNodeProfileSpec`, then regenerate CRD YAML and deepcopy.

**Files:**
- Modify: `api/v1alpha1/clusterimpnodeprofile_types.go`
- Modified by generate: `api/v1alpha1/zz_generated.deepcopy.go` (only if new slice/pointer fields — int32/int64 are value types, no deepcopy change needed)
- Modified by manifests: `config/crd/bases/imp.dev_clusterimpnodeprofiles.yaml`

**Step 1: Write the test first**

In `api/v1alpha1/`, there's no dedicated test file for `ClusterImpNodeProfile`. Check if the webhook test covers it. If not, add a simple validation test to confirm the new fields are recognized:

```go
// api/v1alpha1/clusterimpnodeprofile_types_test.go
package v1alpha1_test

import (
	"encoding/json"
	"testing"

	v1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

func TestClusterImpNodeProfile_capacityFieldsRoundTrip(t *testing.T) {
	p := v1alpha1.ClusterImpNodeProfile{}
	p.Spec.VCPUCapacity = 16
	p.Spec.MemoryMiB = 32768

	data, err := json.Marshal(p.Spec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got v1alpha1.ClusterImpNodeProfileSpec
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.VCPUCapacity != 16 {
		t.Errorf("VCPUCapacity: got %d, want 16", got.VCPUCapacity)
	}
	if got.MemoryMiB != 32768 {
		t.Errorf("MemoryMiB: got %d, want 32768", got.MemoryMiB)
	}
}
```

**Step 2: Run to verify test fails**

```bash
go test ./api/v1alpha1/ -run TestClusterImpNodeProfile_capacityFieldsRoundTrip -v
```

Expected: compile error — `VCPUCapacity` and `MemoryMiB` fields don't exist yet.

**Step 3: Add fields to `ClusterImpNodeProfileSpec`**

In `api/v1alpha1/clusterimpnodeprofile_types.go`, add after `MaxImpVMs`:

```go
// VCPUCapacity is the total number of vCPUs available for VMs on this node.
// When non-zero, takes precedence over fraction-based scheduling.
// +optional
// +kubebuilder:validation:Minimum=0
VCPUCapacity int32 `json:"vcpuCapacity,omitempty"`

// MemoryMiB is the total memory in MiB available for VMs on this node.
// When non-zero, takes precedence over fraction-based scheduling.
// +optional
// +kubebuilder:validation:Minimum=0
MemoryMiB int64 `json:"memoryMiB,omitempty"`
```

**Step 4: Regenerate CRD manifests**

```bash
cd /Users/giovanni/.config/superpowers/worktrees/imp/phase2-deferred
make manifests
```

This updates `config/crd/bases/imp.dev_clusterimpnodeprofiles.yaml` to include the new fields.

If `make manifests` fails due to missing `controller-gen`, install it:
```bash
go install sigs.k8s.io/controller-tools/cmd/controller-gen@latest
```

**Step 5: Run tests**

```bash
go test ./api/v1alpha1/ -v
```

Expected: all pass including the new round-trip test.

**Step 6: Commit**

```bash
git add api/v1alpha1/clusterimpnodeprofile_types.go api/v1alpha1/clusterimpnodeprofile_types_test.go config/crd/bases/imp.dev_clusterimpnodeprofiles.yaml
git commit -m "feat(api): add VCPUCapacity and MemoryMiB to ClusterImpNodeProfileSpec"
```

---

## Task 6: Pure `Schedule()` function with unit tests

Create a new `internal/controller/scheduler.go` with the `NodeInfo` type, `ErrUnschedulable` sentinel, and `Schedule()` pure function. Create `scheduler_test.go` with table-driven tests (no envtest needed).

**Files:**
- Create: `internal/controller/scheduler.go`
- Create: `internal/controller/scheduler_test.go`

**Step 1: Write the tests first**

Create `internal/controller/scheduler_test.go`:

```go
package controller

import (
	"errors"
	"testing"

	"github.com/go-logr/logr"
)

func TestSchedule_singleNodeFits(t *testing.T) {
	nodes := []NodeInfo{{
		NodeName:      "node1",
		VCPUCapacity:  8,
		MemoryMiB:     8192,
		UsedVCPU:      2,
		UsedMemoryMiB: 1024,
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
		NodeName:      "node1",
		VCPUCapacity:  4,
		MemoryMiB:     4096,
		UsedVCPU:      3,
		UsedMemoryMiB: 4000,
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
		{NodeName: "node-a", VCPUCapacity: 8, MemoryMiB: 8192, UsedVCPU: 2, UsedMemoryMiB: 2048}, // free: 6 cpu, 6144 mem
		{NodeName: "node-b", VCPUCapacity: 8, MemoryMiB: 8192, UsedVCPU: 2, UsedMemoryMiB: 1024}, // free: 6 cpu, 7168 mem — wins
		{NodeName: "node-c", VCPUCapacity: 8, MemoryMiB: 8192, UsedVCPU: 2, UsedMemoryMiB: 4096}, // free: 6 cpu, 4096 mem
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
		{NodeName: "small", VCPUCapacity: 4, MemoryMiB: 8192, UsedVCPU: 3, UsedMemoryMiB: 0}, // only 1 free VCPU
		{NodeName: "large", VCPUCapacity: 8, MemoryMiB: 8192, UsedVCPU: 2, UsedMemoryMiB: 0}, // 6 free VCPUs
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
		{NodeName: "low-mem", VCPUCapacity: 8, MemoryMiB: 2048, UsedVCPU: 0, UsedMemoryMiB: 1900}, // only 148 MiB free
		{NodeName: "hi-mem",  VCPUCapacity: 8, MemoryMiB: 8192, UsedVCPU: 0, UsedMemoryMiB: 1024}, // 7168 MiB free
	}
	got, err := Schedule(logr.Discard(), 1, 512, nodes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "hi-mem" {
		t.Errorf("got %q, want %q", got, "hi-mem")
	}
}
```

**Step 2: Run tests to verify they fail**

```bash
go test ./internal/controller/ -run TestSchedule -v
```

Expected: compile error — `NodeInfo`, `ErrUnschedulable`, `Schedule` not defined.

**Step 3: Create `internal/controller/scheduler.go`**

```go
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
//  2. Log each candidate with fit result (debug level)
//  3. Tie-break: highest free memory (bin-packing)
//
// Returns ("", ErrUnschedulable) when no node has sufficient capacity.
func Schedule(log logr.Logger, vcpu int32, memMiB int64, nodes []NodeInfo) (string, error) {
	type candidate struct {
		name        string
		freeVCPU    int32
		freeMemMiB  int64
	}

	var candidates []candidate
	for _, n := range nodes {
		freeVCPU := n.VCPUCapacity - n.UsedVCPU
		freeMemMiB := n.MemoryMiB - n.UsedMemoryMiB
		fits := freeVCPU >= vcpu && freeMemMiB >= memMiB
		log.V(1).Info("scheduling candidate",
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

	// Tie-break: highest free memory (bin-packing — prefer most-loaded node that still fits).
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].freeMemMiB > candidates[j].freeMemMiB
	})
	return candidates[0].name, nil
}
```

**Step 4: Run tests to verify they pass**

```bash
go test ./internal/controller/ -run TestSchedule -v
```

Expected: all 6 tests pass.

**Step 5: Commit**

```bash
git add internal/controller/scheduler.go internal/controller/scheduler_test.go
git commit -m "feat(controller): add pure Schedule() function with resource-fit and debug logging"
```

---

## Task 7: Wire `Schedule()` into the operator scheduling path

Update `impvm_scheduler.go` to use `Schedule()` (with `NodeInfo` built from explicit capacity fields) when `VCPUCapacity > 0` on the node's `ClusterImpNodeProfile`. Fall back to the existing fraction-based logic when `VCPUCapacity == 0`.

**Files:**
- Modify: `internal/controller/impvm_scheduler.go`
- Modify: `internal/controller/impvm_coverage_test.go` (or add new test file)

**Step 1: Write the failing test**

The existing `impvm_scheduler.go` tests are in `impvm_coverage_test.go`. Add a test that exercises the explicit-capacity path (envtest-based):

Find the existing schedule test in `impvm_coverage_test.go`. Add a new `It` block (or table row) for "explicit capacity via ClusterImpNodeProfile":

```go
It("picks node with explicit capacity when VCPUCapacity is set on profile", func() {
	// Create a node with imp/enabled=true.
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "cap-node",
			Labels: map[string]string{"imp/enabled": "true"},
		},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{{
				Type:   corev1.NodeReady,
				Status: corev1.ConditionTrue,
			}},
		},
	}
	Expect(k8sClient.Create(ctx, node)).To(Succeed())

	// Create a ClusterImpNodeProfile with explicit VCPUCapacity.
	profile := &impdevv1alpha1.ClusterImpNodeProfile{
		ObjectMeta: metav1.ObjectMeta{Name: "cap-node"},
		Spec: impdevv1alpha1.ClusterImpNodeProfileSpec{
			VCPUCapacity: 8,
			MemoryMiB:    8192,
		},
	}
	Expect(k8sClient.Create(ctx, profile)).To(Succeed())

	// Create a class with known resources.
	class := &impdevv1alpha1.ImpVMClass{
		ObjectMeta: metav1.ObjectMeta{Name: "small-explicit"},
		Spec: impdevv1alpha1.ImpVMClassSpec{VCPU: 2, MemoryMiB: 512, DiskGiB: 10},
	}
	Expect(k8sClient.Create(ctx, class)).To(Succeed())

	// Create a VM that needs scheduling.
	vm := &impdevv1alpha1.ImpVM{
		ObjectMeta: metav1.ObjectMeta{Name: "vm-cap-test", Namespace: "default"},
		Spec: impdevv1alpha1.ImpVMSpec{
			Image:    "docker.io/library/alpine:latest",
			ClassRef: &corev1.ObjectReference{Name: "small-explicit"},
		},
	}
	Expect(k8sClient.Create(ctx, vm)).To(Succeed())

	// Wait for it to be scheduled.
	Eventually(func(g Gomega) {
		fetched := &impdevv1alpha1.ImpVM{}
		g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "vm-cap-test", Namespace: "default"}, fetched)).To(Succeed())
		g.Expect(fetched.Spec.NodeName).To(Equal("cap-node"))
	}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
})
```

**Step 2: Run test to verify it passes with current code**

The test exercises the scheduling path. Confirm it passes (it should, since the existing fraction logic will still schedule to `cap-node` as long as there's only one node). The key is that after Task 7, the test also verifies the explicit-capacity code path is exercised without breaking existing behavior.

```bash
KUBEBUILDER_ASSETS=$(go run sigs.k8s.io/controller-runtime/tools/setup-envtest@latest use -p path 2>/dev/null) go test ./internal/controller/ -run "TestImpVM" -v
```

**Step 3: Add `sumUsedResources` helper to `impvm_scheduler.go`**

Add before the `schedule` method:

```go
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
```

**Step 4: Update `schedule()` to use `Schedule()` when profile has explicit capacity**

In `impvm_scheduler.go`, after fetching `allVMs` and calling `resolveClassSpec` for `vmVCPU`/`vmMemMiB`, add the explicit-capacity path.

Find the section starting at "5. Fetch global default fraction..." and insert **before** it:

```go
// 5a. Use explicit-capacity scheduling when any eligible node has VCPUCapacity set.
// Build NodeInfo for nodes that have a profile with VCPUCapacity > 0.
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
	// for nodes without a profile, if any.
}
```

> **Important:** The fallthrough to fraction-based scheduling ensures VMs can still be scheduled on nodes without explicit profiles. This preserves backward compatibility.

**Step 5: Run full test suite**

```bash
KUBEBUILDER_ASSETS=$(go run sigs.k8s.io/controller-runtime/tools/setup-envtest@latest use -p path 2>/dev/null) go test ./...
```

Expected: all pass.

**Step 6: Lint**

```bash
golangci-lint run ./...
```

Fix any issues (typically unused imports or `gosec` int cast warnings).

**Step 7: Commit**

```bash
git add internal/controller/impvm_scheduler.go internal/controller/impvm_coverage_test.go
git commit -m "feat(controller): resource-aware scheduling via explicit VCPUCapacity/MemoryMiB"
```

---

## Final Verification

After all 7 tasks:

```bash
cd /Users/giovanni/.config/superpowers/worktrees/imp/phase2-deferred

# Full test suite
KUBEBUILDER_ASSETS=$(go run sigs.k8s.io/controller-runtime/tools/setup-envtest@latest use -p path 2>/dev/null) go test ./...

# Cross-compile for Linux (catches linux-only build tags)
GOOS=linux go build ./...

# Lint
golangci-lint run ./...
```

Expected: all pass, no lint errors.
