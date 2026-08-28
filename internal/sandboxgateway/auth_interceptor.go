package sandboxgateway

import (
	"context"
	"crypto/subtle"
	"fmt"
	"os"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	metaAuthorization = "authorization"
	metaSandboxUID    = "imp-sandbox-uid"
	bearerPrefix      = "bearer "
)

// AuthUnary and AuthStream enforce per-sandbox HMAC bearer auth. Callers
// present metadata: authorization: Bearer <token> plus imp-sandbox-uid.
// The token must equal Token(key, uid); both pieces are required so a
// leaked token alone (without knowing which sandbox it belongs to) and a
// guessed UID alone are both useless.
func (s *Server) AuthUnary(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	if err := s.authorize(ctx); err != nil {
		return nil, err
	}
	return handler(ctx, req)
}

func (s *Server) AuthStream(srv interface{}, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	if err := s.authorize(ss.Context()); err != nil {
		return err
	}
	return handler(srv, ss)
}

func (s *Server) authorize(ctx context.Context) error {
	md, ok := metadata.FromIncomingContext(ctx)
	if dbg := os.Getenv("GATEWAY_DEBUG_MD"); dbg != "" {
		fmt.Printf("AUTHORIZE ok=%v md=%v\n", ok, md)
	}
	if !ok {
		return status.Error(codes.Unauthenticated, "missing metadata")
	}
	uids := md.Get(metaSandboxUID)
	if len(uids) != 1 || uids[0] == "" {
		return status.Error(codes.Unauthenticated, "missing imp-sandbox-uid")
	}
	auth := ""
	for _, v := range md.Get(metaAuthorization) {
		if strings.HasPrefix(strings.ToLower(v), bearerPrefix) {
			auth = v[len(bearerPrefix):]
			break
		}
	}
	if auth == "" {
		return status.Error(codes.Unauthenticated, "missing bearer token")
	}
	if subtle.ConstantTimeCompare([]byte(Token(s.hmacKey, uids[0])), []byte(auth)) != 1 {
		return status.Error(codes.Unauthenticated, "invalid token")
	}
	return nil
}
