# go-scaffold
// TODO(user): Add simple overview of use/purpose

## Description
// TODO(user): An in-depth paragraph about your project and overview of use

## Getting Started

### Prerequisites
- go version v1.24.6+
- docker version 17.03+.
- kubectl version v1.11.3+.
- Access to a Kubernetes v1.11.3+ cluster.

### OCI Golden Image + Firecracker E2E

The repository includes two standalone IaC scripts:

- `hack/oci-build-golden-image.sh`
- `hack/oci-firecracker-e2e.sh`
- `hack/packer-build-golden-image.sh` (Packer wrapper around `oci-build-golden-image.sh`)

`hack/oci-build-golden-image.sh` is idempotent for missing OCI inputs:

- auto-detects compartment and AD for `VM.Standard.E2.1.Micro` from limits
- reuses an existing public subnet, or creates a minimal public VCN/subnet stack
- prunes oldest `imp-fc-golden-*` images if custom image quota is full

Build a minimal golden image:

```sh
IMP_OCI_PROFILE=syscode-api \
IMP_OCI_COMPARTMENT_NAME=homelab \
IMP_OCI_DOMAIN_NAME=homelab \
OCI_SSH_PUBLIC_KEY_FILE="$HOME/.ssh/builder.pub" \
OCI_SSH_PRIVATE_KEY_FILE="$HOME/.ssh/builder" \
OCI_OUTPUT_ENV_FILE="$HOME/.config/imp/oci-golden.env" \
hack/oci-build-golden-image.sh
```

Build the same golden image through Packer while reusing the script checks:

```sh
IMP_OCI_PROFILE=syscode-api \
IMP_OCI_COMPARTMENT_NAME=homelab \
IMP_OCI_DOMAIN_NAME=homelab \
OCI_SSH_PUBLIC_KEY_FILE="$HOME/.ssh/builder.pub" \
OCI_SSH_PRIVATE_KEY_FILE="$HOME/.ssh/builder" \
OCI_OUTPUT_ENV_FILE="$HOME/.config/imp/oci-golden.env" \
hack/packer-build-golden-image.sh
```

Run e2e using the generated image:

```sh
source "$HOME/.config/imp/oci-golden.env"
IMP_OCI_PROFILE=syscode-api \
IMP_OCI_COMPARTMENT_NAME=homelab \
IMP_OCI_DOMAIN_NAME=homelab \
OCI_SSH_PUBLIC_KEY_FILE="$HOME/.ssh/builder.pub" \
OCI_SSH_PRIVATE_KEY_FILE="$HOME/.ssh/builder" \
OCI_IMAGE_OCID="$OCI_IMAGE_OCID" \
hack/oci-firecracker-e2e.sh
```

Or run e2e and let it build a golden image automatically when `OCI_IMAGE_OCID` is unset:

```sh
IMP_OCI_PROFILE=syscode-api \
IMP_OCI_COMPARTMENT_NAME=homelab \
IMP_OCI_DOMAIN_NAME=homelab \
OCI_SSH_PUBLIC_KEY_FILE="$HOME/.ssh/builder.pub" \
OCI_SSH_PRIVATE_KEY_FILE="$HOME/.ssh/builder" \
hack/oci-firecracker-e2e.sh
```

Notes:

- OCI requires boot volume size `>= 50` GB.
- Golden image max size is controlled by `OCI_GOLDEN_MAX_GB` (default `50` GiB), and the script will fail/delete oversize images by default.
- Optional: set `OCI_GOLDEN_ZERO_FILL=true` to zero free space before capture (slower, may reduce resulting image size).
- If your SSH key is passphrase-protected, use an unencrypted key for automation or set `ALLOW_SSH_AGENT=true` with a loaded agent.
- Targeting defaults can be set once with:
  - `IMP_OCI_PROFILE` (recommended `syscode-api`)
  - `IMP_OCI_COMPARTMENT_NAME` (recommended `homelab`)
  - `IMP_OCI_DOMAIN_NAME` (recommended `homelab`)

### To Deploy on the cluster
**Build and push your image to the location specified by `IMG`:**

```sh
make docker-build docker-push IMG=<some-registry>/go-scaffold:tag
```

**NOTE:** This image ought to be published in the personal registry you specified.
And it is required to have access to pull the image from the working environment.
Make sure you have the proper permission to the registry if the above commands don’t work.

**Install the CRDs into the cluster:**

```sh
make install
```

**Deploy the Manager to the cluster with the image specified by `IMG`:**

```sh
make deploy IMG=<some-registry>/go-scaffold:tag
```

> **NOTE**: If you encounter RBAC errors, you may need to grant yourself cluster-admin
privileges or be logged in as admin.

**Create instances of your solution**
You can apply the samples (examples) from the config/sample:

```sh
kubectl apply -k config/samples/
```

>**NOTE**: Ensure that the samples has default values to test it out.

### To Uninstall
**Delete the instances (CRs) from the cluster:**

```sh
kubectl delete -k config/samples/
```

**Delete the APIs(CRDs) from the cluster:**

```sh
make uninstall
```

**UnDeploy the controller from the cluster:**

```sh
make undeploy
```

## Project Distribution

Following the options to release and provide this solution to the users.

### By providing a bundle with all YAML files

1. Build the installer for the image built and published in the registry:

```sh
make build-installer IMG=<some-registry>/go-scaffold:tag
```

**NOTE:** The makefile target mentioned above generates an 'install.yaml'
file in the dist directory. This file contains all the resources built
with Kustomize, which are necessary to install this project without its
dependencies.

2. Using the installer

Users can just run 'kubectl apply -f <URL for YAML BUNDLE>' to install
the project, i.e.:

```sh
kubectl apply -f https://raw.githubusercontent.com/<org>/go-scaffold/<tag or branch>/dist/install.yaml
```

### By providing a Helm Chart

1. Build the chart using the optional helm plugin

```sh
kubebuilder edit --plugins=helm/v2-alpha
```

2. See that a chart was generated under 'dist/chart', and users
can obtain this solution from there.

**NOTE:** If you change the project, you need to update the Helm Chart
using the same command above to sync the latest changes. Furthermore,
if you create webhooks, you need to use the above command with
the '--force' flag and manually ensure that any custom configuration
previously added to 'dist/chart/values.yaml' or 'dist/chart/manager/manager.yaml'
is manually re-applied afterwards.

## CNI Support

Imp provides first-class integration with **Cilium**. When Cilium is detected:

- VMs are enrolled as `CiliumExternalWorkload` objects and become full Cilium mesh participants
- `NetworkPolicy` rules apply to VMs identically to pods
- VM traffic is visible in Hubble (`hubble observe`)
- VMs can reach `ClusterIP` services via kube-dns

Other CNIs (Flannel, Calico, Weave, etc.) work for basic node-local networking. Cross-node VM connectivity uses an automatic VXLAN overlay managed by Imp, without Cilium NetworkPolicy or Hubble visibility.

> **Support policy:** Cilium is the only officially supported CNI for cross-node networking and NetworkPolicy enforcement. Other CNIs receive best-effort support via the VXLAN fallback. External contributions adding CNI-specific integrations are welcome and will be reviewed.

## Contributing
// TODO(user): Add detailed information on how you would like others to contribute to this project

**NOTE:** Run `make help` for more information on all potential `make` targets

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

## License

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
