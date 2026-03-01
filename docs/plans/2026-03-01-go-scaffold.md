# Go Scaffold Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Bootstrap the Imp Go project — module init, all 6 CRD type definitions, operator and agent entrypoints, code generation, and Dockerfiles. No controller logic; this is the compilable, testable skeleton.

**Architecture:** kubebuilder v4 scaffolds the project layout, go.mod, Makefile, and config/ kustomize tree. CRD types are then written one by one with kubebuilder markers. Two separate binaries (`cmd/operator`, `cmd/agent`) replace the single default `cmd/main.go`.

**Tech Stack:** Go 1.23, kubebuilder v4, controller-runtime v0.19, controller-gen, setup-envtest, kustomize

---

## Reference

Design doc: `docs/plans/2026-03-01-imp-design.md`

API group: `imp.dev/v1alpha1`

| Kind | Scope | Short |
|------|-------|-------|
| `ImpVM` | Namespace | `impvm` |
| `ImpVMTemplate` | Namespace | `impvmt` |
| `ImpNetwork` | Namespace | `impnet` |
| `ImpVMClass` | Cluster | `impcls` |
| `ClusterImpConfig` | Cluster | `impcfg` |
| `ClusterImpNodeProfile` | Cluster | `impnp` |

---

## Task 1: Install tooling

**Files:** none (system installs)

**Step 1: Install kubebuilder**

```bash
brew install kubebuilder
kubebuilder version
```

Expected: prints version like `Version: main.version{...KubeBuilderVersion:"4.x.x"...}`

**Step 2: Install controller-gen and setup-envtest**

```bash
go install sigs.k8s.io/controller-tools/cmd/controller-gen@latest
go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest
echo "$(go env GOPATH)/bin" # verify this is in your PATH
```

**Step 3: Verify PATH**

```bash
controller-gen --version
setup-envtest --version
```

Expected: both print version strings without error.

---

## Task 2: Scaffold kubebuilder project

**Files:**
- Create: `go.mod`, `go.sum`, `Makefile`, `PROJECT`, `hack/boilerplate.go.txt`
- Create: `config/` tree (crd, rbac, manager, default, samples)
- Create: `cmd/main.go` (temporary — will be split in Task 11)

**Step 1: Run kubebuilder init**

```bash
cd /Users/giovanni/syscode/git/imp
kubebuilder init --domain imp.dev --repo github.com/syscode-labs/imp --plugins go/v4
```

> **Note on API group:** `--domain imp.dev` means all resources use group `imp.dev`.
> When running `create api`, always pass `--group ""` (empty string) to avoid creating
> `something.imp.dev` — we want exactly `imp.dev/v1alpha1`.

**Step 2: Verify it compiles**

```bash
go build ./...
```

Expected: no errors, produces nothing (no binaries yet).

**Step 3: Scaffold ImpVM resource + controller**

ImpVM is the primary resource. Scaffold it to get the API directory structure and controller stub set up:

```bash
kubebuilder create api \
  --group "" \
  --version v1alpha1 \
  --kind ImpVM \
  --resource \
  --controller
```

When prompted: `Create Resource? [y/n]` → y, `Create Controller? [y/n]` → y

**Step 4: Verify groupversion_info.go has the right group**

```bash
grep 'GroupVersion' api/v1alpha1/groupversion_info.go
```

Expected output contains: `Group: "imp.dev"`. If it says `Group: ".imp.dev"` (leading dot), edit the file and remove the leading dot.

**Step 5: Verify compile + generated test suite**

```bash
go build ./...
```

Expected: compiles cleanly.

**Step 6: Commit**

```bash
git add -A
git commit -m "scaffold: kubebuilder init with ImpVM resource and controller stub"
```

---

## Task 3: Shared types

All CRDs share probe definitions, object references, and enums. Define these in a single shared file so they're not duplicated.

**Files:**
- Create: `api/v1alpha1/shared_types.go`
- Create: `api/v1alpha1/shared_types_test.go`

**Step 1: Write the failing test**

Create `api/v1alpha1/shared_types_test.go`:

```go
package v1alpha1

import (
	"encoding/json"
	"testing"
)

func TestProbeSpec_RoundTrip(t *testing.T) {
	original := ProbeSpec{
		StartupProbe: &Probe{
			Exec:                &ExecAction{Command: []string{"systemctl", "is-system-running"}},
			InitialDelaySeconds: 2,
			PeriodSeconds:       1,
			FailureThreshold:    30,
		},
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got ProbeSpec
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.StartupProbe == nil || got.StartupProbe.Exec == nil {
		t.Fatal("StartupProbe.Exec lost in round-trip")
	}
	if got.StartupProbe.Exec.Command[0] != "systemctl" {
		t.Fatalf("unexpected command: %v", got.StartupProbe.Exec.Command)
	}
}

func TestVMLifecycle_Values(t *testing.T) {
	cases := []VMLifecycle{VMLifecycleEphemeral, VMLifecyclePersistent}
	for _, c := range cases {
		data, _ := json.Marshal(c)
		var got VMLifecycle
		json.Unmarshal(data, &got) //nolint:errcheck
		if got != c {
			t.Errorf("round-trip failed for %q", c)
		}
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./api/v1alpha1/ -run TestProbeSpec_RoundTrip -v
```

Expected: FAIL — `ProbeSpec undefined`

**Step 3: Write the shared types**

Create `api/v1alpha1/shared_types.go`:

