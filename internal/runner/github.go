package runner

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/go-github/v67/github"
	"golang.org/x/oauth2"
)

// GitHubDriver implements PlatformDriver for GitHub Actions and Forgejo.
// Forgejo exposes the same runner API as GitHub Actions; use NewForgejoDriver
// to point it at a Forgejo instance.
type GitHubDriver struct {
	client      *github.Client
	org         string // non-empty for org-level scope
	owner       string // non-empty for repo-level scope
	repo        string // non-empty for repo-level scope
	runnerGroup string
	hmacSecret  []byte
}

// NewGitHubDriver creates a driver for github.com.
// token is a PAT with actions:write scope.
// scope must be "org:<org>" or "repo:<owner>/<repo>".
func NewGitHubDriver(token, scope string, hmacSecret []byte) (*GitHubDriver, error) {
	return NewGitHubDriverWithGroup(token, scope, "", hmacSecret)
}

// NewGitHubDriverWithGroup preserves the named runner-group contract for PATs.
func NewGitHubDriverWithGroup(token, scope, runnerGroup string, hmacSecret []byte) (*GitHubDriver, error) {
	if token == "" {
		return nil, &AuthResolutionError{Reason: "named credential key token is missing"}
	}
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	client := github.NewClient(oauth2.NewClient(context.Background(), ts))
	return newGitHubDriverWithClient(client, scope, runnerGroup, hmacSecret)
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
	return newGitHubDriverWithClient(client, scope, "", hmacSecret)
}

// NewGitHubAppDriver creates a driver for github.com authenticated as a
// GitHub App installation. The source mints installation tokens on demand
// (re-minted near expiry or after a 401; never refreshed) and signs each App
// JWT from the private key held in creds.
func NewGitHubAppDriver(config GitHubConfig, hmacSecret []byte) (*GitHubDriver, error) {
	src, err := newGitHubAppSource(config.Authentication)
	if err != nil {
		return nil, err
	}
	client := github.NewClient(&http.Client{Transport: src})
	return newGitHubDriverWithClient(client, config.Scope, config.RunnerGroup, hmacSecret)
}

// RoundTrip implements http.RoundTripper: it supplies a valid installation
// token on every request, minting when the cached one is stale, and retries
// exactly once on a 401 after invalidating the cache (401 may mean the
// installation was revoked or the App's permissions changed — not just expiry).
func (s *githubAppSource) RoundTrip(req *http.Request) (*http.Response, error) {
	tok, err := s.installationTokenFor(req.Context())
	if err != nil {
		return nil, err
	}
	clone := req.Clone(req.Context())
	clone.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultTransport.RoundTrip(clone)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		s.invalidate()
		tok, err = s.installationTokenFor(req.Context())
		if err != nil {
			return nil, err
		}
		clone = req.Clone(req.Context())
		clone.Header.Set("Authorization", "Bearer "+tok)
		return http.DefaultTransport.RoundTrip(clone)
	}
	return resp, nil
}

func newGitHubDriverWithClient(client *github.Client, scope, runnerGroup string, hmacSecret []byte) (*GitHubDriver, error) {
	d := &GitHubDriver{client: client, runnerGroup: runnerGroup, hmacSecret: hmacSecret}
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
	groupID, err := d.runnerGroupID(ctx)
	if err != nil {
		return nil, err
	}
	req := &github.GenerateJITConfigRequest{
		Name:          fmt.Sprintf("imp-runner-%d", time.Now().UnixNano()),
		RunnerGroupID: groupID,
		Labels:        []string{"self-hosted"},
	}
	var cfg *github.JITRunnerConfig
	if d.org != "" {
		cfg, _, err = d.client.Actions.GenerateOrgJITConfig(ctx, d.org, req)
	} else {
		cfg, _, err = d.client.Actions.GenerateRepoJITConfig(ctx, d.owner, d.repo, req)
	}
	if err != nil {
		return nil, fmt.Errorf("GetJITConfig: %w", err)
	}
	if cfg == nil || cfg.GetEncodedJITConfig() == "" || cfg.Runner == nil || cfg.Runner.GetName() == "" {
		return nil, &JITResponseError{Reason: "empty JIT response"}
	}
	return &JITConfig{
		EncodedConfig: cfg.GetEncodedJITConfig(),
		RunnerName:    cfg.Runner.GetName(),
	}, nil
}

func (d *GitHubDriver) runnerGroupID(ctx context.Context) (int64, error) {
	if d.runnerGroup == "" {
		return 1, nil
	}
	if d.org == "" {
		return 0, fmt.Errorf("runner group %q requires organization scope", d.runnerGroup)
	}
	path := fmt.Sprintf("orgs/%s/actions/runner-groups?per_page=100", d.org)
	req, err := d.client.NewRequest(http.MethodGet, path, nil)
	if err != nil {
		return 0, fmt.Errorf("runner group request: %w", err)
	}
	var response struct {
		RunnerGroups []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"runner_groups"`
	}
	if _, err := d.client.Do(ctx, req, &response); err != nil {
		return 0, fmt.Errorf("list runner groups: %w", err)
	}
	for _, group := range response.RunnerGroups {
		if group.Name == d.runnerGroup {
			return group.ID, nil
		}
	}
	return 0, fmt.Errorf("runner group %q not found in organization %q", d.runnerGroup, d.org)
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
