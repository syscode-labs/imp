package v1alpha1

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestImpVMSnapshot_roundTrip(t *testing.T) {
	snap := ImpVMSnapshot{
		ObjectMeta: metav1.ObjectMeta{Name: "snap1", Namespace: "default"},
		Spec: ImpVMSnapshotSpec{
			SourceVMName:      "my-vm",
			SourceVMNamespace: "default",
			Schedule:          "0 2 * * *",
			Retention:         5,
			Storage: SnapshotStorageSpec{
				Type: "oci-registry",
				OCIRegistry: &OCIRegistrySpec{
					Repository:    "ghcr.io/org/imp-snapshots",
					PullSecretRef: &corev1.LocalObjectReference{Name: "registry-secret"},
				},
			},
		},
	}
	b, err := json.Marshal(snap)
	require.NoError(t, err)
	var out ImpVMSnapshot
	require.NoError(t, json.Unmarshal(b, &out))
	assert.Equal(t, "0 2 * * *", out.Spec.Schedule)
	assert.Equal(t, int32(5), out.Spec.Retention)
	assert.Equal(t, "ghcr.io/org/imp-snapshots", out.Spec.Storage.OCIRegistry.Repository)
	assert.Equal(t, "registry-secret", out.Spec.Storage.OCIRegistry.PullSecretRef.Name)
}

func TestImpVMSnapshot_nodeLocal(t *testing.T) {
	snap := ImpVMSnapshot{
		Spec: ImpVMSnapshotSpec{
			SourceVMName:      "my-vm",
			SourceVMNamespace: "default",
			Storage:           SnapshotStorageSpec{Type: "node-local"},
		},
	}
	b, err := json.Marshal(snap)
	require.NoError(t, err)
	var out ImpVMSnapshot
	require.NoError(t, json.Unmarshal(b, &out))
	assert.Equal(t, "node-local", out.Spec.Storage.Type)
	assert.Nil(t, out.Spec.Storage.OCIRegistry)
}
