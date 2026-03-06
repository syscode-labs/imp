package v1alpha1

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImpVMRunnerPool_roundTrip(t *testing.T) {
	pool := ImpVMRunnerPool{}
	pool.Name, pool.Namespace = "ci-linux", "ci"
	pool.Spec = ImpVMRunnerPoolSpec{
		TemplateName: "ubuntu-runner",
		Platform: RunnerPlatformSpec{
			Type:              "github-actions",
			CredentialsSecret: "gh-creds",
			Scope:             &RunnerScopeSpec{Org: "my-org"},
		},
		RunnerLayer: "ghcr.io/syscode-labs/imp-runners/github-actions:v2.317",
		Labels:      []string{"self-hosted", "linux"},
		Scaling:     &RunnerScalingSpec{MinIdle: 0, MaxConcurrent: 10},
		JobDetection: &RunnerJobDetectionSpec{
			Webhook: &RunnerWebhookSpec{Enabled: true, SecretRef: "gh-webhook"},
			Polling: &RunnerPollingSpec{Enabled: true, IntervalSeconds: 30},
		},
	}

	b, err := json.Marshal(pool)
	require.NoError(t, err)
	var out ImpVMRunnerPool
	require.NoError(t, json.Unmarshal(b, &out))
	assert.Equal(t, "ubuntu-runner", out.Spec.TemplateName)
	assert.Equal(t, "github-actions", out.Spec.Platform.Type)
	assert.Equal(t, "my-org", out.Spec.Platform.Scope.Org)
	assert.Equal(t, int32(10), out.Spec.Scaling.MaxConcurrent)
}
