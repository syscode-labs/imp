package runner

import (
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ErrInstallationUnavailable is returned when the installation token cannot be
// minted or re-minted: the installation was revoked, uninstalled (new
// installation ID), or the App's permissions changed pending org re-approval.
var ErrInstallationUnavailable = errors.New("github app installation unavailable (revoked, uninstalled, or pending permission approval)")

// GitHubAppCredentials holds the static GitHub App secret surface.
// Exactly three fields: the App private key (PEM), the App ID, and the
// installation ID. Everything else is derived at runtime and never persisted.
type GitHubAppCredentials struct {
	PrivateKeyPEM string
	AppID         int64
	Installation  int64
}

// githubAppSource mints installation tokens on demand. It mirrors the
// ghinstallation transport model: mutex-guarded check-then-mint with re-mint
// (never refresh) when the cached token is within renewWindow of expiry or was
// rejected with a 401. There is no background refresher: a registered runner
// holds its own credentials, and the API credential is only needed at
// provision/remove/reconcile time.
type githubAppSource struct {
	creds GitHubAppCredentials

	mu    sync.Mutex
	token string
	exp   time.Time

	// now is swappable for tests.
	now func() time.Time

	// mintOverride, when non-nil, replaces the HTTP mint call in tests.
	mintOverride func(ctx context.Context) (token string, exp time.Time, err error)
}

// renewWindow is how far before expiry a token is considered stale.
const renewWindow = 5 * time.Minute

// jwtLifetime is the App JWT lifetime; kept off the 10-minute API maximum.
const jwtLifetime = 9 * time.Minute

// newGitHubAppSource builds a mint-on-demand token source.
func newGitHubAppSource(creds GitHubAppCredentials) (*githubAppSource, error) {
	if creds.AppID == 0 || creds.Installation == 0 {
		return nil, fmt.Errorf("github app credentials require appId and installationId")
	}
	if _, err := parseAppPrivateKey(creds.PrivateKeyPEM); err != nil {
		return nil, err
	}
	return &githubAppSource{creds: creds, now: time.Now}, nil
}

// parseAppPrivateKey decodes the PEM private key exactly once for validation
// at construction; the key is re-read from the source material at mint time.
func parseAppPrivateKey(pemStr string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, fmt.Errorf("github app private key: no PEM block found")
	}
	var key *rsa.PrivateKey
	// RSAPrivateKey PKCS1 is the format GitHub issues ("RSA PRIVATE KEY").
	if k, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		key = k
	} else if k8, err2 := x509.ParsePKCS8PrivateKey(block.Bytes); err2 == nil {
		r, ok := k8.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("github app private key: PKCS8 block is not RSA")
		}
		key = r
	} else {
		return nil, fmt.Errorf("github app private key: unsupported format: %w", err)
	}
	return key, nil
}

// appJWT signs the short-lived RS256 App JWT.
// iss = App ID, iat = now-60s (clock drift), exp = now+9min (API max is 10).
func (s *githubAppSource) appJWT(now time.Time) (string, error) {
	key, err := parseAppPrivateKey(s.creds.PrivateKeyPEM)
	if err != nil {
		return "", err
	}
	claims := jwt.RegisteredClaims{
		Issuer:    fmt.Sprintf("%d", s.creds.AppID),
		IssuedAt:  jwt.NewNumericDate(now.Add(-60 * time.Second)),
		ExpiresAt: jwt.NewNumericDate(now.Add(jwtLifetime)),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	return token.SignedString(key)
}

// installationTokenFor returns a valid installation token, minting if needed.
// Single-flight: the mutex is held across the check and the mint, so
// concurrent callers block and share one mint.
func (s *githubAppSource) installationTokenFor(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.token != "" && s.now().Add(renewWindow).Before(s.exp) {
		return s.token, nil
	}
	return s.mint(ctx)
}

// invalidate drops the cached token; called after a 401.
func (s *githubAppSource) invalidate() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.token, s.exp = "", time.Time{}
}

// mint performs JWT exchange for a fresh installation token.
func (s *githubAppSource) mint(ctx context.Context) (string, error) {
	if s.mintOverride != nil {
		token, exp, err := s.mintOverride(ctx)
		if err != nil {
			return "", err
		}
		s.token, s.exp = token, exp
		return token, nil
	}
	jwtStr, err := s.appJWT(s.now())
	if err != nil {
		return "", err
	}
	url := fmt.Sprintf("https://api.github.com/app/installations/%d/access_tokens", s.creds.Installation)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(nil))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+jwtStr)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := http.DefaultClient.Do(req) // #nosec G704 -- URL is fixed to api.github.com; installation ID is numeric
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	switch {
	case resp.StatusCode == http.StatusOK:
		var out struct {
			Token     string    `json:"token"`
			ExpiresAt time.Time `json:"expires_at"`
		}
		if err := json.Unmarshal(body, &out); err != nil {
			return "", fmt.Errorf("github app installation token decode: %w", err)
		}
		if out.Token == "" {
			return "", fmt.Errorf("github app installation token: empty token in response")
		}
		s.token, s.exp = out.Token, out.ExpiresAt
		return out.Token, nil
	case resp.StatusCode == http.StatusNotFound:
		return "", fmt.Errorf("%w: installation %d not found for app %d", ErrInstallationUnavailable, s.creds.Installation, s.creds.AppID)
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return "", fmt.Errorf("%w: status %d minting token for installation %d", ErrInstallationUnavailable, resp.StatusCode, s.creds.Installation)
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		return "", fmt.Errorf("github app token mint: transient %d (retry with backoff)", resp.StatusCode)
	default:
		return "", fmt.Errorf("github app token mint: unexpected status %d", resp.StatusCode)
	}
}
