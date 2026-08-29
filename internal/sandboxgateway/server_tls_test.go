package sandboxgateway

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
)

func generateSelfSignedCert(t *testing.T) (certFile, keyFile string, certPEM []byte) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	require.NoError(t, err)
	tmpl := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	require.NoError(t, err)
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})

	dir := t.TempDir()
	certFile = filepath.Join(dir, "tls.crt")
	keyFile = filepath.Join(dir, "tls.key")
	require.NoError(t, os.WriteFile(certFile, certPEM, 0600))
	require.NoError(t, os.WriteFile(keyFile, keyPEM, 0600))
	return certFile, keyFile, certPEM
}

func TestNewServer_tlsRequiresBothFiles(t *testing.T) {
	_, err := NewServer(Options{SocketDir: t.TempDir(), HMACKey: []byte("01234567890123456789012345678901"), TLSCertFile: "/tmp/x.crt"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "both tls cert and key")

	_, err = NewServer(Options{SocketDir: t.TempDir(), HMACKey: []byte("01234567890123456789012345678901"), TLSKeyFile: "/tmp/x.key"})
	require.Error(t, err)
}

func TestGateway_TLSOptIn(t *testing.T) {
	certFile, keyFile, certPEM := generateSelfSignedCert(t)

	srv, err := NewServer(Options{
		SocketDir:   t.TempDir(),
		HMACKey:     []byte(testKey),
		TLSCertFile: certFile,
		TLSKeyFile:  keyFile,
	})
	require.NoError(t, err)

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	// Use non-TLS listener; ListenAndServe creates its own, so we test via manual Serve path.
	// Replicate ListenAndServe's grpc server construction but serve on our listener to avoid port race.
	tlsCfg, err := loadTLSConfig(certFile, keyFile)
	require.NoError(t, err)
	require.Equal(t, uint16(tls.VersionTLS13), tlsCfg.MinVersion)

	gs := grpc.NewServer(
		grpc.Creds(credentials.NewTLS(tlsCfg)),
		grpc.ChainUnaryInterceptor(srv.AuthUnary),
		grpc.ChainStreamInterceptor(srv.AuthStream),
	)
	healthpb.RegisterHealthServer(gs, &healthServerStub{})
	go func() { _ = gs.Serve(lis) }()
	t.Cleanup(gs.Stop)

	addr := lis.Addr().String()

	// Plaintext client must fail at transport level (server expects TLS).
	plainConn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer func() { _ = plainConn.Close() }()
	_, err = healthpb.NewHealthClient(plainConn).Check(context.Background(), &healthpb.HealthCheckRequest{})
	require.Error(t, err)
	// Either Unavailable (handshake) or Unknown depending on grpc version; ensure it is NOT Unauthenticated from auth interceptor.
	code := status.Code(err)
	assert.NotEqual(t, codes.Unauthenticated, code, "plaintext to TLS server should not reach auth interceptor, got %v", err)

	// TLS 1.2 client must be rejected due to MinVersion 1.3.
	roots := x509.NewCertPool()
	require.True(t, roots.AppendCertsFromPEM(certPEM))
	tls12Cfg := &tls.Config{
		RootCAs:    roots,
		ServerName: "localhost",
		MinVersion: tls.VersionTLS12,
		MaxVersion: tls.VersionTLS12,
	}
	conn12, err := grpc.NewClient(addr, grpc.WithTransportCredentials(credentials.NewTLS(tls12Cfg)))
	require.NoError(t, err)
	defer func() { _ = conn12.Close() }()
	_, err = healthpb.NewHealthClient(conn12).Check(context.Background(), &healthpb.HealthCheckRequest{})
	require.Error(t, err)
	assert.NotEqual(t, codes.Unauthenticated, status.Code(err), "TLS1.2 should be rejected at handshake, not auth")

	// Valid TLS 1.3 client reaches the auth interceptor (Unauthenticated without bearer).
	tls13Cfg := &tls.Config{
		RootCAs:    roots,
		ServerName: "localhost",
		MinVersion: tls.VersionTLS13,
	}
	tlsConn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(credentials.NewTLS(tls13Cfg)))
	require.NoError(t, err)
	defer func() { _ = tlsConn.Close() }()
	_, err = healthpb.NewHealthClient(tlsConn).Check(context.Background(), &healthpb.HealthCheckRequest{})
	assert.Equal(t, codes.Unauthenticated, status.Code(err), "TLS1.3 handshake should succeed and hit auth gate")

	// Plaintext server still works when TLS not configured (compatibility).
	plainSrv, err := NewServer(Options{SocketDir: t.TempDir(), HMACKey: []byte(testKey)})
	require.NoError(t, err)
	plainLis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	gs2 := grpc.NewServer(
		grpc.ChainUnaryInterceptor(plainSrv.AuthUnary),
		grpc.ChainStreamInterceptor(plainSrv.AuthStream),
	)
	healthpb.RegisterHealthServer(gs2, &healthServerStub{})
	go func() { _ = gs2.Serve(plainLis) }()
	t.Cleanup(gs2.Stop)
	plainConn2, err := grpc.NewClient(plainLis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer func() { _ = plainConn2.Close() }()
	_, err = healthpb.NewHealthClient(plainConn2).Check(context.Background(), &healthpb.HealthCheckRequest{})
	assert.Equal(t, codes.Unauthenticated, status.Code(err), "plaintext server should still gate at auth")
}

// healthServerStub is a minimal health service that lets the auth interceptor run.
type healthServerStub struct {
	healthpb.UnimplementedHealthServer
}

func (h *healthServerStub) Check(ctx context.Context, _ *healthpb.HealthCheckRequest) (*healthpb.HealthCheckResponse, error) {
	return &healthpb.HealthCheckResponse{Status: healthpb.HealthCheckResponse_SERVING}, nil
}
