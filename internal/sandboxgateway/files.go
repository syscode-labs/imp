package sandboxgateway

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	sandboxpb "github.com/syscode-labs/imp/internal/proto/sandbox"
	gwpb "github.com/syscode-labs/imp/internal/proto/sandboxgateway"
)

// requireVM validates the VMRef and returns the guest socket path.
func (s *Server) requireVM(vm *gwpb.VMRef) (string, error) {
	if vm == nil || vm.GetNamespace() == "" || vm.GetVmName() == "" {
		return "", status.Error(codes.InvalidArgument, "vm.namespace and vm.vm_name are required")
	}
	return VSOCKPath(s.opts.SocketDir, vm.GetNamespace(), vm.GetVmName()), nil
}

// withSession runs fn with a freshly opened guest session for socket.
func (s *Server) withSession(socket string, fn func(ctx context.Context, c sandboxpb.SandboxControlClient, sessionID string) error) error {
	guestToken, err := guestToken()
	if err != nil {
		return err
	}
	return withGuestConn(socket, func(ctx context.Context, c sandboxpb.SandboxControlClient) error {
		session, err := openGuestSession(ctx, c, guestToken)
		if err != nil {
			return err
		}
		defer closeGuestSession(ctx, c, session)
		return fn(ctx, c, session)
	})
}

func (s *Server) ReadFile(ctx context.Context, req *gwpb.ReadFileRequest) (*gwpb.ReadFileResponse, error) {
	socket, err := s.requireVM(req.GetVm())
	if err != nil {
		return nil, err
	}
	var resp *gwpb.ReadFileResponse
	err = s.withSession(socket, func(ctx context.Context, c sandboxpb.SandboxControlClient, session string) error {
		r, err := c.ReadFile(ctx, &sandboxpb.ReadFileRequest{SessionId: session, Path: req.GetPath()})
		if err != nil {
			return err
		}
		resp = &gwpb.ReadFileResponse{Content: r.GetContent(), Size: r.GetSize(), Truncated: r.GetTruncated()}
		return nil
	})
	return resp, err
}

func (s *Server) WriteFile(ctx context.Context, req *gwpb.WriteFileRequest) (*gwpb.WriteFileResponse, error) {
	socket, err := s.requireVM(req.GetVm())
	if err != nil {
		return nil, err
	}
	var resp *gwpb.WriteFileResponse
	err = s.withSession(socket, func(ctx context.Context, c sandboxpb.SandboxControlClient, session string) error {
		w, err := c.WriteFile(ctx, &sandboxpb.WriteFileRequest{
			SessionId: session,
			Path:      req.GetPath(),
			Content:   req.GetContent(),
			Append:    req.GetAppend(),
			Mode:      req.GetMode(),
		})
		if err != nil {
			return err
		}
		resp = &gwpb.WriteFileResponse{BytesWritten: w.GetBytesWritten()}
		return nil
	})
	return resp, err
}

func (s *Server) ListDir(ctx context.Context, req *gwpb.ListDirRequest) (*gwpb.ListDirResponse, error) {
	socket, err := s.requireVM(req.GetVm())
	if err != nil {
		return nil, err
	}
	var resp *gwpb.ListDirResponse
	err = s.withSession(socket, func(ctx context.Context, c sandboxpb.SandboxControlClient, session string) error {
		l, err := c.ListDir(ctx, &sandboxpb.ListDirRequest{SessionId: session, Path: req.GetPath()})
		if err != nil {
			return err
		}
		entries := make([]*gwpb.GuestFileEntry, 0, len(l.GetEntries()))
		for _, e := range l.GetEntries() {
			entries = append(entries, &gwpb.GuestFileEntry{
				Name:         e.GetName(),
				Size:         e.GetSize(),
				IsDir:        e.GetIsDir(),
				ModifiedUnix: e.GetModifiedUnix(),
			})
		}
		resp = &gwpb.ListDirResponse{Entries: entries}
		return nil
	})
	return resp, err
}

func (s *Server) Stat(ctx context.Context, req *gwpb.StatRequest) (*gwpb.StatResponse, error) {
	socket, err := s.requireVM(req.GetVm())
	if err != nil {
		return nil, err
	}
	var resp *gwpb.StatResponse
	err = s.withSession(socket, func(ctx context.Context, c sandboxpb.SandboxControlClient, session string) error {
		st, err := c.Stat(ctx, &sandboxpb.StatRequest{SessionId: session, Path: req.GetPath()})
		if err != nil {
			return err
		}
		resp = &gwpb.StatResponse{
			Size:         st.GetSize(),
			IsDir:        st.GetIsDir(),
			ModifiedUnix: st.GetModifiedUnix(),
			Mode:         st.GetMode(),
		}
		return nil
	})
	return resp, err
}

func (s *Server) Remove(ctx context.Context, req *gwpb.RemoveRequest) (*gwpb.RemoveResponse, error) {
	socket, err := s.requireVM(req.GetVm())
	if err != nil {
		return nil, err
	}
	err = s.withSession(socket, func(ctx context.Context, c sandboxpb.SandboxControlClient, session string) error {
		_, err := c.Remove(ctx, &sandboxpb.RemoveRequest{
			SessionId: session,
			Path:      req.GetPath(),
			Recursive: req.GetRecursive(),
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	return &gwpb.RemoveResponse{}, nil
}
