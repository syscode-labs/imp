package main

import (
	"flag"
	"os"

	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
	"github.com/syscode-labs/imp/internal/agent"
)

func main() {
	opts := zap.Options{Development: true}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	log := ctrl.Log.WithName("agent")

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

	// IMP_STUB_DRIVER=true: StubDriver (CI, test clusters, no KVM needed).
	// Otherwise: FirecrackerDriver (reads FC_BIN, FC_SOCK_DIR, FC_KERNEL env vars).
	var driver agent.VMDriver
	if os.Getenv("IMP_STUB_DRIVER") == "true" {
		log.Info("Using StubDriver (IMP_STUB_DRIVER=true)")
		driver = agent.NewStubDriver()
	} else {
		fc, err := newProductionDriver(mgr.GetClient())
		if err != nil {
			log.Error(err, "Unable to create FirecrackerDriver — set FC_KERNEL and ensure FC_BIN is in PATH")
			os.Exit(1)
		}
		log.Info("Using FirecrackerDriver")
		driver = fc
	}

	if err := (&agent.ImpVMReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		NodeName: nodeName,
		Driver:   driver,
	}).SetupWithManager(mgr); err != nil {
		log.Error(err, "Unable to set up ImpVMReconciler")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		log.Error(err, "Unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		log.Error(err, "Unable to set up ready check")
		os.Exit(1)
	}

	log.Info("Agent starting", "node", nodeName)
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		log.Error(err, "Problem running agent manager")
		os.Exit(1)
	}
}
