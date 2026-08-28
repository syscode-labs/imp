package sandboxgateway

import (
	"fmt"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	gwpb "github.com/syscode-labs/imp/internal/proto/sandboxgateway"
)

// Options configures the gateway server.
type Options struct {
	SocketDir string
	HMACKey   []byte
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
	return &Server{opts: opts, hmacKey: opts.HMACKey}, nil
}

// ListenAndServe blocks serving gRPC (with reflection + health) on addr.
func (s *Server) ListenAndServe(addr string) error {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}

	gs := grpc.NewServer(
		grpc.ChainUnaryInterceptor(s.AuthUnary),
		grpc.ChainStreamInterceptor(s.AuthStream),
	)
	gwpb.RegisterSandboxGatewayServer(gs, s)
	grpc_health_v1.RegisterHealthServer(gs, health.NewServer())
	reflection.Register(gs)

	return gs.Serve(lis)
}
