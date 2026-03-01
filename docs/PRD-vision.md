# Imp — Vision & Value Proposition

> *microVMs that do your bidding*

---

## The Problem

Containers give you fast, lightweight workloads — but they share the host kernel. Every container on a node trusts every other container to not break out. For most workloads that's fine. For CI runners executing untrusted code, sandboxed environments, or anything handling sensitive data, the shared kernel is a fundamental security boundary that simply isn't there.

VMs solve the isolation problem, but introduce their own: they're slow (minutes to boot), fat (gigabytes of RAM overhead for the hypervisor), and operationally painful (libvirt XML, VMware vSphere, cloud provider APIs). You can't `kubectl apply` a VM the way you'd apply a Pod.

There's no good answer for teams that want **real isolation at container speed**.

---

## The Solution

**Imp** is a Kubernetes operator for [Firecracker](https://firecracker-microvm.github.io/) microVMs.

Declare a real isolated Linux environment the same way you'd declare a Pod — it spawns in under a second, does its job, and disappears.

```yaml
apiVersion: imp.dev/v1alpha1
kind: ImpVM
metadata:
  name: my-sandbox
spec:
  classRef:
    name: small          # 1 vCPU, 512 MiB RAM, 10 GiB disk
  image: ghcr.io/myorg/runner:latest
  lifecycle: ephemeral   # gone when the workload exits
  env:
    - name: JOB_ID
      value: "abc123"
```

That's it. The operator schedules it to a node, the node agent boots a Firecracker VM from the OCI image, and the status flows back via standard Kubernetes conditions.

---

## Why Firecracker

Firecracker is the VMM AWS built for Lambda and Fargate. It's open source (Apache 2.0), production-proven at massive scale, and uniquely minimal:

- **~125ms boot time** for the VMM itself — no BIOS, no legacy devices
- **<5 MB RAM overhead** per VM (vs. hundreds of MB for QEMU)
- **KVM-native** — runs directly on the host kernel's KVM subsystem
- **Minimal attack surface** — ~50k lines of Rust, no USB, no video, no sound
- **Multi-arch** — amd64 and arm64, same binary interface

The gap Imp fills: Firecracker has no Kubernetes-native management layer. You get a REST API to a single VMM process. Imp wraps that in the full Kubernetes operator pattern — declarative config, reconciliation loops, status conditions, events, RBAC.

---

## Target Environments

Imp is designed for **self-hosted Kubernetes on bare metal**, specifically:

| Environment | Hardware | Use |
|-------------|----------|-----|
| Homelab | Lenovo M720q (amd64) | Development, experimentation |
| Cloud free tier | OCI Ampere A1 (arm64) | CI, lightweight production |

**Kubernetes distribution:** Talos Linux — immutable, API-driven, no SSH.
**CNI:** Cilium in kube-proxy-free mode (other CNIs supported with degraded integration).

Imp is not designed for managed Kubernetes (EKS, GKE, AKS) — those environments don't expose `/dev/kvm`.

---

## Who It's For

**Primary:** Homelab operators and platform engineers who run self-hosted Kubernetes and want strong workload isolation without the overhead of traditional VMs.

**Use cases:**
- **CI runners** — ephemeral, isolated environments per job; no cross-job contamination
- **Sandboxed execution** — untrusted code, student submissions, fuzzing
- **Development environments** — reproducible, disposable Linux VMs from OCI images
- **Secure multi-tenancy** — multiple teams sharing a cluster with VM-level isolation

---

## Value Proposition

| | Containers | Traditional VMs | Imp microVMs |
|---|---|---|---|
| Boot time | ~100ms | 30–120s | ~200–300ms (warm) |
| RAM overhead | ~1 MB | 200 MB+ | <5 MB |
| Kernel isolation | No | Yes | Yes |
| K8s-native UX | Yes | No | Yes |
| OCI images | Yes | No | Yes |
| Multi-arch | Yes | Varies | Yes (amd64 + arm64) |

---

## Phased Ambition

**Phase 1 (current):** Ephemeral VMs — sandboxes and CI runners. Boot fast, run a job, disappear.

**Phase 2:** Persistent VMs with migration via Firecracker snapshot/restore. Warm VM pools for near-instant assignment.

**Phase 3:** CI runner mode with auto-registration, cross-node VM networking, Cilium NetworkPolicy on VMs (Hubble visibility).

---

## Name

An *imp* is a small, mischievous creature that does its master's bidding — apt for disposable microVMs that spawn, run a task, and vanish.
