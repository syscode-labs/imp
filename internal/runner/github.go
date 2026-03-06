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

	"github.com/google/go-github/v67/github"
	"golang.org/x/oauth2"
)

// GitHubDriver implements PlatformDriver for GitHub Actions and Forgejo.
// Forgejo exposes the same runner API as GitHub Actions; use NewForgejoDriver
// to point it at a Forgejo instance.
type GitHubDriver struct {
	client     *github.Client
	org        string // non-empty for org-level scope
	owner      string // non-empty for repo-level scope
	repo       string // non-empty for repo-level scope
	hmacSecret []byte
}

// NewGitHubDriver creates a driver for github.com.
// token is a PAT with actions:write scope.
// scope must be "org:<org>" or "repo:<owner>/<repo>".
func NewGitHubDriver(token, scope string, hmacSecret []byte) (*GitHubDriver, error) {
	// context.Background() is used here intentionally: the token source is created
	// once at startup and holds a static PAT (no refresh flow). Per-request contexts
	// are applied via the ctx parameter passed to each method call.
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	client := github.NewClient(oauth2.NewClient(context.Background(), ts))
	return newGitHubDriverWithClient(client, scope, hmacSecret)
}

// NewForgejoDriver creates a driver for a Forgejo instance.
// Forgejo implements the GitHub Actions runner API; serverURL is the Forgejo base URL.
func NewForgejoDriver(token, serverURL, scope string, hmacSecret []byte) (*GitHubDriver, error) {
	// context.Background() is used here intentionally: the token source is created
	// once at startup and holds a static PAT (no refresh flow). Per-request contexts
	// are applied via the ctx parameter passed to each method call.
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	client := github.NewClient(oauth2.NewClient(context.Background(), ts))
	baseURL := strings.TrimRight(serverURL, "/") + "/api/v1/"
	var err error
	client, err = client.WithEnterpriseURLs(baseURL, baseURL)
	if err != nil {
		return nil, fmt.Errorf("forgejo client: %w", err)
	}
	return newGitHubDriverWithClient(client, scope, hmacSecret)
}

func newGitHubDriverWithClient(client *github.Client, scope string, hmacSecret []byte) (*GitHubDriver, error) {
	d := &GitHubDriver{client: client, hmacSecret: hmacSecret}
	switch {
	case strings.HasPrefix(scope, "org:"):
		d.org = strings.TrimPrefix(scope, "org:")
	case strings.HasPrefix(scope, "repo:"):
		parts := strings.SplitN(strings.TrimPrefix(scope, "repo:"), "/", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid repo scope %q: expected owner/repo", scope)
		}
		d.owner, d.repo = parts[0], parts[1]
	default:
		return nil, fmt.Errorf("invalid scope %q: must start with org: or repo", scope)
	}
	return d, nil
}

func (d *GitHubDriver) GetJITConfig(ctx context.Context) (*JITConfig, error) {
	req := &github.GenerateJITConfigRequest{
		Name:          "imp-runner",
		RunnerGroupID: 1,
		Labels:        []string{"self-hosted"},
	}
	var cfg *github.JITRunnerConfig
	var err error
	if d.org != "" {
		cfg, _, err = d.client.Actions.GenerateOrgJITConfig(ctx, d.org, req)
	} else {
		cfg, _, err = d.client.Actions.GenerateRepoJITConfig(ctx, d.owner, d.repo, req)
	}
	if err != nil {
		return nil, fmt.Errorf("GetJITConfig: %w", err)
	}
	return &JITConfig{
		EncodedConfig: cfg.GetEncodedJITConfig(),
		RunnerName:    cfg.Runner.GetName(),
	}, nil
}

func (d *GitHubDriver) QueueDepth(ctx context.Context) (int, error) {
	// Org-level workflow run listing is not available in the go-github SDK;
	// fall back to repo-level when owner/repo are set, otherwise return 0.
	if d.org != "" {
		return 0, nil // best-effort: org queue depth not available without GraphQL
	}
	opts := &github.ListWorkflowRunsOptions{Status: "queued"}
	runs, _, err := d.client.Actions.ListRepositoryWorkflowRuns(ctx, d.owner, d.repo, opts)
	if err != nil {
		return 0, nil // best-effort
	}
	return runs.GetTotalCount(), nil
}

func (d *GitHubDriver) ValidateWebhook(payload []byte, signature string) (int, error) {
	if !d.validHMAC(payload, signature) {
		return 0, errors.New("invalid webhook signature")
	}
	var event struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal(payload, &event); err != nil {
		return 0, nil
	}
	if event.Action == "queued" {
		return 1, nil
	}
	return 0, nil
}

func (d *GitHubDriver) validHMAC(payload []byte, signature string) bool {
	if len(d.hmacSecret) == 0 {
		return true
	}
	mac := hmac.New(sha256.New, d.hmacSecret)
	mac.Write(payload)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}

// compile-time assertion
var _ PlatformDriver = (*GitHubDriver)(nil)
