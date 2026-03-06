package runner

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	gitlab "github.com/xanzy/go-gitlab"
)

// GitLabDriver implements PlatformDriver for GitLab CI.
type GitLabDriver struct {
	client            *gitlab.Client //nolint:staticcheck // go-gitlab deprecated in favour of gitlab.com/gitlab-org/api/client-go; migration deferred
	groupPath         string         // non-empty for group-level scope
	projectID         string         // non-empty for project-level scope
	registrationToken string         // runner registration token
	hmacSecret        []byte
}

// NewGitLabDriver creates a driver for a GitLab instance.
// token is a runner registration token (not a PAT).
// scope must be "group:<path>" or "project:<id>".
// serverURL is the GitLab base URL (e.g. "https://gitlab.com"). Empty uses gitlab.com.
func NewGitLabDriver(token, serverURL, scope string, hmacSecret []byte) (*GitLabDriver, error) {
	opts := []gitlab.ClientOptionFunc{}
	if serverURL != "" {
		opts = append(opts, gitlab.WithBaseURL(serverURL))
	}
	client, err := gitlab.NewClient(token, opts...) //nolint:staticcheck
	if err != nil {
		return nil, fmt.Errorf("gitlab client: %w", err)
	}
	d := &GitLabDriver{client: client, registrationToken: token, hmacSecret: hmacSecret}
	switch {
	case strings.HasPrefix(scope, "group:"):
		d.groupPath = strings.TrimPrefix(scope, "group:")
	case strings.HasPrefix(scope, "project:"):
		d.projectID = strings.TrimPrefix(scope, "project:")
	default:
		return nil, fmt.Errorf("invalid scope %q: must start with group: or project", scope)
	}
	return d, nil
}

// GetJITConfig registers a new runner with GitLab and returns its token.
// NOTE: GitLab does not provide a true JIT (single-use) runner token equivalent
// to GitHub Actions' JIT config. This creates a persistent runner registration.
// Caller is responsible for deregistering the runner after the VM terminates
// by calling the GitLab DELETE /api/v4/runners endpoint with the runner token.
// Accumulated stale registrations can be cleaned up via the GitLab UI or API.
func (d *GitLabDriver) GetJITConfig(ctx context.Context) (*JITConfig, error) {
	opts := &gitlab.RegisterNewRunnerOptions{
		Token:       gitlab.Ptr(d.registrationToken),
		Description: gitlab.Ptr("imp-runner"),
		TagList:     &[]string{"self-hosted"},
	}
	runner, _, err := d.client.Runners.RegisterNewRunner(opts, gitlab.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("GitLab RegisterNewRunner: %w", err)
	}
	return &JITConfig{
		EncodedConfig: runner.Token,
		RunnerName:    fmt.Sprintf("imp-runner-%d", runner.ID),
	}, nil
}

func (d *GitLabDriver) QueueDepth(_ context.Context) (int, error) {
	// GitLab does not expose a simple queue depth endpoint without admin scope.
	// Job detection relies on webhooks.
	return 0, nil
}

// ValidateWebhook verifies the payload against the HMAC secret.
// signature must be the raw hex string from X-Gitlab-Token (no "sha256=" prefix).
func (d *GitLabDriver) ValidateWebhook(payload []byte, signature string) (int, error) {
	if len(d.hmacSecret) > 0 {
		mac := hmac.New(sha256.New, d.hmacSecret)
		mac.Write(payload)
		expected := hex.EncodeToString(mac.Sum(nil))
		if !hmac.Equal([]byte(expected), []byte(signature)) {
			return 0, errors.New("invalid GitLab webhook signature")
		}
	}
	var event struct {
		ObjectKind  string `json:"object_kind"`
		BuildStatus string `json:"build_status"`
	}
	if err := json.Unmarshal(payload, &event); err != nil {
		return 0, nil
	}
	if event.ObjectKind == "build" && event.BuildStatus == "pending" {
		return 1, nil
	}
	return 0, nil
}

// compile-time assertion
var _ PlatformDriver = (*GitLabDriver)(nil)
