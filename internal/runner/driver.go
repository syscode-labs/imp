package runner

import "context"

// JITConfig is a one-time runner registration token issued by the platform.
// The runner binary uses this to register itself and pick up exactly one job.
type JITConfig struct {
	// EncodedConfig is passed directly to the runner binary at startup.
	EncodedConfig string
	// RunnerName is the name assigned by the platform.
	RunnerName string
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
	ValidateWebhook(payload []byte, signature string) (int, error)
}