```go
package v1alpha1

// LocalObjectRef is a reference to an object in the same namespace.
type LocalObjectRef struct {
	Name string `json:"name"`
}

// ClusterObjectRef is a reference to a cluster-scoped object.
type ClusterObjectRef struct {
	Name string `json:"name"`
}

// ProbeSpec holds optional startup, readiness, and liveness probes.
// The most specific definition in the inheritance chain wins:
// ImpVMClass → ImpVMTemplate → ImpVM.
type ProbeSpec struct {
	StartupProbe   *Probe `json:"startupProbe,omitempty"`
	ReadinessProbe *Probe `json:"readinessProbe,omitempty"`
	LivenessProbe  *Probe `json:"livenessProbe,omitempty"`
}

// Probe defines a single probe. Only one of Exec or HTTP may be set.
type Probe struct {
	// +optional
	Exec *ExecAction `json:"exec,omitempty"`
	// +optional
	HTTP *HTTPGetAction `json:"http,omitempty"`

	// +optional
	// +kubebuilder:default=0
	InitialDelaySeconds int32 `json:"initialDelaySeconds,omitempty"`
	// +optional
	// +kubebuilder:default=10
	PeriodSeconds int32 `json:"periodSeconds,omitempty"`
	// +optional
	// +kubebuilder:default=3
	FailureThreshold int32 `json:"failureThreshold,omitempty"`
}

// ExecAction runs a command inside the VM via VSOCK guest agent.
type ExecAction struct {
	// +kubebuilder:validation:MinItems=1
	Command []string `json:"command"`
}

// HTTPGetAction performs an HTTP GET against the VM's guest agent.
type HTTPGetAction struct {
	Path string `json:"path"`
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	Port int32 `json:"port"`
}

// VMLifecycle controls what happens when an ImpVM finishes.
// +kubebuilder:validation:Enum=ephemeral;persistent
type VMLifecycle string

const (
	// VMLifecycleEphemeral deletes the VM once its workload exits.
	VMLifecycleEphemeral VMLifecycle = "ephemeral"
	// VMLifecyclePersistent keeps the VM running until explicitly deleted.
	VMLifecyclePersistent VMLifecycle = "persistent"
)

// VMPhase is the current lifecycle phase of an ImpVM.
// +kubebuilder:validation:Enum=Pending;Scheduling;Starting;Running;Stopping;Stopped;Failed
type VMPhase string

const (
	VMPhasePending    VMPhase = "Pending"
	VMPhaseScheduling VMPhase = "Scheduling"
	VMPhaseStarting   VMPhase = "Starting"
	VMPhaseRunning    VMPhase = "Running"
	VMPhaseStopping   VMPhase = "Stopping"
	VMPhaseStopped    VMPhase = "Stopped"
	VMPhaseFailed     VMPhase = "Failed"
)

// Arch is the CPU architecture for a VM class.
// +kubebuilder:validation:Enum=amd64;arm64;multi
type Arch string

const (
	ArchAMD64 Arch = "amd64"
	ArchARM64 Arch = "arm64"
	ArchMulti Arch = "multi"
)
```

**Step 4: Run tests to verify they pass**

```bash
go test ./api/v1alpha1/ -run "TestProbeSpec|TestVMLifecycle" -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add api/v1alpha1/shared_types.go api/v1alpha1/shared_types_test.go
git commit -m "api: add shared types (ProbeSpec, VMLifecycle, VMPhase, Arch)"
```

---

## Task 4: Complete ImpVM type

The kubebuilder scaffold generated `api/v1alpha1/impvm_types.go` with empty spec/status. Fill it in from the design doc.

**Files:**
- Modify: `api/v1alpha1/impvm_types.go`
- Create: `api/v1alpha1/impvm_types_test.go`

**Step 1: Write the failing test**

Create `api/v1alpha1/impvm_types_test.go`:

```go
package v1alpha1

import (
	"encoding/json"
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestImpVMSpec_TemplateRef_XOR_ClassRef(t *testing.T) {
	// A VM with only templateRef should round-trip cleanly
	vm := ImpVMSpec{
		TemplateRef: &LocalObjectRef{Name: "ubuntu-sandbox"},
		Lifecycle:   VMLifecycleEphemeral,
	}
	data, err := json.Marshal(vm)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got ImpVMSpec
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.TemplateRef == nil || got.TemplateRef.Name != "ubuntu-sandbox" {
		t.Fatal("TemplateRef lost in round-trip")
	}
	if got.ClassRef != nil {
		t.Fatal("ClassRef should be nil")
	}
}

func TestImpVMSpec_EnvVars(t *testing.T) {
	vm := ImpVMSpec{
		ClassRef: &ClusterObjectRef{Name: "small"},
		Image:    "ghcr.io/myorg/my-app:latest",
		Env: []corev1.EnvVar{
			{Name: "PORT", Value: "8080"},
		},
	}
	data, _ := json.Marshal(vm)
	var got ImpVMSpec
	json.Unmarshal(data, &got) //nolint:errcheck
	if len(got.Env) != 1 || got.Env[0].Name != "PORT" {
		t.Fatalf("Env lost in round-trip: %+v", got.Env)
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./api/v1alpha1/ -run "TestImpVMSpec" -v
```

Expected: FAIL — `ImpVMSpec` fields are missing

**Step 3: Replace the generated ImpVM types**

Edit `api/v1alpha1/impvm_types.go` — replace the empty Spec/Status structs:

