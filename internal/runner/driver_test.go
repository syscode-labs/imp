package runner

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
)

// --- GitHubDriver tests ---

func TestGitHubDriver_scopeParsing_org(t *testing.T) {
	d, err := newGitHubDriverWithClient(nil, "org:my-org", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.org != "my-org" {
		t.Errorf("expected org=my-org, got %q", d.org)
	}
}

func TestGitHubDriver_scopeParsing_repo(t *testing.T) {
	d, err := newGitHubDriverWithClient(nil, "repo:owner/myrepo", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.owner != "owner" || d.repo != "myrepo" {
		t.Errorf("unexpected owner/repo: %s/%s", d.owner, d.repo)
	}
}

func TestGitHubDriver_scopeParsing_invalid(t *testing.T) {
	_, err := newGitHubDriverWithClient(nil, "invalid-scope", nil)
	if err == nil {
		t.Error("expected error for invalid scope")
	}
}

func TestGitHubDriver_scopeParsing_repoMissingSlash(t *testing.T) {
	_, err := newGitHubDriverWithClient(nil, "repo:noslash", nil)
	if err == nil {
		t.Error("expected error for repo scope without slash")
	}
}

func TestGitHubDriver_ValidateWebhook_queuedEvent(t *testing.T) {
	d := &GitHubDriver{hmacSecret: nil}
	payload, _ := json.Marshal(map[string]string{"action": "queued"})
	count, err := d.ValidateWebhook(payload, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 for queued event, got %d", count)
	}
}

func TestGitHubDriver_ValidateWebhook_nonQueuedEvent(t *testing.T) {
	d := &GitHubDriver{hmacSecret: nil}
	payload, _ := json.Marshal(map[string]string{"action": "completed"})
	count, err := d.ValidateWebhook(payload, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 for completed event, got %d", count)
	}
}

func TestGitHubDriver_ValidateWebhook_validHMAC(t *testing.T) {
	secret := []byte("mysecret")
	payload := []byte(`{"action":"queued"}`)
	mac := hmac.New(sha256.New, secret)
	mac.Write(payload)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	d := &GitHubDriver{hmacSecret: secret}
	count, err := d.ValidateWebhook(payload, sig)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1, got %d", count)
	}
}

func TestGitHubDriver_ValidateWebhook_invalidHMAC(t *testing.T) {
	d := &GitHubDriver{hmacSecret: []byte("secret")}
	payload := []byte(`{"action":"queued"}`)
	_, err := d.ValidateWebhook(payload, "sha256=badhash")
	if err == nil {
		t.Error("expected error for invalid HMAC")
	}
}

func TestGitHubDriver_ValidateWebhook_invalidJSON(t *testing.T) {
	d := &GitHubDriver{hmacSecret: nil}
	count, err := d.ValidateWebhook([]byte("not-json"), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 for bad JSON, got %d", count)
	}
}

// --- GitLabDriver tests ---

func TestNewGitLabDriver_invalidScope(t *testing.T) {
	_, err := NewGitLabDriver("token", "", "invalid", nil)
	if err == nil {
		t.Error("expected error for invalid scope")
	}
}

func TestGitLabDriver_ValidateWebhook_pendingBuild(t *testing.T) {
	d := &GitLabDriver{hmacSecret: nil}
	payload, _ := json.Marshal(map[string]string{
		"object_kind":  "build",
		"build_status": "pending",
	})
	count, err := d.ValidateWebhook(payload, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1, got %d", count)
	}
}

func TestGitLabDriver_ValidateWebhook_nonPendingBuild(t *testing.T) {
	d := &GitLabDriver{hmacSecret: nil}
	payload, _ := json.Marshal(map[string]string{
		"object_kind":  "build",
		"build_status": "running",
	})
	count, err := d.ValidateWebhook(payload, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 for running build, got %d", count)
	}
}

func TestGitLabDriver_QueueDepth_returnsZero(t *testing.T) {
	d := &GitLabDriver{}
	n, err := d.QueueDepth(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0, got %d", n)
	}
}

// compile-time assertions (also in source files, but belt-and-suspenders)
var (
	_ PlatformDriver = (*GitHubDriver)(nil)
	_ PlatformDriver = (*GitLabDriver)(nil)
)
