# CNI Detection Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement CNI auto-detection in the operator so it can select the correct NAT backend (nftables vs iptables) for each cluster's networking stack.

**Architecture:** A pure `internal/cnidetect` package exposes a `Detect(ctx, client)` function that inspects the cluster's REST mapper (for CRD presence) and the kube-system DaemonSets (for CNI agents). The result is stored in a thread-safe `Store`, wired into the operator via a one-shot `manager.Runnable` in `main.go`, and emits a `CNIDetected` (or `CNIAmbiguous`) Kubernetes Event on the `ClusterImpConfig` singleton.

**Tech Stack:** Go, controller-runtime fake client (`sigs.k8s.io/controller-runtime/pkg/client/fake`), `k8s.io/apimachinery/pkg/api/meta` for REST mapper, table-driven `testing.T` (no envtest needed — pure unit tests).

---

## Detection Logic (design doc §9.1)

```
Priority:
1. Explicit: ClusterImpConfig.spec.networking.cni.provider is set → use it, skip detection
2. CRD presence via REST mapper:
     ciliumnetworkpolicies.cilium.io → Cilium
     globalnetworkpolicies.projectcalico.org → Calico
3. DaemonSet check in kube-system (403 handled gracefully):
     cilium-agent      → Cilium
     kube-flannel-ds   → Flannel
     calico-node       → Calico
4. Multiple CNIs detected → Ambiguous, iptables fallback
5. Nothing detected    → Unknown, iptables fallback
```

## NAT Backend by Provider (design doc §9.2)

| Provider | NATBackend |
|----------|------------|
| `cilium` | `nftables` |
| `cilium-kubeproxy-free` | `nftables` |
| `flannel` / `calico` / `unknown` | `iptables` |

---

### Task 1: Add CNI event reason constants

**Files:**
- Modify: `internal/controller/events.go`

No test needed — these are just constants.

**Step 1: Add the constants**

```go
// in the existing Event reason constants block
EventReasonCNIDetected  = "CNIDetected"
EventReasonCNIAmbiguous = "CNIAmbiguous"
```

**Step 2: Verify the file compiles**

```bash
go build ./internal/controller/...
```

Expected: no output (clean build).

**Step 3: Commit**

```bash
git add internal/controller/events.go
git commit -m "feat(controller): add CNIDetected/CNIAmbiguous event reason constants"
```

---

### Task 2: Write failing tests for `cnidetect.Detect`

**Files:**
- Create: `internal/cnidetect/detect_test.go`

**Step 1: Create the test file**

