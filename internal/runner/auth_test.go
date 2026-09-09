package runner

import (
	"errors"
	"testing"
)

func TestPATDriverRetainsRunnerGroup(t *testing.T) {
	d, err := NewGitHubDriverWithGroup("token", "org:syscode-labs", "omni-runner", nil)
	if err != nil {
		t.Fatal(err)
	}
	if d.runnerGroup != "omni-runner" {
		t.Fatalf("runner group = %q", d.runnerGroup)
	}
}

func TestResolvePlatformAuthAppUsesNamedKeys(t *testing.T) {
	auth, err := ResolvePlatformAuth("github-actions", "github_app", "org:syscode-labs", "omni-runner", map[string][]byte{
		"github-app-id":              []byte("4795312"),
		"github-app-installation-id": []byte("158305388"),
		"github-app-private-key":     []byte("pem"),
		"unrelated":                  []byte("must-not-be-selected"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if auth.Source != "github_app" || auth.RunnerGroup != "omni-runner" {
		t.Fatalf("unexpected auth: %#v", auth)
	}
}

func TestResolvePlatformAuthPATRejectsFallbackAndPreservesGroup(t *testing.T) {
	_, err := ResolvePlatformAuth("github-actions", "pat", "org:syscode-labs", "omni-runner", map[string][]byte{"other": []byte("token")})
	var authErr *AuthResolutionError
	if !errors.As(err, &authErr) {
		t.Fatalf("error = %v, want typed auth error", err)
	}
	if err.Error() == "" || len(err.Error()) > 160 {
		t.Fatalf("error is not secret-safe: %q", err)
	}
	auth, err := ResolvePlatformAuth("github-actions", "pat", "org:syscode-labs", "omni-runner", map[string][]byte{"token": []byte("token")})
	if err != nil || auth.RunnerGroup != "omni-runner" {
		t.Fatalf("PAT auth/group = %#v, %v", auth, err)
	}
}

func TestResolvePlatformAuthRejectsNonExclusiveScopeAndGrouplessGitHub(t *testing.T) {
	for _, tc := range []struct {
		name, scope, group string
	}{
		{name: "missing scope", group: "omni-runner"},
		{name: "both scope forms", scope: "org:org/repo:owner/repo", group: "omni-runner"},
		{name: "missing group", scope: "org:syscode-labs"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ResolvePlatformAuth("github-actions", "pat", tc.scope, tc.group, map[string][]byte{"token": []byte("token")})
			var authErr *AuthResolutionError
			if !errors.As(err, &authErr) {
				t.Fatalf("error = %v, want typed auth error", err)
			}
		})
	}
}

func TestResolvePlatformAuthAcceptsLegacyRawPATDefaults(t *testing.T) {
	auth, err := ResolvePlatformAuth("forgejo", "", "repo:owner/repo", "", map[string][]byte{"token": []byte("token")})
	if err != nil || auth.Source != "pat" {
		t.Fatalf("legacy auth = %#v, %v", auth, err)
	}
}
