package v1alpha1

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestImpVMMigration_roundTrip(t *testing.T) {
	m := ImpVMMigration{
		ObjectMeta: metav1.ObjectMeta{Name: "mig1", Namespace: "default"},
		Spec: ImpVMMigrationSpec{
			SourceVMName:      "my-vm",
			SourceVMNamespace: "default",
			TargetNode:        "node-2",
		},
	}
	b, err := json.Marshal(m)
	require.NoError(t, err)
	var out ImpVMMigration
	require.NoError(t, json.Unmarshal(b, &out))
	assert.Equal(t, "my-vm", out.Spec.SourceVMName)
	assert.Equal(t, "node-2", out.Spec.TargetNode)
}

func TestImpVMMigration_noTargetNode(t *testing.T) {
	m := ImpVMMigration{
		Spec: ImpVMMigrationSpec{
			SourceVMName:      "my-vm",
			SourceVMNamespace: "default",
		},
	}
	b, err := json.Marshal(m)
	require.NoError(t, err)
	var out ImpVMMigration
	require.NoError(t, json.Unmarshal(b, &out))
	assert.Empty(t, out.Spec.TargetNode)
}

func TestClusterImpNodeProfile_cpuModel(t *testing.T) {
	p := ClusterImpNodeProfile{
		Spec: ClusterImpNodeProfileSpec{
			CPUModel: "Intel(R) Core(TM) i5-8500T CPU @ 2.10GHz",
		},
	}
	b, err := json.Marshal(p)
	require.NoError(t, err)
	var out ClusterImpNodeProfile
	require.NoError(t, json.Unmarshal(b, &out))
	assert.Equal(t, "Intel(R) Core(TM) i5-8500T CPU @ 2.10GHz", out.Spec.CPUModel)
}