```go
package cnidetect_test

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
	"github.com/syscode-labs/imp/internal/cnidetect"
)

func buildScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	_ = impdevv1alpha1.AddToScheme(s)
	return s
}

// buildMapper returns a REST mapper that reports the given CRD groups as present.
// Pass "cilium.io" to simulate CiliumNetworkPolicy CRD,
// "projectcalico.org" to simulate GlobalNetworkPolicy CRD.
func buildMapper(crdGroups ...string) meta.RESTMapper {
	mapper := meta.NewDefaultRESTMapper(nil)
	for _, group := range crdGroups {
		switch group {
		case "cilium.io":
			mapper.Add(schema.GroupVersionKind{
				Group: "cilium.io", Version: "v2", Kind: "CiliumNetworkPolicy",
			}, meta.RESTScopeNamespace)
		case "projectcalico.org":
			mapper.Add(schema.GroupVersionKind{
				Group: "projectcalico.org", Version: "v3", Kind: "GlobalNetworkPolicy",
			}, meta.RESTScopeRoot)
		}
	}
	return mapper
}

func ds(name string) *appsv1.DaemonSet {
	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "kube-system"},
	}
}

func TestDetect(t *testing.T) {
	tests := []struct {
		name        string
		crdGroups   []string
		daemonSets  []*appsv1.DaemonSet
		explicitCfg string // ClusterImpConfig.spec.networking.cni.provider
		want        cnidetect.Result
	}{
		{
			name: "no signals → unknown with iptables",
			want: cnidetect.Result{Provider: cnidetect.ProviderUnknown, NATBackend: cnidetect.NATBackendIPTables},
		},
		{
			name:      "CiliumNetworkPolicy CRD → cilium with nftables",
			crdGroups: []string{"cilium.io"},
			want:      cnidetect.Result{Provider: cnidetect.ProviderCilium, NATBackend: cnidetect.NATBackendNftables},
		},
		{
			name:      "GlobalNetworkPolicy CRD → calico with iptables",
			crdGroups: []string{"projectcalico.org"},
			want:      cnidetect.Result{Provider: cnidetect.ProviderCalico, NATBackend: cnidetect.NATBackendIPTables},
		},
		{
			name:       "cilium-agent DaemonSet → cilium with nftables",
			daemonSets: []*appsv1.DaemonSet{ds("cilium-agent")},
			want:       cnidetect.Result{Provider: cnidetect.ProviderCilium, NATBackend: cnidetect.NATBackendNftables},
		},
		{
			name:       "kube-flannel-ds DaemonSet → flannel with iptables",
			daemonSets: []*appsv1.DaemonSet{ds("kube-flannel-ds")},
			want:       cnidetect.Result{Provider: cnidetect.ProviderFlannel, NATBackend: cnidetect.NATBackendIPTables},
		},
		{
			name:       "calico-node DaemonSet → calico with iptables",
			daemonSets: []*appsv1.DaemonSet{ds("calico-node")},
			want:       cnidetect.Result{Provider: cnidetect.ProviderCalico, NATBackend: cnidetect.NATBackendIPTables},
		},
		{
			name:      "cilium CRD + calico DaemonSet → ambiguous with iptables",
			crdGroups: []string{"cilium.io"},
			daemonSets: []*appsv1.DaemonSet{ds("calico-node")},
			want:       cnidetect.Result{Provider: cnidetect.ProviderUnknown, NATBackend: cnidetect.NATBackendIPTables, Ambiguous: true},
		},
		{
			name:        "explicit provider cilium-kubeproxy-free → used as-is with nftables",
			explicitCfg: "cilium-kubeproxy-free",
			want:        cnidetect.Result{Provider: cnidetect.ProviderCiliumKubeProxyFree, NATBackend: cnidetect.NATBackendNftables},
		},
		{
			name:        "explicit provider overrides CRD signals",
			explicitCfg: "flannel",
			crdGroups:   []string{"cilium.io"}, // would detect cilium without explicit config
			want:        cnidetect.Result{Provider: cnidetect.ProviderFlannel, NATBackend: cnidetect.NATBackendIPTables},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			scheme := buildScheme()
			mapper := buildMapper(tc.crdGroups...)

			var objs []runtime.Object
			for _, d := range tc.daemonSets {
				objs = append(objs, d)
			}
			if tc.explicitCfg != "" {
				objs = append(objs, &impdevv1alpha1.ClusterImpConfig{
					ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
					Spec: impdevv1alpha1.ClusterImpConfigSpec{
						Networking: impdevv1alpha1.NetworkingConfig{
							CNI: impdevv1alpha1.CNIConfig{Provider: tc.explicitCfg},
						},
					},
				})
			}

			c := fake.NewClientBuilder().
				WithScheme(scheme).
				WithRESTMapper(mapper).
				WithRuntimeObjects(objs...).
				Build()

			got, err := cnidetect.Detect(context.Background(), c)
			if err != nil {
				t.Fatalf("Detect() error = %v", err)
			}
			if got != tc.want {
				t.Errorf("Detect() = %+v, want %+v", got, tc.want)
			}
		})
	}
}
```

**Step 2: Run to confirm the package doesn't exist yet**

```bash
go test ./internal/cnidetect/... 2>&1
```

Expected: `cannot find package "github.com/syscode-labs/imp/internal/cnidetect"`

---

### Task 3: Implement `internal/cnidetect/detect.go`

**Files:**
- Create: `internal/cnidetect/detect.go`

**Step 1: Create the file**

