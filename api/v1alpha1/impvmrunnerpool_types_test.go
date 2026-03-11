package v1alpha1

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
		ExpireAfter: &metav1.Duration{Duration: 4 * time.Hour},
	}

	b, err := json.Marshal(pool)
	require.NoError(t, err)
	var out ImpVMRunnerPool
	require.NoError(t, json.Unmarshal(b, &out))
	assert.Equal(t, "ubuntu-runner", out.Spec.TemplateName)
	assert.Equal(t, "github-actions", out.Spec.Platform.Type)
	assert.Equal(t, "my-org", out.Spec.Platform.Scope.Org)
	assert.Equal(t, int32(10), out.Spec.Scaling.MaxConcurrent)
	require.NotNil(t, out.Spec.ExpireAfter)
	assert.Equal(t, 4*time.Hour, out.Spec.ExpireAfter.Duration)
}

func TestImpVMRunnerPoolCRD_scopeValidationRequiresExactlyOne(t *testing.T) {
	b, err := os.ReadFile("../../config/crd/bases/imp.dev_impvmrunnerpools.yaml")
	require.NoError(t, err)

	yaml := string(b)
	assert.Contains(t, yaml, "set exactly one of org or repo")
	assert.Contains(t, yaml, "(size(self.org) > 0) != (size(self.repo) > 0)")
	assert.False(t, strings.Contains(yaml, "!(size(self.org) > 0 && size(self.repo) > 0)"),
		"scope validation should require at least one of org or repo, not only mutual exclusion")
}
