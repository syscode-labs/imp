package sandboxgateway

import (
	"crypto/tls"
	"fmt"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	gwpb "github.com/syscode-labs/imp/internal/proto/sandboxgateway"
)

// Options configures the gateway server.
type Options struct {
	SocketDir string
	HMACKey   []byte
	// TLSCertFile and TLSKeyFile enable TLS when both are set. When empty the
	// server serves plaintext for compatibility/internal preview.
	TLSCertFile string
	TLSKeyFile  string
}

// Server is the node-local sandbox gateway.
type Server struct {
	gwpb.UnimplementedSandboxGatewayServer
	opts    Options
	hmacKey []byte
}

// NewServer validates options and constructs the gateway.
func NewServer(opts Options) (*Server, error) {
	if opts.SocketDir == "" {
		return nil, fmt.Errorf("socket dir is required")
	}
	if len(opts.HMACKey) == 0 {
		return nil, fmt.Errorf("hmac key is required")
	}
	if (opts.TLSCertFile != "") != (opts.TLSKeyFile != "") {
		return nil, fmt.Errorf("both tls cert and key must be set together")
	}
	return &Server{opts: opts, hmacKey: opts.HMACKey}, nil
}

// ListenAndServe blocks serving gRPC (with reflection + health) on addr.
func (s *Server) ListenAndServe(addr string) error {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}

	opts := []grpc.ServerOption{
		grpc.ChainUnaryInterceptor(s.AuthUnary),
		grpc.ChainStreamInterceptor(s.AuthStream),
	}
	if s.opts.TLSCertFile != "" {
		tlsCfg, err := loadTLSConfig(s.opts.TLSCertFile, s.opts.TLSKeyFile)
		if err != nil {
			return err
		}
		opts = append(opts, grpc.Creds(credentials.NewTLS(tlsCfg)))
	}

	gs := grpc.NewServer(opts...)
	gwpb.RegisterSandboxGatewayServer(gs, s)
	grpc_health_v1.RegisterHealthServer(gs, health.NewServer())
	reflection.Register(gs)

	return gs.Serve(lis)
}

func loadTLSConfig(certFile, keyFile string) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load tls keypair: %w", err)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
	}, nil
}