```go
package cnidetect

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

// Provider identifies the CNI plugin running in the cluster.
type Provider string

const (
	ProviderCiliumKubeProxyFree Provider = "cilium-kubeproxy-free"
	ProviderCilium              Provider = "cilium"
	ProviderFlannel             Provider = "flannel"
	ProviderCalico              Provider = "calico"
	ProviderUnknown             Provider = "unknown"
)

// NATBackend selects the kernel NAT implementation for ImpNetwork rules.
type NATBackend string

const (
	NATBackendNftables NATBackend = "nftables"
	NATBackendIPTables NATBackend = "iptables"
)

// Result is the output of a CNI detection run.
type Result struct {
	Provider   Provider
	NATBackend NATBackend
	Ambiguous  bool // true if multiple CNIs were detected simultaneously
}

// Detect inspects the cluster and returns the active CNI and appropriate NAT backend.
//
// Detection priority (design doc §9.1):
//  1. Explicit ClusterImpConfig.spec.networking.cni.provider — skips all detection.
//  2. CRD presence via REST mapper:
//     ciliumnetworkpolicies.cilium.io → Cilium
//     globalnetworkpolicies.projectcalico.org → Calico
//  3. DaemonSet presence in kube-system (403 errors handled gracefully):
//     cilium-agent, kube-flannel-ds, calico-node
//  4. Multiple signals → Ambiguous, iptables fallback.
//  5. No signals → Unknown, iptables fallback.
func Detect(ctx context.Context, c client.Client) (Result, error) {
	log := logf.FromContext(ctx).WithName("cnidetect")

	// 1. Explicit provider in ClusterImpConfig singleton.
	cfg := &impdevv1alpha1.ClusterImpConfig{}
	if err := c.Get(ctx, client.ObjectKey{Name: "cluster"}, cfg); err == nil {
		if p := cfg.Spec.Networking.CNI.Provider; p != "" {
			log.Info("using explicit CNI provider", "provider", p)
			return resultFromProvider(Provider(p)), nil
		}
	}

	// 2. CRD-based detection via REST mapper (no apiextensions scheme needed).
	var signals []Provider
	if hasCRD(c, "cilium.io", "ciliumnetworkpolicies") {
		signals = append(signals, ProviderCilium)
	}
	if hasCRD(c, "projectcalico.org", "globalnetworkpolicies") {
		signals = append(signals, ProviderCalico)
	}

	// 3. DaemonSet-based detection (graceful error handling).
	if !containsProvider(signals, ProviderCilium) {
		if hasDaemonSet(ctx, c, "cilium-agent") {
			signals = append(signals, ProviderCilium)
		}
	}
	if !containsProvider(signals, ProviderFlannel) {
		if hasDaemonSet(ctx, c, "kube-flannel-ds") {
			signals = append(signals, ProviderFlannel)
		}
	}
	if !containsProvider(signals, ProviderCalico) {
		if hasDaemonSet(ctx, c, "calico-node") {
			signals = append(signals, ProviderCalico)
		}
	}

	// 4+5. Resolve signals.
	switch len(signals) {
	case 0:
		log.Info("no CNI detected, using iptables fallback")
		return Result{Provider: ProviderUnknown, NATBackend: NATBackendIPTables}, nil
	case 1:
		log.Info("CNI detected", "provider", signals[0])
		return resultFromProvider(signals[0]), nil
	default:
		log.Info("multiple CNIs detected, ambiguous", "providers", signals)
		return Result{Provider: ProviderUnknown, NATBackend: NATBackendIPTables, Ambiguous: true}, nil
	}
}

// hasCRD returns true if a CRD for the given group+resource exists in the cluster's REST mapper.
func hasCRD(c client.Client, group, resource string) bool {
	mappings, err := c.RESTMapper().ResourcesFor(schema.GroupVersionResource{
		Group:    group,
		Resource: resource,
	})
	return err == nil && len(mappings) > 0
}

// hasDaemonSet returns true if the named DaemonSet exists in kube-system.
// Returns false on any error (including 403 Forbidden) to remain graceful in
// clusters where the operator has minimal RBAC (design doc §9.4).
func hasDaemonSet(ctx context.Context, c client.Client, name string) bool {
	ds := &appsv1.DaemonSet{}
	err := c.Get(ctx, client.ObjectKey{Namespace: "kube-system", Name: name}, ds)
	if err == nil {
		return true
	}
	if !apierrors.IsNotFound(err) {
		// Log 403 / other errors but don't propagate them.
		logf.FromContext(ctx).V(1).Info("DaemonSet check skipped", "name", name, "err", err)
	}
	return false
}

// resultFromProvider maps a Provider to its correct NAT backend.
func resultFromProvider(p Provider) Result {
	switch p {
	case ProviderCilium, ProviderCiliumKubeProxyFree:
		return Result{Provider: p, NATBackend: NATBackendNftables}
	default:
		return Result{Provider: p, NATBackend: NATBackendIPTables}
	}
}

func containsProvider(providers []Provider, p Provider) bool {
	for _, x := range providers {
		if x == p {
			return true
		}
	}
	return false
}
```

**Step 2: Run the tests**

```bash
go test ./internal/cnidetect/... -v
```

Expected: all 8 cases PASS.

**Step 3: Commit**

```bash
git add internal/cnidetect/detect.go internal/cnidetect/detect_test.go
git commit -m "feat(cnidetect): Detect function with CRD + DaemonSet heuristics"
```

---

### Task 4: Thread-safe `Store` for the detection result

The `Store` is a lightweight holder so the future `ImpNetwork` controller can read the CNI result without re-running detection.

**Files:**
- Create: `internal/cnidetect/store.go`
- Create: `internal/cnidetect/store_test.go`

**Step 1: Write failing test**

```go
// internal/cnidetect/store_test.go
package cnidetect_test

import (
	"testing"

	"github.com/syscode-labs/imp/internal/cnidetect"
)

func TestStore(t *testing.T) {
	s := &cnidetect.Store{}

	// Unset returns false.
	if _, ok := s.Result(); ok {
		t.Fatal("expected no result before Set")
	}

	want := cnidetect.Result{Provider: cnidetect.ProviderCilium, NATBackend: cnidetect.NATBackendNftables}
	s.Set(want)

	got, ok := s.Result()
	if !ok {
		t.Fatal("expected result after Set")
	}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}
```

