package runner

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGitHubApp_mintAcceptsOKAndCreated(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pemStr := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	for _, status := range []int{http.StatusOK, http.StatusCreated} {
		t.Run(fmt.Sprintf("status-%d", status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"token":"ghs_test","expires_at":"2099-01-01T00:00:00Z"}`))
			}))
			defer srv.Close()
			s, err := newGitHubAppSource(GitHubAppCredentials{PrivateKeyPEM: pemStr, AppID: 1, Installation: 2})
			if err != nil {
				t.Fatal(err)
			}
			s.tokenURL = srv.URL
			tok, err := s.installationTokenFor(context.Background())
			if err != nil || tok != "ghs_test" {
				t.Fatalf("token = %q, err = %v", tok, err)
			}
		})
	}
}
