package sandboxgateway

import (
	"context"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	sandboxpb "github.com/syscode-labs/imp/internal/proto/sandbox"
	gwpb "github.com/syscode-labs/imp/internal/proto/sandboxgateway"
)

// Exec runs a command in the guest and streams its output as frames.
// v1 performs a single buffered guest Exec and emits one stdout frame, one
// stderr frame (when non-empty), and a final exit frame; the streaming
// signature keeps the wire contract stable for incremental streaming later.
func (s *Server) Exec(req *gwpb.ExecRequest, stream gwpb.SandboxGateway_ExecServer) error {
	socket, err := s.requireVM(stream.Context(), req.GetVm())
	if err != nil {
		return err
	}
	if len(req.GetCommand()) == 0 {
		return status.Error(codes.InvalidArgument, "command is required")
	}
	guestToken, err := guestToken()
	if err != nil {
		return err
	}
	timeout := time.Duration(req.GetTimeoutSeconds()) * time.Second
	if timeout <= 0 || timeout > execTimeout {
		timeout = execTimeout
	}

	var execResp *sandboxpb.ExecResponse
	err = withGuestConn(socket, func(ctx context.Context, c sandboxpb.SandboxControlClient) error {
		gCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		session, err := openGuestSession(gCtx, c, guestToken)
		if err != nil {
			return err
		}
		defer closeGuestSession(gCtx, c, session)
		execResp, err = c.Exec(gCtx, &sandboxpb.ExecRequest{
			SessionId:      session,
			Command:        req.GetCommand(),
			TimeoutSeconds: int32(timeout.Seconds()),
		})
		return err
	})
	if err != nil {
		return err
	}

	if out := execResp.GetStdout(); out != "" {
		if err := stream.Send(&gwpb.ExecFrame{Payload: &gwpb.ExecFrame_Stdout{Stdout: out}}); err != nil {
			return err
		}
	}
	if errText := execResp.GetStderr(); errText != "" {
		if err := stream.Send(&gwpb.ExecFrame{Payload: &gwpb.ExecFrame_Stderr{Stderr: errText}}); err != nil {
			return err
		}
	}
	return stream.Send(&gwpb.ExecFrame{
		ExitCode: execResp.GetExitCode(),
		Final:    true,
	})
}
