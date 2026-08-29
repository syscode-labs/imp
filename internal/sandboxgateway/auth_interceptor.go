package sandboxgateway

import (
	"context"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	metaAuthorization = "authorization"
	metaSandboxUID    = "imp-sandbox-uid"
	metaSandboxNS     = "imp-sandbox-namespace"
	metaSandboxVMName = "imp-sandbox-vm-name"
	bearerPrefix      = "bearer "
)

type principal struct {
	uid       string
	namespace string
	vmName    string
}

type principalContextKey struct{}

type serverStreamWithContext struct {
	grpc.ServerStream
	ctx context.Context
}

func (s serverStreamWithContext) Context() context.Context { return s.ctx }

// AuthUnary and AuthStream enforce a bearer token cryptographically bound to
// a sandbox UID and its backing VM. Every data-plane handler verifies its
// request VMRef against this principal before opening a guest socket.
func (s *Server) AuthUnary(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	ctx, err := s.authorize(ctx)
	if err != nil {
		return nil, err
	}
	return handler(ctx, req)
}

func (s *Server) AuthStream(srv interface{}, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	ctx, err := s.authorize(ss.Context())
	if err != nil {
		return err
	}
	return handler(srv, serverStreamWithContext{ServerStream: ss, ctx: ctx})
}

func (s *Server) authorize(ctx context.Context) (context.Context, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing metadata")
	}
	p, err := metadataPrincipal(md)
	if err != nil {
		return nil, err
	}
	auth := ""
	for _, v := range md.Get(metaAuthorization) {
		if strings.HasPrefix(strings.ToLower(v), bearerPrefix) {
			auth = v[len(bearerPrefix):]
			break
		}
	}
	if auth == "" {
		return nil, status.Error(codes.Unauthenticated, "missing bearer token")
	}
	if !Verify(s.hmacKey, p.uid, p.namespace, p.vmName, auth) {
		return nil, status.Error(codes.Unauthenticated, "invalid token")
	}
	return context.WithValue(ctx, principalContextKey{}, p), nil
}

func metadataPrincipal(md metadata.MD) (principal, error) {
	values := func(key string) (string, bool) {
		v := md.Get(key)
		if len(v) != 1 || v[0] == "" {
			return "", false
		}
		return v[0], true
	}
	uid, ok := values(metaSandboxUID)
	if !ok {
		return principal{}, status.Error(codes.Unauthenticated, "missing imp-sandbox-uid")
	}
	namespace, ok := values(metaSandboxNS)
	if !ok {
		return principal{}, status.Error(codes.Unauthenticated, "missing imp-sandbox-namespace")
	}
	vmName, ok := values(metaSandboxVMName)
	if !ok {
		return principal{}, status.Error(codes.Unauthenticated, "missing imp-sandbox-vm-name")
	}
	return principal{uid: uid, namespace: namespace, vmName: vmName}, nil
}

func principalFromContext(ctx context.Context) (principal, bool) {
	p, ok := ctx.Value(principalContextKey{}).(principal)
	return p, ok
}