```go
package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ImpVMSpec defines the desired state of an ImpVM.
// Exactly one of TemplateRef or ClassRef must be set.
type ImpVMSpec struct {
	// TemplateRef references an ImpVMTemplate. Mutually exclusive with ClassRef.
	// +optional
	TemplateRef *LocalObjectRef `json:"templateRef,omitempty"`

	// ClassRef references an ImpVMClass directly. Mutually exclusive with TemplateRef.
	// +optional
	ClassRef *ClusterObjectRef `json:"classRef,omitempty"`

	// NetworkRef references the ImpNetwork to attach this VM to.
	// +optional
	NetworkRef *LocalObjectRef `json:"networkRef,omitempty"`

	// Image is the OCI image used as the VM rootfs. CMD/ENTRYPOINT from the
	// manifest become the VM's init command. Required when ClassRef is set directly.
	// +optional
	Image string `json:"image,omitempty"`

	// Lifecycle controls VM behaviour after the workload exits.
	// +optional
	// +kubebuilder:default=ephemeral
	Lifecycle VMLifecycle `json:"lifecycle,omitempty"`

	// NodeName pins the VM to a specific node. Set by the operator scheduler;
	// do not set manually unless you know what you're doing.
	// +optional
	NodeName string `json:"nodeName,omitempty"`

	// NodeSelector constrains scheduling to nodes matching all labels.
	// Same semantics as Pod.spec.nodeSelector.
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// Env sets environment variables inside the VM via the guest agent.
	// +optional
	Env []corev1.EnvVar `json:"env,omitempty"`

	// EnvFrom populates env vars from ConfigMaps or Secrets.
	// +optional
	EnvFrom []corev1.EnvFromSource `json:"envFrom,omitempty"`

	// UserData is an optional cloud-init user-data payload. Off by default;
	// use the guest agent + VSOCK for env injection instead.
	// +optional
	UserData *UserDataSource `json:"userData,omitempty"`

	// Probes override probe settings inherited from ImpVMTemplate or ImpVMClass.
	// +optional
	Probes *ProbeSpec `json:"probes,omitempty"`
}

// UserDataSource references a ConfigMap containing cloud-init user-data.
type UserDataSource struct {
	ConfigMapRef LocalObjectRef `json:"configMapRef"`
}

// ImpVMStatus reflects the observed state of an ImpVM.
type ImpVMStatus struct {
	// Phase is the current lifecycle phase.
	// +optional
	Phase VMPhase `json:"phase,omitempty"`

	// NodeName is the node where the VM is running.
	// +optional
	NodeName string `json:"nodeName,omitempty"`

	// IP is the IP address assigned to the VM by the ImpNetwork.
	// +optional
	IP string `json:"ip,omitempty"`

	// FirecrackerPID is the PID of the Firecracker process on the node (informational).
	// +optional
	FirecrackerPID int64 `json:"firecrackerPID,omitempty"`

	// Conditions follow the standard k8s condition convention.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=impvm,categories=imp
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Node",type=string,JSONPath=`.status.nodeName`
// +kubebuilder:printcolumn:name="IP",type=string,JSONPath=`.status.ip`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// ImpVM is the Schema for the impvms API.
type ImpVM struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ImpVMSpec   `json:"spec,omitempty"`
	Status ImpVMStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ImpVMList contains a list of ImpVM.
type ImpVMList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ImpVM `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ImpVM{}, &ImpVMList{})
}
```

**Step 4: Run tests to verify they pass**

```bash
go test ./api/v1alpha1/ -run "TestImpVMSpec" -v
```

Expected: PASS

**Step 5: Verify compile**

```bash
go build ./...
```

**Step 6: Commit**

```bash
git add api/v1alpha1/impvm_types.go api/v1alpha1/impvm_types_test.go
git commit -m "api: define ImpVM spec and status"
```

---

## Task 5: ImpVMClass type

Cluster-scoped resource. No controller — the operator reads it during scheduling.

**Files:**
- Create: `api/v1alpha1/impvmclass_types.go`
- Create: `api/v1alpha1/impvmclass_types_test.go`

**Step 1: Write the failing test**

Create `api/v1alpha1/impvmclass_types_test.go`:

```go
package v1alpha1

import (
	"encoding/json"
	"testing"
)

