/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Command sandbox is the optional Imp Sandbox add-on controller manager.
// It reconciles ImpSandbox objects into base imp resources and serves the
// sandbox admission webhooks. Installing this binary is what activates the
// add-on; base imp deployments never run it.
package main

import (
	"context"
	"errors"
	"flag"
	"os"
	"path/filepath"

	_ "k8s.io/client-go/plugin/pkg/client/auth"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	sandboxv1alpha1 "github.com/syscode-labs/imp/api/sandbox/v1alpha1"
	impv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
	"github.com/syscode-labs/imp/internal/cnidetect"
	sandboxcontroller "github.com/syscode-labs/imp/internal/controller/sandbox"
	sandboxwebhook "github.com/syscode-labs/imp/internal/webhook/sandboxv1alpha1"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("sandbox-setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(impv1alpha1.AddToScheme(scheme))
	utilruntime.Must(sandboxv1alpha1.AddToScheme(scheme))
}

// cniDetectRunnable runs CNI detection once after the manager cache syncs so
// tenancy admission can verify Cilium capability without per-request discovery
// calls. It reads through the API reader (uncached) on purpose: a cached
// DaemonSet informer would need cluster-wide list RBAC, and any forbidden
// informer poisons the manager's shared cache-sync gate.
type cniDetectRunnable struct {
	reader   client.Reader
	recorder record.EventRecorder
	store    *cnidetect.Store
	mapper   meta.RESTMapper
}

func (r *cniDetectRunnable) Start(ctx context.Context) error {
	result, err := cnidetect.Detect(ctx, r.reader, r.mapper)
	if err != nil {
		return err
	}
	r.store.Set(result)

	cfg := &impv1alpha1.ClusterImpConfig{}
	if getErr := r.reader.Get(ctx, client.ObjectKey{Name: "cluster"}, cfg); getErr == nil {
		r.recorder.Eventf(cfg, corev1.EventTypeNormal,
			"SandboxCNIDetected",
			"CNI detected: provider=%s", result.Provider)
	}
	setupLog.Info("CNI detection complete", "provider", result.Provider)
	return nil
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var enableWebhooks bool
	var probeAddr string
	var sessionHMACKeyFile string

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8090", "The address the metrics endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8091", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", true, "Enable leader election for controller manager.")
	flag.BoolVar(&enableWebhooks, "enable-webhooks", true, "Enable admission webhooks.")
	flag.StringVar(&sessionHMACKeyFile, "session-hmac-key-file", "", "File containing the cluster HMAC key used to mint sandbox session tokens. Empty disables session-secret minting.")

	opts := zap.Options{Development: true}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	cfg := ctrl.GetConfigOrDie()
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsserver.Options{BindAddress: metricsAddr},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "sandbox.imp.dev",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	cniStore := &cnidetect.Store{}

	// Session-secret minting is enabled only when the cluster HMAC key is
	// provisioned (mounted from the gateway-hmac Secret by the chart).
	var sessionKey []byte
	if sessionHMACKeyFile != "" {
		key, err := os.ReadFile(filepath.Clean(sessionHMACKeyFile))
		if err != nil {
			setupLog.Error(err, "unable to read session hmac key", "file", sessionHMACKeyFile)
			os.Exit(1)
		}
		if len(key) < 32 {
			setupLog.Error(errors.New("session hmac key too short"), "need at least 32 bytes")
			os.Exit(1)
		}
		sessionKey = key
	}

	if err = (&sandboxcontroller.ImpSandboxReconciler{
		Client:         mgr.GetClient(),
		Scheme:         mgr.GetScheme(),
		Recorder:       mgr.GetEventRecorderFor("impsandbox-controller"), //nolint:staticcheck
		CNIStore:       cniStore,
		SessionHMACKey: sessionKey,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ImpSandbox")
		os.Exit(1)
	}

	if enableWebhooks {
		if err = builder.WebhookManagedBy(mgr, &sandboxv1alpha1.ImpSandbox{}).
			WithValidator(&sandboxwebhook.ImpSandboxWebhook{
				Client:   mgr.GetClient(),
				CNIStore: cniStore,
			}).
			Complete(); err != nil {
			setupLog.Error(err, "unable to register webhook", "webhook", "ImpSandbox")
			os.Exit(1)
		}
	} else {
		setupLog.Info("admission webhooks disabled; tenancy enforcement remains in the reconciler")
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	if err := mgr.Add(&cniDetectRunnable{
		reader:   mgr.GetAPIReader(),
		mapper:   mgr.GetRESTMapper(),
		recorder: mgr.GetEventRecorderFor("sandbox-cni-detector"), //nolint:staticcheck
		store:    cniStore,
	}); err != nil {
		setupLog.Error(err, "unable to register cni-detect runnable")
		os.Exit(1)
	}

	setupLog.Info("starting sandbox manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
