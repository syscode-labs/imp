package runner

import (
	"fmt"
	"strings"
)

// AuthResolutionError is safe to expose in status and logs: it never includes
// Secret values or the contents of a credential.
type AuthResolutionError struct {
	Reason string
}

func (e *AuthResolutionError) Error() string { return "runner authentication rejected: " + e.Reason }

// PlatformAuth is the operator-owned, secret-free driver input contract.
type PlatformAuth struct {
	Provider    string
	Source      string
	Scope       string
	RunnerGroup string
	Credentials map[string][]byte
}

// ResolvePlatformAuth validates the existing pool fields and the named Secret
// keys. It deliberately does not search for a usable value under another key.
func ResolvePlatformAuth(provider, source, scope, runnerGroup string, credentials map[string][]byte) (PlatformAuth, error) {
	if provider == "" {
		return PlatformAuth{}, &AuthResolutionError{Reason: "provider is required"}
	}
	if scope == "" {
		return PlatformAuth{}, &AuthResolutionError{Reason: "exclusive org or repo scope is required"}
	}
	if runnerGroup == "" && provider == "github-actions" {
		return PlatformAuth{}, &AuthResolutionError{Reason: "runner group is required"}
	}
	if provider == "github-actions" {
		if source == "" {
			source = "pat"
		}
		if source != "pat" && source != "github_app" {
			return PlatformAuth{}, &AuthResolutionError{Reason: "unsupported token source"}
		}
	} else if source == "github_app" {
		return PlatformAuth{}, &AuthResolutionError{Reason: "github_app is supported only for github-actions"}
	} else if source == "" {
		source = "pat"
	}
	if !validScope(provider, scope) {
		return PlatformAuth{}, &AuthResolutionError{Reason: "scope must be an exclusive org:<name> or repo:<owner>/<repo>"}
	}
	if source == "github_app" {
		for _, key := range []string{"github-app-id", "github-app-installation-id", "github-app-private-key"} {
			if len(credentials[key]) == 0 {
				return PlatformAuth{}, &AuthResolutionError{Reason: "required GitHub App credential key is missing"}
			}
		}
	} else if len(credentials["token"]) == 0 {
		return PlatformAuth{}, &AuthResolutionError{Reason: "named credential key token is missing"}
	}
	return PlatformAuth{Provider: provider, Source: source, Scope: scope, RunnerGroup: runnerGroup, Credentials: credentials}, nil
}

func validScope(provider, scope string) bool {
	if provider == "gitlab" {
		return strings.HasPrefix(scope, "group:") || strings.HasPrefix(scope, "project:")
	}
	if strings.HasPrefix(scope, "org:") {
		return len(strings.TrimPrefix(scope, "org:")) > 0 && !strings.Contains(strings.TrimPrefix(scope, "org:"), "/")
	}
	if strings.HasPrefix(scope, "repo:") {
		parts := strings.Split(strings.TrimPrefix(scope, "repo:"), "/")
		return len(parts) == 2 && parts[0] != "" && parts[1] != "" && !strings.Contains(parts[0], "/") && !strings.Contains(parts[1], "/")
	}
	return false
}

func ParseNumericCredential(data map[string][]byte, key string) (int64, error) {
	var n int64
	if _, err := fmt.Sscan(strings.TrimSpace(string(data[key])), &n); err != nil || n <= 0 {
		return 0, &AuthResolutionError{Reason: "numeric credential is invalid"}
	}
	return n, nil
}
