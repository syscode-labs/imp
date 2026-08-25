//go:build linux

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"

	"github.com/syscode-labs/imp/internal/agent"
	"github.com/syscode-labs/imp/internal/agent/api"
	"github.com/syscode-labs/imp/internal/agent/network"
	"github.com/syscode-labs/imp/internal/capability"
	"github.com/syscode-labs/imp/internal/telemetry"
)

// capabilityProbe runs the standalone KVM/Firecracker capability probe and
// exits: 0 when the host passes, 1 otherwise. Used by the Helm KVM preflight
// hook Job so it can reuse the agent image without starting the manager.
var capabilityProbe = flag.Bool("capability-probe", false, "run the KVM/Firecracker capability probe and exit")

func main() {
	opts := zap.Options{Development: true}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	log := ctrl.Log.WithName("agent")

	if *capabilityProbe {
		r := capability.CheckDefault(os.Getenv("FC_BIN"))
		if !r.OK() {
			fmt.Fprintf(os.Stderr, "capability probe failed: kvm_available=%v (%s) firecracker_available=%v (%s)\n",
				r.KVMAvailable, r.KVMError, r.FirecrackerAvailable, r.FirecrackerError)
			os.Exit(1)
		}
		fmt.Println("capability probe passed")
		os.Exit(0)
	}

	nodeName := os.Getenv("NODE_NAME")
	if nodeName == "" {
		log.Error(nil, "NODE_NAME env var not set — run as DaemonSet with fieldRef downward API")
		os.Exit(1)
	}

	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		log.Error(err, "Unable to add client-go scheme")
		os.Exit(1)
	}
	if err := impdevv1alpha1.AddToScheme(scheme); err != nil {
		log.Error(err, "Unable to add imp scheme")
		os.Exit(1)
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		HealthProbeBindAddress: ":8081",
		LeaderElection:         false, // DaemonSet: one instance per node, no election needed.
	})
	if err != nil {
		log.Error(err, "Unable to start manager")
		os.Exit(1)
	}
	nodeIP, err := resolveVTEPIP(context.Background(), mgr.GetClient(), nodeName)
	if err != nil {
		log.Error(err, "Unable to resolve VTEP underlay address, cross-node VXLAN disabled", "node", nodeName)
		nodeIP = ""
	}
	if nodeIP == "" {
		log.Info("Cross-node VXLAN disabled; configure ClusterImpNodeProfile.spec.vtepIP", "node", nodeName)
	}

	agentReg := prometheus.NewRegistry()
	agentReg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	mp, shutdownTelemetry, err := telemetry.SetupMeterProvider(context.Background(), agentReg)
	if err != nil {
		log.Error(err, "unable to set up telemetry")
		os.Exit(1)
	}
	defer func() { _ = shutdownTelemetry(context.Background()) }()
	_, shutdownTraces, err := telemetry.SetupTracerProvider(context.Background())
	if err != nil {
		log.Error(err, "unable to set up traces")
		os.Exit(1)
	}
	defer func() { _ = shutdownTraces(context.Background()) }()
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	mc := agent.NewVMMetricsCollector(mp.Meter("imp.agent"), agentReg)

	// IMP_STUB_DRIVER=true: StubDriver (CI, test clusters, no KVM needed).
	// Otherwise: FirecrackerDriver (reads FC_BIN, FC_SOCK_DIR, FC_KERNEL env vars).
	var driver agent.VMDriver
	var prodNet network.NetManager
	if os.Getenv("IMP_STUB_DRIVER") == "true" {
		log.Info("Using StubDriver (IMP_STUB_DRIVER=true)")
		driver = agent.NewStubDriver()
	} else {
		var fcErr error
		driver, prodNet, fcErr = newProductionDriver(mgr.GetClient(), mc, nodeName)
		if fcErr != nil {
			log.Error(fcErr, "Unable to create FirecrackerDriver — set FC_KERNEL and ensure FC_BIN is in PATH")
			os.Exit(1)
		}
		log.Info("Using FirecrackerDriver")
	}

	// Scale-to-zero (wake-on-traffic) is experimental and its capture hook is not
	// yet hardware-validated — opt in explicitly via IMP_SCALE_TO_ZERO=true.
	var sz *agent.ScaleToZero
	if os.Getenv("IMP_SCALE_TO_ZERO") == "true" {
		sz = agent.NewLinuxScaleToZero(1024, 15*time.Second)
		log.Info("scale-to-zero enabled (experimental; wake path not hardware-validated)")
	}

	if err := (&agent.ImpVMReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		NodeName: nodeName,
		NodeIP:   nodeIP,
		Driver:   driver,
		Metrics:  mc,
		Net:      prodNet,
		Alloc:    nil,
		SZ:       sz,
		Recorder: mgr.GetEventRecorderFor("imp-agent"), //nolint:staticcheck // controller-runtime returns legacy recorder type expected by reconciler
	}).SetupWithManager(mgr); err != nil {
		log.Error(err, "Unable to set up ImpVMReconciler")
		os.Exit(1)
	}

	if err := (&agent.ImpVMSnapshotReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		NodeName: nodeName,
		Driver:   driver,
	}).SetupWithManager(mgr); err != nil {
		log.Error(err, "unable to create controller", "controller", "ImpVMSnapshot")
		os.Exit(1)
	}

	if err := (&agent.ImpNetworkReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		NodeName: nodeName,
		NodeIP:   nodeIP,
		Net:      prodNet,
	}).SetupWithManager(mgr); err != nil {
		log.Error(err, "unable to create controller", "controller", "ImpNetwork(agent)")
		os.Exit(1)
	}

	// Register the HTTP API server when the runtime driver exposes VSOCK paths.
	if vsockDriver, ok := driver.(api.VSockDialer); ok {
		socketDir := os.Getenv("FC_SOCK_DIR")
		if socketDir == "" {
			socketDir = "/run/imp/sockets"
		}
		apiServer := &api.APIServer{
			SocketDir: socketDir,
			Driver:    vsockDriver,
		}
		if err := mgr.Add(apiServer); err != nil {
			log.Error(err, "Unable to add APIServer runnable")
			os.Exit(1)
		}
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		log.Error(err, "Unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		log.Error(err, "Unable to set up ready check")
		os.Exit(1)
	}

	log.Info("Agent starting", "node", nodeName, "vtepIP", nodeIP)
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		log.Error(err, "Problem running agent manager")
		os.Exit(1)
	}
}
