//go:build linux

package main

import (
	"flag"
	"net/http"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
	"github.com/syscode-labs/imp/internal/agent"
	"github.com/syscode-labs/imp/internal/agent/network"
	"github.com/syscode-labs/imp/internal/capability"
	"github.com/syscode-labs/imp/internal/noderuntime"
	"github.com/syscode-labs/imp/internal/runtimeapi"
)

func main() {
	opts := zap.Options{Development: true}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	log := ctrl.Log.WithName("runtime")

	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = impdevv1alpha1.AddToScheme(scheme)
	client, err := ctrlclient.New(ctrl.GetConfigOrDie(), ctrlclient.Options{Scheme: scheme})
	if err != nil {
		log.Error(err, "Unable to create Kubernetes client")
		os.Exit(1)
	}
	driver, err := agent.NewFirecrackerDriver(client)
	if err != nil {
		log.Error(err, "Unable to create FirecrackerDriver")
		os.Exit(1)
	}
	net := network.NewLinuxNetManager()
	driver.Net = net
	driver.NodeName = os.Getenv("NODE_NAME")
	endpoint := os.Getenv("IMP_RUNTIME_SOCKET")
	if endpoint == "" {
		endpoint = "/run/imp/runtime.sock"
	}
	statePath := os.Getenv("IMP_RUNTIME_STATE_PATH")
	if statePath == "" {
		statePath = "/var/lib/imp/runtime"
	}
	server := runtimeapi.NewServer(&noderuntime.Backend{Driver: driver, Net: net, StatePath: statePath})
	if err := server.Start(endpoint); err != nil {
		log.Error(err, "Unable to start runtime server")
		os.Exit(1)
	}
	defer server.Close() //nolint:errcheck
	mux := http.NewServeMux()
	registry := prometheus.NewRegistry()
	ready := prometheus.NewGauge(prometheus.GaugeOpts{Name: "imp_runtime_ready", Help: "Whether the Imp node runtime is ready to serve VM operations."})
	registry.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}), ready)
	ready.Set(1)
	mux.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		if _, err := os.Stat(os.Getenv("FC_KERNEL")); err != nil || !capability.CheckDefault(os.Getenv("FC_BIN")).OK() {
			http.Error(w, "runtime dependencies unavailable", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	health := &http.Server{Addr: ":8082", Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = health.ListenAndServe() }()
	defer health.Close() //nolint:errcheck
	log.Info("Runtime starting", "socket", endpoint, "node", driver.NodeName)
	<-ctrl.SetupSignalHandler().Done()
}