**Step 2: Run to confirm it fails**

```bash
go test ./internal/cnidetect/... -run TestStore -v
```

Expected: `undefined: cnidetect.Store`

**Step 3: Implement `store.go`**

```go
// internal/cnidetect/store.go
package cnidetect

import "sync"

// Store holds the result of a CNI detection run.
// It is set once at operator startup and then read by controllers.
type Store struct {
	mu     sync.RWMutex
	result *Result
}

// Set stores the detection result. Safe to call from any goroutine.
func (s *Store) Set(r Result) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r2 := r
	s.result = &r2
}

// Result returns the stored detection result and true, or zero value + false
// if detection has not yet completed.
func (s *Store) Result() (Result, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.result == nil {
		return Result{}, false
	}
	return *s.result, true
}
```

**Step 4: Run all cnidetect tests**

```bash
go test ./internal/cnidetect/... -v
```

Expected: all tests PASS.

**Step 5: Commit**

```bash
git add internal/cnidetect/store.go internal/cnidetect/store_test.go
git commit -m "feat(cnidetect): thread-safe Store for CNI detection result"
```

---

### Task 5: Wire CNI detection into operator startup

**Files:**
- Modify: `cmd/operator/main.go`

The detection runs as a one-shot `manager.Runnable` — controller-runtime calls `Start(ctx)` after the cache is synced, so client reads work correctly.

**Step 1: Add the Runnable to `main.go`**

Add the following imports:
```go
corev1 "k8s.io/api/core/v1"
"k8s.io/client-go/tools/record"

"github.com/syscode-labs/imp/internal/cnidetect"
"github.com/syscode-labs/imp/internal/controller"
```

Add a `cniDetectRunnable` type below the existing `init()`:
```go
type cniDetectRunnable struct {
	client   client.Client
	recorder record.EventRecorder
	store    *cnidetect.Store
}

func (r *cniDetectRunnable) Start(ctx context.Context) error {
	log := ctrl.Log.WithName("cni-detect")

	result, err := cnidetect.Detect(ctx, r.client)
	if err != nil {
		return err
	}
	r.store.Set(result)

	// Emit event on the ClusterImpConfig singleton (best-effort; skip if absent).
	cfg := &impv1alpha1.ClusterImpConfig{}
	if getErr := r.client.Get(ctx, client.ObjectKey{Name: "cluster"}, cfg); getErr == nil {
		if result.Ambiguous {
			r.recorder.Event(cfg, corev1.EventTypeWarning,
				controller.EventReasonCNIAmbiguous,
				"Multiple CNIs detected; using iptables fallback. Set spec.networking.cni.provider explicitly.")
		} else {
			r.recorder.Eventf(cfg, corev1.EventTypeNormal,
				controller.EventReasonCNIDetected,
				"CNI detected: provider=%s natBackend=%s", result.Provider, result.NATBackend)
		}
	}

	log.Info("CNI detection complete",
		"provider", result.Provider,
		"natBackend", result.NATBackend,
		"ambiguous", result.Ambiguous)
	return nil
}
```

In `main()`, before `mgr.Start(ctrl.SetupSignalHandler())`, add:
```go
cniStore := &cnidetect.Store{}
if err := mgr.Add(&cniDetectRunnable{
    client:   mgr.GetClient(),
    recorder: mgr.GetEventRecorderFor("cni-detector"),
    store:    cniStore,
}); err != nil {
    setupLog.Error(err, "unable to register cni-detect runnable")
    os.Exit(1)
}
```

**Step 2: Build check**

```bash
go build ./cmd/operator/...
```

Expected: clean build, no errors.

**Step 3: Run all tests**

```bash
go test ./internal/cnidetect/... ./internal/controller/...
```

Expected: all PASS.

**Step 4: Commit**

```bash
git add cmd/operator/main.go
git commit -m "feat(operator): run CNI detection at startup, emit CNIDetected event"
```

---

## Summary

After completing all tasks:

- `internal/cnidetect/detect.go` — `Detect()` function with CRD + DaemonSet heuristics
- `internal/cnidetect/store.go` — thread-safe `Store` for sharing the result with future controllers
- `internal/controller/events.go` — `CNIDetected` / `CNIAmbiguous` event reason constants
- `cmd/operator/main.go` — `cniDetectRunnable` wires detection into the manager lifecycle

The `cniStore` pointer created in `main.go` is ready to be injected into the `ImpNetwork` controller in the next task.
