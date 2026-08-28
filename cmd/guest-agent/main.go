//go:build linux

// cmd/guest-agent is the gRPC server that runs inside Imp microVMs.
// It listens on VSOCK port 10000 and implements probe execution and metrics.
package main

import (
	"fmt"
	"os"

	"github.com/mdlayher/vsock"
	"google.golang.org/grpc"

	"github.com/syscode-labs/imp/internal/guest"
	pb "github.com/syscode-labs/imp/internal/proto/guest"
	sandboxpb "github.com/syscode-labs/imp/internal/proto/sandbox"
)

const vsockPort = 10000

func main() {
	l, err := vsock.Listen(vsockPort, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "guest-agent: listen vsock:%d: %v\n", vsockPort, err)
		os.Exit(1)
	}
	fmt.Printf("guest-agent: listening on vsock port %d\n", vsockPort)

	srv := grpc.NewServer()
	pb.RegisterGuestAgentServer(srv, guest.NewServer())
	// Dormant unless IMP_SANDBOX_CONTROL_TOKEN is set in the VM environment.
	sandboxpb.RegisterSandboxControlServer(srv, guest.NewSandboxServer())
	if err := srv.Serve(l); err != nil {
		fmt.Fprintf(os.Stderr, "guest-agent: serve: %v\n", err)
		os.Exit(1)
	}
}
