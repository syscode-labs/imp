//go:build linux

package probe_test

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	impv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
	"github.com/syscode-labs/imp/internal/agent/probe"
	pb "github.com/syscode-labs/imp/internal/proto/guest"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type fakeGuestAgent struct {
	pb.UnimplementedGuestAgentServer
	execExitCode int32
	httpStatus   int32
}

func (f *fakeGuestAgent) Exec(_ context.Context, _ *pb.ExecRequest) (*pb.ExecResponse, error) {
	return &pb.ExecResponse{ExitCode: f.execExitCode}, nil
}
func (f *fakeGuestAgent) HTTPCheck(_ context.Context, _ *pb.HTTPCheckRequest) (*pb.HTTPCheckResponse, error) {
	return &pb.HTTPCheckResponse{StatusCode: f.httpStatus}, nil
}

func startFakeAgent(t *testing.T, agent *fakeGuestAgent) pb.GuestAgentClient {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := grpc.NewServer()
	pb.RegisterGuestAgentServer(srv, agent)
	go srv.Serve(lis) //nolint:errcheck
	t.Cleanup(srv.GracefulStop)
	conn, err := grpc.NewClient(lis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() }) //nolint:errcheck
	return pb.NewGuestAgentClient(conn)
}

func TestRunner_readinessProbe_pass(t *testing.T) {
	client := startFakeAgent(t, &fakeGuestAgent{execExitCode: 0})

	probeSpec := &impv1alpha1.ProbeSpec{
		ReadinessProbe: &impv1alpha1.Probe{
			Exec:             &impv1alpha1.ExecAction{Command: []string{"true"}},
			PeriodSeconds:    1,
			FailureThreshold: 3,
		},
	}
	conditions := make(chan []metav1.Condition, 1)
	r := probe.NewRunner(client, probeSpec, func(conds []metav1.Condition) {
		select {
		case conditions <- conds:
		default:
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go r.Run(ctx)

	select {
	case conds := <-conditions:
		found := false
		for _, c := range conds {
			if c.Type == "Ready" && c.Status == metav1.ConditionTrue {
				found = true
			}
		}
		if !found {
			t.Errorf("expected Ready=True condition, got %v", conds)
		}
	case <-ctx.Done():
		t.Fatal("timeout waiting for probe condition")
	}
}

func TestRunner_readinessProbe_fail(t *testing.T) {
	client := startFakeAgent(t, &fakeGuestAgent{execExitCode: 1})

	probeSpec := &impv1alpha1.ProbeSpec{
		ReadinessProbe: &impv1alpha1.Probe{
			Exec:             &impv1alpha1.ExecAction{Command: []string{"false"}},
			PeriodSeconds:    1,
			FailureThreshold: 1,
		},
	}
	conditions := make(chan []metav1.Condition, 1)
	r := probe.NewRunner(client, probeSpec, func(conds []metav1.Condition) {
		select {
		case conditions <- conds:
		default:
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go r.Run(ctx)

	select {
	case conds := <-conditions:
		found := false
		for _, c := range conds {
			if c.Type == "Ready" && c.Status == metav1.ConditionFalse {
				found = true
			}
		}
		if !found {
			t.Errorf("expected Ready=False condition, got %v", conds)
		}
	case <-ctx.Done():
		t.Fatal("timeout waiting for probe condition")
	}
}

func TestRunner_httpProbe_pass(t *testing.T) {
	client := startFakeAgent(t, &fakeGuestAgent{httpStatus: 200})

	probeSpec := &impv1alpha1.ProbeSpec{
		ReadinessProbe: &impv1alpha1.Probe{
			HTTP: &impv1alpha1.HTTPGetAction{
				Path: "/healthz",
				Port: 8080,
			},
			PeriodSeconds:    1,
			FailureThreshold: 3,
		},
	}
	conditions := make(chan []metav1.Condition, 1)
	r := probe.NewRunner(client, probeSpec, func(conds []metav1.Condition) {
		select {
		case conditions <- conds:
		default:
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go r.Run(ctx)

	select {
	case conds := <-conditions:
		found := false
		for _, c := range conds {
			if c.Type == "Ready" && c.Status == metav1.ConditionTrue {
				found = true
			}
		}
		if !found {
			t.Errorf("expected Ready=True condition from HTTP probe, got %v", conds)
		}
	case <-ctx.Done():
		t.Fatal("timeout waiting for HTTP probe condition")
	}
}

func TestRunner_nilSpec(t *testing.T) {
	client := startFakeAgent(t, &fakeGuestAgent{})

	r := probe.NewRunner(client, nil, func(_ []metav1.Condition) {
		t.Error("patcher called unexpectedly for nil spec")
	})
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	// Run should return immediately for nil spec
	r.Run(ctx)
}

func TestRunner_livenessProbe_pass(t *testing.T) {
	client := startFakeAgent(t, &fakeGuestAgent{execExitCode: 0})

	probeSpec := &impv1alpha1.ProbeSpec{
		LivenessProbe: &impv1alpha1.Probe{
			Exec:             &impv1alpha1.ExecAction{Command: []string{"true"}},
			PeriodSeconds:    1,
			FailureThreshold: 3,
		},
	}
	conditions := make(chan []metav1.Condition, 1)
	r := probe.NewRunner(client, probeSpec, func(conds []metav1.Condition) {
		select {
		case conditions <- conds:
		default:
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go r.Run(ctx)

	select {
	case conds := <-conditions:
		found := false
		for _, c := range conds {
			if c.Type == "Healthy" && c.Status == metav1.ConditionTrue {
				found = true
			}
		}
		if !found {
			t.Errorf("expected Healthy=True condition, got %v", conds)
		}
	case <-ctx.Done():
		t.Fatal("timeout waiting for liveness probe condition")
	}
}
