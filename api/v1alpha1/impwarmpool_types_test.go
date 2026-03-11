package v1alpha1

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestImpWarmPool_roundTrip(t *testing.T) {
	pool := ImpWarmPool{
		ObjectMeta: metav1.ObjectMeta{Name: "wp1", Namespace: "default"},
		Spec: ImpWarmPoolSpec{
			SnapshotRef:  "my-snapshot",
			Size:         3,
			TemplateName: "ci-runner",
			ExpireAfter:  &metav1.Duration{Duration: 90 * time.Minute},
		},
	}
	b, err := json.Marshal(pool)
	require.NoError(t, err)
	var out ImpWarmPool
	require.NoError(t, json.Unmarshal(b, &out))
	assert.Equal(t, "my-snapshot", out.Spec.SnapshotRef)
	assert.Equal(t, int32(3), out.Spec.Size)
	assert.Equal(t, "ci-runner", out.Spec.TemplateName)
	require.NotNil(t, out.Spec.ExpireAfter)
	assert.Equal(t, 90*time.Minute, out.Spec.ExpireAfter.Duration)
}
