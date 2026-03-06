package v1alpha1

// Constants for the Cilium CRDs that Imp interacts with.
// CiliumExternalWorkload is a cluster-scoped Cilium CRD; we interact with it
// via unstructured objects to avoid a hard dependency on the Cilium API packages.
const (
	CiliumGroup      = "cilium.io"
	CiliumVersion    = "v2"
	CiliumEWResource = "ciliumexternalworkloads"
)
