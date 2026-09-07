package runner

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func testKey(t *testing.T) (string, *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("gen key: %v", err)
	}
	pemStr := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	return pemStr, key
}

func TestGitHubApp_appJWT_claims(t *testing.T) {
	pemStr, key := testKey(t)
	s, err := newGitHubAppSource(GitHubAppCredentials{PrivateKeyPEM: pemStr, AppID: 12345, Installation: 777})
	if err != nil {
		t.Fatalf("source: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	s.now = func() time.Time { return now }

	jwtStr, err := s.appJWT(now)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	parsed, err := jwt.ParseWithClaims(jwtStr, &jwt.RegisteredClaims{}, func(tok *jwt.Token) (any, error) {
		if tok.Method != jwt.SigningMethodRS256 {
			return nil, fmt.Errorf("alg %v, want RS256", tok.Header["alg"])
		}
		return &key.PublicKey, nil
	})
	if err != nil {
		t.Fatalf("parse/verify: %v", err)
	}
	claims := parsed.Claims.(*jwt.RegisteredClaims)
	if claims.Issuer != "12345" {
		t.Errorf("iss = %q, want 12345", claims.Issuer)
	}
	gotIAT := claims.IssuedAt.Time.Sub(now)
	if gotIAT != -60*time.Second {
		t.Errorf("iat offset = %v, want -60s", gotIAT)
	}
	gotEXP := claims.ExpiresAt.Time.Sub(now)
	if gotEXP != 9*time.Minute {
		t.Errorf("exp offset = %v, want 9m", gotEXP)
	}
}

func TestGitHubApp_mintAndCache_reuse(t *testing.T) {
	pemStr, _ := testKey(t)
	s, _ := newGitHubAppSource(GitHubAppCredentials{PrivateKeyPEM: pemStr, AppID: 1, Installation: 2})
	var mints int32
	s.now = func() time.Time { return time.Now() }
	s.mintOverride = func(ctx context.Context) (string, time.Time, error) {
		atomic.AddInt32(&mints, 1)
		return "ghs_opaque_token", time.Now().Add(time.Hour), nil
	}
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		tok, err := s.installationTokenFor(ctx)
		if err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
		if tok != "ghs_opaque_token" {
			t.Errorf("token = %q", tok)
		}
	}
	if mints != 1 {
		t.Errorf("mints = %d, want 1 (cache reuse)", mints)
	}
}

func TestGitHubApp_remintNearExpiry(t *testing.T) {
	pemStr, _ := testKey(t)
	s, _ := newGitHubAppSource(GitHubAppCredentials{PrivateKeyPEM: pemStr, AppID: 1, Installation: 2})
	current := time.Now().Add(4 * time.Minute) // inside 5-min renew window
	s.now = func() time.Time { return time.Now() }
	s.mintOverride = func(ctx context.Context) (string, time.Time, error) {
		return "fresh", time.Now().Add(time.Hour), nil
	}
	s.token, s.exp = "stale", current
	tok, err := s.installationTokenFor(context.Background())
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if tok != "fresh" {
		t.Errorf("token = %q, want fresh (re-mint near expiry)", tok)
	}
}

func TestGitHubApp_singleFlight(t *testing.T) {
	pemStr, _ := testKey(t)
	s, _ := newGitHubAppSource(GitHubAppCredentials{PrivateKeyPEM: pemStr, AppID: 1, Installation: 2})
	var mints int32
	s.now = func() time.Time { return time.Now() }
	release := make(chan struct{})
	s.mintOverride = func(ctx context.Context) (string, time.Time, error) {
		atomic.AddInt32(&mints, 1)
		<-release // hold the first mint
		return "single", time.Now().Add(time.Hour), nil
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.installationTokenFor(context.Background()); err != nil {
				t.Errorf("concurrent mint: %v", err)
			}
		}()
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()
	if mints != 1 {
		t.Errorf("mints = %d, want 1 (single-flight)", mints)
	}
}

func TestGitHubApp_invalidateForcesRemint(t *testing.T) {
	pemStr, _ := testKey(t)
	s, _ := newGitHubAppSource(GitHubAppCredentials{PrivateKeyPEM: pemStr, AppID: 1, Installation: 2})
	var mints int32
	s.now = func() time.Time { return time.Now() }
	s.mintOverride = func(ctx context.Context) (string, time.Time, error) {
		n := atomic.AddInt32(&mints, 1)
		return fmt.Sprintf("tok%d", n), time.Now().Add(time.Hour), nil
	}
	ctx := context.Background()
	if _, err := s.installationTokenFor(ctx); err != nil {
		t.Fatal(err)
	}
	s.invalidate() // 401 path
	tok, err := s.installationTokenFor(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if tok != "tok2" {
		t.Errorf("token = %q, want tok2 after invalidate", tok)
	}
}

func TestGitHubApp_missingFields(t *testing.T) {
	pemStr, _ := testKey(t)
	if _, err := newGitHubAppSource(GitHubAppCredentials{PrivateKeyPEM: pemStr}); err == nil {
		t.Error("want error for missing app/installation IDs")
	}
	if _, err := newGitHubAppSource(GitHubAppCredentials{AppID: 1, Installation: 2}); err == nil {
		t.Error("want error for missing key")
	}
	if _, err := newGitHubAppSource(GitHubAppCredentials{PrivateKeyPEM: "not a pem", AppID: 1, Installation: 2}); err == nil {
		t.Error("want error for bad PEM")
	}
}
