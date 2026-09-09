package runner

import (
	"context"
	"fmt"
)

const (
	JITConfigSecretKey         = "jitconfig"
	JITConfigVersionAnnotation = "imp.dev/runner-config-version"
	JITConfigRunnerAnnotation  = "imp.dev/runner-name"
	JITConfigVersionV1         = "v1"
)

// JITConfig is a one-time runner registration token issued by the platform.
// The runner binary uses this to register itself and pick up exactly one job.
// JITResponseError indicates a successful API response that cannot produce a usable runner.
type JITResponseError struct {
	Reason string
}

func (e *JITResponseError) Error() string { return "runner JIT response rejected: " + e.Reason }

type JITConfig struct {
	// EncodedConfig is passed directly to the runner binary at startup.
	EncodedConfig string
	// RunnerName is the name assigned by the platform.
	RunnerName string
}

// MarshalSecretValue returns the version-1 Secret payload. The raw encoding is
// retained so old agents can consume Secrets minted by a new operator during a
// rolling upgrade.
func (c JITConfig) MarshalSecretValue() ([]byte, error) {
	if c.EncodedConfig == "" {
		return nil, fmt.Errorf("JIT config is empty")
	}
	return []byte(c.EncodedConfig), nil
}

// JITConfigFromSecretValue decodes the typed JIT contract from a Secret. A
// missing version means version 1 for compatibility with existing Secrets.
func JITConfigFromSecretValue(version string, payload []byte, runnerName string) (JITConfig, error) {
	if version != "" && version != JITConfigVersionV1 {
		return JITConfig{}, fmt.Errorf("unsupported JIT config version %q", version)
	}
	if len(payload) == 0 {
		return JITConfig{}, fmt.Errorf("JIT config is empty")
	}
	return JITConfig{EncodedConfig: string(payload), RunnerName: runnerName}, nil
}

// PlatformDriver abstracts CI platform interactions.
// Each method must be safe to call concurrently.
type PlatformDriver interface {
	// GetJITConfig exchanges the stored credential for a one-time runner config.
	GetJITConfig(ctx context.Context) (*JITConfig, error)

	// QueueDepth returns the number of jobs currently queued and waiting for a runner.
	// Returns 0 on error (best-effort; must not break the reconcile loop).
	QueueDepth(ctx context.Context) (int, error)

	// ValidateWebhook verifies the HMAC signature of an inbound webhook payload
	// and returns the number of queued jobs mentioned in the event (0 if not a job event).
	//
	// The expected format of signature varies by platform:
	//   - GitHub Actions / Forgejo: "sha256=<hex>" (the raw value of X-Hub-Signature-256 header)
	//   - GitLab: bare "<hex>" (the raw value of X-Gitlab-Token header when HMAC mode is used)
	//
	// Pass the raw header value for the platform; do not strip or add prefixes.
	ValidateWebhook(payload []byte, signature string) (int, error)
}