func TestImpVMClassSpec_RoundTrip(t *testing.T) {
	cls := ImpVMClassSpec{
		VCPU:      2,
		MemoryMiB: 1024,
		DiskGiB:   20,
		Arch:      ArchMulti,
		Probes: &ProbeSpec{
			StartupProbe: &Probe{
				Exec:             &ExecAction{Command: []string{"systemctl", "is-system-running"}},
				PeriodSeconds:    1,
				FailureThreshold: 30,
			},
		},
	}
	data, err := json.Marshal(cls)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got ImpVMClassSpec
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.VCPU != 2 || got.MemoryMiB != 1024 || got.DiskGiB != 20 {
		t.Fatalf("compute fields wrong: %+v", got)
	}
	if got.Arch != ArchMulti {
		t.Fatalf("arch wrong: %v", got.Arch)
	}
	if got.Probes == nil || got.Probes.StartupProbe == nil {
		t.Fatal("Probes lost in round-trip")
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./api/v1alpha1/ -run TestImpVMClassSpec_RoundTrip -v
```

Expected: FAIL — `ImpVMClassSpec undefined`

**Step 3: Write the type**

Create `api/v1alpha1/impvmclass_types.go`:

```go
package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// ImpVMClassSpec defines compute resources for a class of VMs.
// All ImpVMs and ImpVMTemplates referencing this class inherit these values.
type ImpVMClassSpec struct {
	// VCPU is the number of virtual CPUs allocated to each VM.
	// +kubebuilder:validation:Minimum=1
	VCPU int32 `json:"vcpu"`

	// MemoryMiB is the amount of RAM in mebibytes.
	// +kubebuilder:validation:Minimum=128
	MemoryMiB int32 `json:"memoryMiB"`

	// DiskGiB is the root disk size in gibibytes.
	// +kubebuilder:validation:Minimum=1
	DiskGiB int32 `json:"diskGiB"`

	// Arch is the CPU architecture this class targets.
	// Use "multi" to allow scheduling on either amd64 or arm64 nodes.
	// +optional
	// +kubebuilder:default=multi
	Arch Arch `json:"arch,omitempty"`

	// Probes defines default probes for VMs using this class.
	// Can be overridden by ImpVMTemplate or ImpVM.
	// +optional
	Probes *ProbeSpec `json:"probes,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,shortName=impcls,categories=imp
// +kubebuilder:printcolumn:name="VCPU",type=integer,JSONPath=`.spec.vcpu`
// +kubebuilder:printcolumn:name="Memory",type=integer,JSONPath=`.spec.memoryMiB`
// +kubebuilder:printcolumn:name="Disk",type=integer,JSONPath=`.spec.diskGiB`
// +kubebuilder:printcolumn:name="Arch",type=string,JSONPath=`.spec.arch`

// ImpVMClass is the Schema for the impvmclasses API.
type ImpVMClass struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec ImpVMClassSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true

// ImpVMClassList contains a list of ImpVMClass.
type ImpVMClassList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ImpVMClass `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ImpVMClass{}, &ImpVMClassList{})
}
```

**Step 4: Run test to verify it passes**

```bash
go test ./api/v1alpha1/ -run TestImpVMClassSpec_RoundTrip -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add api/v1alpha1/impvmclass_types.go api/v1alpha1/impvmclass_types_test.go
git commit -m "api: define ImpVMClass type"
```

---

## Task 6: ImpVMTemplate type

**Files:**
- Create: `api/v1alpha1/impvmtemplate_types.go`
- Create: `api/v1alpha1/impvmtemplate_types_test.go`

**Step 1: Write the failing test**

Create `api/v1alpha1/impvmtemplate_types_test.go`:

```go
package v1alpha1

import (
	"encoding/json"
	"testing"
)

func TestImpVMTemplateSpec_RoundTrip(t *testing.T) {
	tmpl := ImpVMTemplateSpec{
		ClassRef:   ClusterObjectRef{Name: "small"},
		NetworkRef: &LocalObjectRef{Name: "sandbox-net"},
		Image:      "ghcr.io/myorg/rootfs:ubuntu-22.04",
		Probes: &ProbeSpec{
			ReadinessProbe: &Probe{
				HTTP: &HTTPGetAction{Path: "/ready", Port: 8080},
			},
		},
	}
	data, err := json.Marshal(tmpl)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got ImpVMTemplateSpec
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ClassRef.Name != "small" {
		t.Fatalf("ClassRef wrong: %v", got.ClassRef)
	}
	if got.Probes == nil || got.Probes.ReadinessProbe == nil || got.Probes.ReadinessProbe.HTTP == nil {
		t.Fatal("ReadinessProbe.HTTP lost")
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./api/v1alpha1/ -run TestImpVMTemplateSpec_RoundTrip -v
```

**Step 3: Write the type**

Create `api/v1alpha1/impvmtemplate_types.go`:

```go
package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// ImpVMTemplateSpec defines a reusable VM blueprint.
type ImpVMTemplateSpec struct {
	// ClassRef is the required compute class for VMs created from this template.
	ClassRef ClusterObjectRef `json:"classRef"`

	// NetworkRef optionally binds VMs to a specific ImpNetwork.
	// +optional
	NetworkRef *LocalObjectRef `json:"networkRef,omitempty"`

	// Image is the OCI image to use as the VM rootfs.
	// +optional
	Image string `json:"image,omitempty"`

	// Probes overrides probes inherited from the ImpVMClass.
	// +optional
	Probes *ProbeSpec `json:"probes,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,shortName=impvmt,categories=imp
// +kubebuilder:printcolumn:name="Class",type=string,JSONPath=`.spec.classRef.name`
// +kubebuilder:printcolumn:name="Network",type=string,JSONPath=`.spec.networkRef.name`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// ImpVMTemplate is the Schema for the impvmtemplates API.
type ImpVMTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec ImpVMTemplateSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true

// ImpVMTemplateList contains a list of ImpVMTemplate.
type ImpVMTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ImpVMTemplate `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ImpVMTemplate{}, &ImpVMTemplateList{})
}
```

**Step 4: Run test to verify it passes**

```bash
go test ./api/v1alpha1/ -run TestImpVMTemplateSpec_RoundTrip -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add api/v1alpha1/impvmtemplate_types.go api/v1alpha1/impvmtemplate_types_test.go
git commit -m "api: define ImpVMTemplate type"
```

---

## Task 7: ImpNetwork type

**Files:**
- Create: `api/v1alpha1/impnetwork_types.go`
- Create: `api/v1alpha1/impnetwork_types_test.go`

**Step 1: Write the failing test**

Create `api/v1alpha1/impnetwork_types_test.go`:

```go
package v1alpha1

import (
	"encoding/json"
	"testing"
)

func TestImpNetworkSpec_RoundTrip(t *testing.T) {
	net := ImpNetworkSpec{
		Subnet:  "192.168.100.0/24",
		Gateway: "192.168.100.1",
		NAT:     NATSpec{Enabled: true, EgressInterface: "eth0"},
		DNS:     []string{"1.1.1.1", "8.8.8.8"},
		Cilium:  &CiliumNetworkSpec{ExcludeFromIPAM: true, MasqueradeViaCilium: true},
	}
	data, err := json.Marshal(net)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got ImpNetworkSpec
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Subnet != "192.168.100.0/24" {
		t.Fatalf("Subnet wrong: %v", got.Subnet)
	}
	if !got.NAT.Enabled {
		t.Fatal("NAT.Enabled lost")
	}
	if len(got.DNS) != 2 {
		t.Fatalf("DNS wrong: %v", got.DNS)
	}
	if got.Cilium == nil || !got.Cilium.MasqueradeViaCilium {
		t.Fatal("Cilium config lost")
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./api/v1alpha1/ -run TestImpNetworkSpec_RoundTrip -v
```

**Step 3: Write the type**

Create `api/v1alpha1/impnetwork_types.go`:

```go
package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// ImpNetworkSpec defines a virtual network shared by one or more VMs.
type ImpNetworkSpec struct {
	// Subnet is the CIDR block for this network (e.g. "192.168.100.0/24").
	// +kubebuilder:validation:Pattern=`^([0-9]{1,3}\.){3}[0-9]{1,3}/[0-9]{1,2}$`
	Subnet string `json:"subnet"`

	// Gateway is the default gateway IP for VMs on this network.
	// Defaults to the first usable host address in Subnet if unset.
	// +optional
	Gateway string `json:"gateway,omitempty"`

	// NAT configures masquerade/SNAT for outbound VM traffic.
	// +optional
	NAT NATSpec `json:"nat,omitempty"`

	// DNS lists the nameservers injected into VMs on this network.
	// +optional
	DNS []string `json:"dns,omitempty"`

	// Cilium holds Cilium-specific integration settings.
	// Only relevant when the cluster CNI is Cilium.
	// +optional
	Cilium *CiliumNetworkSpec `json:"cilium,omitempty"`
}

// NATSpec configures outbound NAT for a network.
type NATSpec struct {
	// Enabled turns on MASQUERADE/SNAT for outbound traffic.
	Enabled bool `json:"enabled"`

	// EgressInterface is the host network interface used for NAT.
	// The node agent auto-detects the default route interface if unset.
	// +optional
	EgressInterface string `json:"egressInterface,omitempty"`
}

// CiliumNetworkSpec configures Cilium integration for this network.
type CiliumNetworkSpec struct {
	// ExcludeFromIPAM tells Cilium not to manage IP allocation for this subnet.
	// +optional
	ExcludeFromIPAM bool `json:"excludeFromIPAM,omitempty"`

	// MasqueradeViaCilium delegates outbound masquerade to Cilium's ipMasqAgent.
	// Requires Cilium ipMasqAgent to be configured with this subnet.
	// +optional
	MasqueradeViaCilium bool `json:"masqueradeViaCilium,omitempty"`
}

// ImpNetworkStatus reflects the observed state of an ImpNetwork.
type ImpNetworkStatus struct {
	// AllocatedIPs tracks IP addresses currently assigned to ImpVMs on this network.
	// +optional
	AllocatedIPs []string `json:"allocatedIPs,omitempty"`

	// Conditions follow the standard k8s condition convention.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=impnet,categories=imp
// +kubebuilder:printcolumn:name="Subnet",type=string,JSONPath=`.spec.subnet`
// +kubebuilder:printcolumn:name="NAT",type=boolean,JSONPath=`.spec.nat.enabled`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// ImpNetwork is the Schema for the impnetworks API.
type ImpNetwork struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ImpNetworkSpec   `json:"spec,omitempty"`
	Status ImpNetworkStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ImpNetworkList contains a list of ImpNetwork.
type ImpNetworkList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ImpNetwork `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ImpNetwork{}, &ImpNetworkList{})
}
```

**Step 4: Run test to verify it passes**

```bash
go test ./api/v1alpha1/ -run TestImpNetworkSpec_RoundTrip -v
```

**Step 5: Commit**

```bash
git add api/v1alpha1/impnetwork_types.go api/v1alpha1/impnetwork_types_test.go
git commit -m "api: define ImpNetwork type"
```

---

## Task 8: ClusterImpConfig type

Cluster-scoped singleton. Name must be `cluster` (enforced later via webhook; document the convention for now).

**Files:**
- Create: `api/v1alpha1/clusterimpconfig_types.go`
- Create: `api/v1alpha1/clusterimpconfig_types_test.go`

**Step 1: Write the failing test**

Create `api/v1alpha1/clusterimpconfig_types_test.go`:

```go
package v1alpha1

import (
	"encoding/json"
	"testing"
)

func TestClusterImpConfigSpec_Defaults(t *testing.T) {
	cfg := ClusterImpConfigSpec{}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got ClusterImpConfigSpec
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// zero value should round-trip cleanly
	if got.Capacity.DefaultFraction != 0 {
		t.Fatalf("unexpected default fraction: %v", got.Capacity.DefaultFraction)
	}
}

func TestClusterImpConfigSpec_CNIProvider(t *testing.T) {
	cfg := ClusterImpConfigSpec{
		Networking: NetworkingConfig{
			CNI: CNIConfig{
				AutoDetect: true,
				Provider:   "cilium-kubeproxy-free",
				NATBackend: "nftables",
			},
		},
	}
	data, _ := json.Marshal(cfg)
	var got ClusterImpConfigSpec
	json.Unmarshal(data, &got) //nolint:errcheck
	if got.Networking.CNI.Provider != "cilium-kubeproxy-free" {
		t.Fatalf("CNI provider lost: %v", got.Networking.CNI.Provider)
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./api/v1alpha1/ -run TestClusterImpConfig -v
```

**Step 3: Write the type**

Create `api/v1alpha1/clusterimpconfig_types.go`:

```go
package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// ClusterImpConfigSpec defines operator-wide settings.
// There must be exactly one ClusterImpConfig named "cluster" per cluster.
type ClusterImpConfigSpec struct {
	// Networking controls CNI detection and NAT backend selection.
	// +optional
	Networking NetworkingConfig `json:"networking,omitempty"`

	// Capacity sets default compute headroom across all nodes.
	// +optional
	Capacity CapacityConfig `json:"capacity,omitempty"`

	// Observability configures metrics and tracing.
	// +optional
	Observability ObservabilityConfig `json:"observability,omitempty"`
}

// NetworkingConfig holds cluster-wide networking settings.
type NetworkingConfig struct {
	// CNI controls how the operator detects and integrates with the cluster CNI.
	// +optional
	CNI CNIConfig `json:"cni,omitempty"`

	// IPAM controls IP address management for ImpNetworks.
	// +optional
	IPAM IPAMConfig `json:"ipam,omitempty"`
}

// CNIConfig configures CNI detection and NAT backend selection.
type CNIConfig struct {
	// AutoDetect enables automatic CNI detection at operator startup.
	// +optional
	// +kubebuilder:default=true
	AutoDetect bool `json:"autoDetect,omitempty"`

	// Provider explicitly sets the CNI, skipping auto-detection.
	// Valid values: cilium-kubeproxy-free, cilium, flannel, calico, unknown.
	// +optional
	Provider string `json:"provider,omitempty"`

	// NATBackend selects the NAT implementation used by the node agent.
	// +optional
	// +kubebuilder:validation:Enum=nftables;iptables
	NATBackend string `json:"natBackend,omitempty"`
}

// IPAMConfig controls IP address management.
type IPAMConfig struct {
	// Provider selects the IPAM backend.
	// +optional
	// +kubebuilder:default=internal
	// +kubebuilder:validation:Enum=internal
	Provider string `json:"provider,omitempty"`
}

// CapacityConfig controls how much node compute is reserved for ImpVMs.
type CapacityConfig struct {
	// DefaultFraction is the fraction of node allocatable CPU and memory
	// available to ImpVMs across all nodes. Range: 0.0–1.0.
	// +optional
	// +kubebuilder:default="0.9"
	// +kubebuilder:validation:Pattern=`^(0(\.[0-9]+)?|1(\.0+)?)$`
	DefaultFraction string `json:"defaultFraction,omitempty"`
}

// ObservabilityConfig enables metrics and tracing.
type ObservabilityConfig struct {
	// Metrics configures Prometheus metrics exposure.
	// +optional
	Metrics MetricsConfig `json:"metrics,omitempty"`

	// Tracing configures OpenTelemetry trace export (opt-in).
	// +optional
	Tracing TracingConfig `json:"tracing,omitempty"`
}

// MetricsConfig controls Prometheus metrics.
type MetricsConfig struct {
	// Enabled turns on the /metrics endpoint.
	// +optional
	// +kubebuilder:default=true
	Enabled bool `json:"enabled,omitempty"`

	// Port is the port the /metrics endpoint listens on.
	// +optional
	// +kubebuilder:default=9090
	Port int32 `json:"port,omitempty"`
}

// TracingConfig controls OpenTelemetry tracing.
type TracingConfig struct {
	// Enabled turns on trace export. Off by default.
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// Endpoint is the OTLP gRPC endpoint (e.g. "http://otel-collector:4317").
	// +optional
	Endpoint string `json:"endpoint,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,shortName=impcfg,categories=imp
// +kubebuilder:printcolumn:name="CNI",type=string,JSONPath=`.spec.networking.cni.provider`
// +kubebuilder:printcolumn:name="DefaultFraction",type=string,JSONPath=`.spec.capacity.defaultFraction`

// ClusterImpConfig is the Schema for the clusterimpconfigs API.
// Create exactly one instance named "cluster".
type ClusterImpConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec ClusterImpConfigSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true

// ClusterImpConfigList contains a list of ClusterImpConfig.
type ClusterImpConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterImpConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClusterImpConfig{}, &ClusterImpConfigList{})
}
```

**Step 4: Run test to verify it passes**

```bash
go test ./api/v1alpha1/ -run TestClusterImpConfig -v
```

**Step 5: Commit**

```bash
git add api/v1alpha1/clusterimpconfig_types.go api/v1alpha1/clusterimpconfig_types_test.go
git commit -m "api: define ClusterImpConfig type"
```

---

## Task 9: ClusterImpNodeProfile type

**Files:**
- Create: `api/v1alpha1/clusterimpnodeprofile_types.go`
- Create: `api/v1alpha1/clusterimpnodeprofile_types_test.go`

**Step 1: Write the failing test**

Create `api/v1alpha1/clusterimpnodeprofile_types_test.go`:

```go
package v1alpha1

import (
	"encoding/json"
	"testing"
)

func TestClusterImpNodeProfileSpec_RoundTrip(t *testing.T) {
	np := ClusterImpNodeProfileSpec{
		CapacityFraction: "0.85",
		MaxImpVMs:        10,
	}
	data, err := json.Marshal(np)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got ClusterImpNodeProfileSpec
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.CapacityFraction != "0.85" {
		t.Fatalf("CapacityFraction wrong: %v", got.CapacityFraction)
	}
	if got.MaxImpVMs != 10 {
		t.Fatalf("MaxImpVMs wrong: %v", got.MaxImpVMs)
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./api/v1alpha1/ -run TestClusterImpNodeProfileSpec_RoundTrip -v
```

**Step 3: Write the type**

Create `api/v1alpha1/clusterimpnodeprofile_types.go`:

```go
package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// ClusterImpNodeProfileSpec defines per-node capacity overrides.
// The resource name must match the Kubernetes node name.
// If no profile exists for a node, ClusterImpConfig.spec.capacity.defaultFraction applies.
type ClusterImpNodeProfileSpec struct {
	// CapacityFraction overrides the cluster-default fraction for this node.
	// Range: 0.0–1.0.
	// +optional
	// +kubebuilder:validation:Pattern=`^(0(\.[0-9]+)?|1(\.0+)?)$`
	CapacityFraction string `json:"capacityFraction,omitempty"`

	// MaxImpVMs is a hard cap on the number of ImpVMs allowed on this node,
	// regardless of remaining compute headroom.
	// +optional
	// +kubebuilder:validation:Minimum=0
	MaxImpVMs int32 `json:"maxImpVMs,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,shortName=impnp,categories=imp
// +kubebuilder:printcolumn:name="Fraction",type=string,JSONPath=`.spec.capacityFraction`
// +kubebuilder:printcolumn:name="MaxVMs",type=integer,JSONPath=`.spec.maxImpVMs`

// ClusterImpNodeProfile is the Schema for the clusterimpnodeprofiles API.
// Name must match the target Kubernetes node name.
type ClusterImpNodeProfile struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec ClusterImpNodeProfileSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true

// ClusterImpNodeProfileList contains a list of ClusterImpNodeProfile.
type ClusterImpNodeProfileList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterImpNodeProfile `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClusterImpNodeProfile{}, &ClusterImpNodeProfileList{})
}
```

**Step 4: Run test to verify it passes**

```bash
go test ./api/v1alpha1/ -run TestClusterImpNodeProfileSpec_RoundTrip -v
```

**Step 5: Commit**

```bash
git add api/v1alpha1/clusterimpnodeprofile_types.go api/v1alpha1/clusterimpnodeprofile_types_test.go
git commit -m "api: define ClusterImpNodeProfile type"
```

---

## Task 10: Scheme registration test

Verify all 6 types register correctly with the k8s scheme. This is the integration test for the whole API layer.

**Files:**
- Create: `api/v1alpha1/scheme_test.go`

**Step 1: Write the test**

Create `api/v1alpha1/scheme_test.go`:

```go
package v1alpha1_test

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	impv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

func TestAllTypesRegisterWithScheme(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := impv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}

	gv := schema.GroupVersion{Group: "imp.dev", Version: "v1alpha1"}

	kinds := []string{
		"ImpVM", "ImpVMList",
		"ImpVMClass", "ImpVMClassList",
		"ImpVMTemplate", "ImpVMTemplateList",
		"ImpNetwork", "ImpNetworkList",
		"ClusterImpConfig", "ClusterImpConfigList",
		"ClusterImpNodeProfile", "ClusterImpNodeProfileList",
	}

	for _, kind := range kinds {
		gvk := gv.WithKind(kind)
		if !scheme.Recognizes(gvk) {
			t.Errorf("scheme does not recognise %s", gvk)
		}
	}
}
```

**Step 2: Run test**

```bash
go test ./api/v1alpha1/ -run TestAllTypesRegisterWithScheme -v
```

Expected: PASS — all 12 GVKs recognised (6 kinds + 6 list kinds).
If any fail, ensure the missing type's `init()` calls `SchemeBuilder.Register`.

**Step 3: Run all API tests together**

```bash
go test ./api/v1alpha1/... -v
```

Expected: all PASS.

**Step 4: Commit**

```bash
git add api/v1alpha1/scheme_test.go
git commit -m "api: add scheme registration test for all 6 CRD types"
```

---

## Task 11: Run codegen

Generate the `zz_generated.deepcopy.go` file and all CRD YAML manifests.

**Files:**
- Create: `api/v1alpha1/zz_generated.deepcopy.go` (generated, do not edit)
- Create: `config/crd/bases/*.yaml` (generated, do not edit)

**Step 1: Generate DeepCopy methods**

```bash
controller-gen object:headerFile="hack/boilerplate.go.txt" paths="./..."
```

Expected: creates/updates `api/v1alpha1/zz_generated.deepcopy.go`.

**Step 2: Verify it compiles**

```bash
go build ./...
```

**Step 3: Generate CRD manifests**

```bash
controller-gen rbac:roleName=imp-operator-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases
```

Expected: creates YAML files under `config/crd/bases/`, one per CRD.

**Step 4: Verify 6 CRD files exist**

```bash
ls config/crd/bases/
```

Expected files:
- `imp.dev_impvms.yaml`
- `imp.dev_impvmclasses.yaml`
- `imp.dev_impvmtemplates.yaml`
- `imp.dev_impnetworks.yaml`
- `imp.dev_clusterimpconfigs.yaml`
- `imp.dev_clusterimpnodeprofiles.yaml`

**Step 5: Run all tests**

```bash
go test ./... -v
```

**Step 6: Commit**

```bash
git add api/v1alpha1/zz_generated.deepcopy.go config/crd/bases/
git commit -m "codegen: generate DeepCopy methods and CRD manifests"
```

---

## Task 12: Update Makefile

The kubebuilder-generated Makefile references `cmd/main.go`. We need two binaries. Update the Makefile targets.

**Files:**
- Modify: `Makefile`

**Step 1: Open the generated Makefile**

Look for the `build` target. It will look something like:

```makefile
.PHONY: build
build: manifests generate fmt vet ## Build manager binary.
	go build -o bin/manager ./cmd/main.go
```

**Step 2: Replace the build target**

Find and replace the `build` target and add per-binary targets:

```makefile
.PHONY: build
build: build-operator build-agent ## Build both operator and agent binaries.

.PHONY: build-operator
build-operator: manifests generate fmt vet ## Build the operator binary.
	go build -o bin/operator ./cmd/operator

.PHONY: build-agent
build-agent: manifests generate fmt vet ## Build the node agent binary.
	go build -o bin/agent ./cmd/agent
```

Also find the `run` target and update it (or remove it — we'll use a proper cluster for testing):

```makefile
.PHONY: run
run: manifests generate fmt vet ## Run operator from your host (for development).
	go run ./cmd/operator
```

**Step 3: Verify make build compiles**

```bash
mkdir -p bin
make build 2>&1 | head -20
```

Expected: compiles (will fail on missing `cmd/operator` and `cmd/agent` — that's fine, those come in the next task).

**Step 4: Commit**

```bash
git add Makefile
git commit -m "build: update Makefile for dual operator/agent binaries"
```

---

## Task 13: Operator main.go

**Files:**
- Create: `cmd/operator/main.go`
- Delete or empty: `cmd/main.go` (kubebuilder's default)

**Step 1: Create cmd/operator/main.go**

```go
package main

import (
	"flag"
	"os"

	// Import all Kubernetes client auth plugins (e.g. OIDC).
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	impv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
	"github.com/syscode-labs/imp/internal/operator/controller"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(impv1alpha1.AddToScheme(scheme))
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metrics endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false, "Enable leader election for controller manager.")

	opts := zap.Options{Development: true}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: metricsAddr,
		},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "imp.dev",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err = (&controller.ImpVMReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ImpVM")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
```

**Step 2: Verify the controller stub path**

The kubebuilder-generated controller lives at `internal/controller/impvm_controller.go`.
Check the package name and struct name match what main.go imports:

```bash
head -20 internal/controller/impvm_controller.go
```

Ensure it has `ImpVMReconciler` with `Client` and `Scheme` fields and a `SetupWithManager` method. If the package path is different (e.g. `internal/controller/` vs something else), adjust the import in main.go.

**Step 3: Move the kubebuilder default cmd/main.go out of the way**

```bash
# The kubebuilder default entrypoint is no longer needed
# (keep it for now to avoid breaking any generated references, but clear it)
rm cmd/main.go
```

If `cmd/main.go` is referenced anywhere in the Makefile or PROJECT file, update those references to `cmd/operator`.

**Step 4: Build the operator**

```bash
go build ./cmd/operator/...
```

Expected: produces no errors (no binary written, just compilation check).

**Step 5: Commit**

```bash
git add cmd/operator/main.go
git rm cmd/main.go 2>/dev/null || true
git commit -m "cmd: add operator entrypoint"
```

---

## Task 14: Agent main.go

The node agent runs as a DaemonSet. It does not use controller-runtime's manager — it runs its own reconcile loop watching ImpVM objects where `spec.nodeName == os.Hostname()`.

**Files:**
- Create: `cmd/agent/main.go`
- Create: `internal/agent/agent.go` (stub)

**Step 1: Create the agent stub**

Create `internal/agent/agent.go`:

```go
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
```

**Step 2: Create cmd/agent/main.go**

```go
package main

import (
	"context"
	"flag"
	"os"

	_ "k8s.io/client-go/plugin/pkg/client/auth"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/syscode-labs/imp/internal/agent"
)

func main() {
	opts := zap.Options{Development: true}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	log := ctrl.Log.WithName("main")

	nodeName := os.Getenv("NODE_NAME")
	if nodeName == "" {
		log.Error(nil, "NODE_NAME env var not set — run as DaemonSet with fieldRef downward API")
		os.Exit(1)
	}

	a := &agent.Agent{NodeName: nodeName}

	ctx := ctrl.SetupSignalHandler()
	if err := a.Run(ctx); err != nil && err != context.Canceled {
		log.Error(err, "agent exited with error")
		os.Exit(1)
	}
}
```

**Step 3: Build the agent**

```bash
go build ./cmd/agent/...
```

Expected: compiles without errors.

**Step 4: Build both**

```bash
make build
```

Expected: both `bin/operator` and `bin/agent` produced.

**Step 5: Commit**

```bash
git add cmd/agent/main.go internal/agent/agent.go
git commit -m "cmd: add node agent entrypoint and stub"
```

---

## Task 15: Dockerfiles

The CI release workflow already references `Dockerfile.operator` and `Dockerfile.agent` at the repo root.

**Files:**
- Create: `Dockerfile.operator`
- Create: `Dockerfile.agent`

**Step 1: Create Dockerfile.operator**

```dockerfile
# syntax=docker/dockerfile:1
FROM golang:1.23-alpine AS builder
WORKDIR /workspace

COPY go.mod go.sum ./
RUN go mod download

COPY api/ api/
COPY cmd/operator/ cmd/operator/
COPY internal/ internal/

RUN CGO_ENABLED=0 GOOS=linux go build -a -o operator ./cmd/operator

FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/operator .
USER 65532:65532
ENTRYPOINT ["/operator"]
```

**Step 2: Create Dockerfile.agent**

```dockerfile
# syntax=docker/dockerfile:1
FROM golang:1.23-alpine AS builder
WORKDIR /workspace

COPY go.mod go.sum ./
RUN go mod download

COPY api/ api/
COPY cmd/agent/ cmd/agent/
COPY internal/ internal/

RUN CGO_ENABLED=0 GOOS=linux go build -a -o agent ./cmd/agent

FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/agent .
USER 65532:65532
ENTRYPOINT ["/agent"]
```

**Step 3: Lint Dockerfiles**

```bash
hadolint Dockerfile.operator Dockerfile.agent
```

Expected: no errors. If `hadolint` isn't installed locally: `brew install hadolint`.

**Step 4: Commit**

```bash
git add Dockerfile.operator Dockerfile.agent
git commit -m "docker: add operator and agent Dockerfiles"
```

---

## Task 16: Final verification

**Step 1: Run all tests with race detector**

```bash
go test -race ./...
```

Expected: all PASS, no race conditions detected.

**Step 2: Run linter**

```bash
golangci-lint run ./...
```

Expected: no errors. Fix any issues before committing.

**Step 3: Build everything**

```bash
make build
```

Expected: `bin/operator` and `bin/agent` produced without errors.

**Step 4: Verify CRD manifests are current**

```bash
make manifests
git diff --exit-code config/crd/bases/
```

Expected: no diff — generated files match what's committed.

**Step 5: Commit and push**

```bash
git push origin main
```

---

## Notes

- **Go version mismatch:** local Go is 1.23.7; CI uses 1.24. Both are fine — go.mod will declare `go 1.23`. Update CI's `GO_VERSION` to `"1.23"` for consistency if preferred.
- **kubebuilder group resolution:** if `groupversion_info.go` shows a double-dot or wrong group after `kubebuilder create api`, manually correct `GroupVersion.Group` to `"imp.dev"`.
- **Agent uses controller-runtime client, not manager:** the agent will use `ctrl.GetConfigOrDie()` + a direct client to watch ImpVM objects. The manager pattern (leader election, webhooks) is for the operator only.
- **No webhooks yet:** defaulting and validation webhooks are a later task. The kubebuilder markers include validation constraints that controller-gen will embed in the CRD `spec.validation` — those are enforced by the API server without webhooks.
