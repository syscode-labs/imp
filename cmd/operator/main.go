package main

import (
	"context"
	"flag"
	"os"
	"strings"

	"github.com/go-logr/logr"
	// Import all Kubernetes client auth plugins (e.g. OIDC).
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/discovery"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	impv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
	"github.com/syscode-labs/imp/internal/cnidetect"
	"github.com/syscode-labs/imp/internal/controller"
	webhookv1alpha1 "github.com/syscode-labs/imp/internal/webhook/v1alpha1"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(impv1alpha1.AddToScheme(scheme))
}

// logCiliumPresence checks for key Cilium CRDs and logs their presence at startup.
// This is informational only — it does not affect operator behaviour.
func logCiliumPresence(cfg *rest.Config, log logr.Logger) {
	dc, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		log.V(1).Info("could not create discovery client for Cilium check", "err", err)
		return
	}
	ciliumGroups := []string{"cilium.io"}
	_, lists, err := dc.ServerGroupsAndResources()
	if err != nil && !discovery.IsGroupDiscoveryFailedError(err) {
		log.V(1).Info("could not query API groups for Cilium check", "err", err)
		return
	}
	found := []string{}
	for _, list := range lists {
		for _, g := range ciliumGroups {
			if strings.Contains(list.GroupVersion, g) {
				for _, r := range list.APIResources {
					found = append(found, list.GroupVersion+"/"+r.Name)
				}
			}
		}
	}
	if len(found) > 0 {
		log.Info("Cilium CRDs detected at startup", "resources", found)
	} else {
		log.V(1).Info("no Cilium CRDs detected")
	}
}

// cniDetectRunnable runs CNI detection once after the manager cache is synced,
// stores the result, and emits an event on the ClusterImpConfig singleton.
type cniDetectRunnable struct {
	client   client.Client
	recorder record.EventRecorder
	store    *cnidetect.Store
}

func (r *cniDetectRunnable) Start(ctx context.Context) error {
	log := ctrl.Log.WithName("cni-detect")

	result, err := cnidetect.Detect(ctx, r.client)
	if err != nil {
		return err
	}
	r.store.Set(result)

	// Emit event on the ClusterImpConfig singleton (best-effort; skip if absent).
	cfg := &impv1alpha1.ClusterImpConfig{}
	if getErr := r.client.Get(ctx, client.ObjectKey{Name: "cluster"}, cfg); getErr == nil {
		if result.Ambiguous {
			r.recorder.Event(cfg, corev1.EventTypeWarning,
				controller.EventReasonCNIAmbiguous,
				"Multiple CNIs detected; using iptables fallback. Set spec.networking.cni.provider explicitly.")
		} else {
			r.recorder.Eventf(cfg, corev1.EventTypeNormal,
				controller.EventReasonCNIDetected,
				"CNI detected: provider=%s natBackend=%s", result.Provider, result.NATBackend)
		}
	}

	log.Info("CNI detection complete",
		"provider", result.Provider,
		"natBackend", result.NATBackend,
		"ambiguous", result.Ambiguous)
	return nil
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metrics endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false, "Enable leader election for controller manager.")

	opts := zap.Options{Development: true}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	cfg := ctrl.GetConfigOrDie()
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: metricsAddr,
		},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "imp.dev",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	logCiliumPresence(cfg, setupLog)

	cniStore := &cnidetect.Store{}

	if err = (&controller.ImpVMReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("impvm-controller"), //nolint:staticcheck
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ImpVM")
		os.Exit(1)
	}

	if err = (&controller.ImpNetworkReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("impnetwork-controller"), //nolint:staticcheck
		CNIStore: cniStore,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ImpNetwork")
		os.Exit(1)
	}

	if err = (&controller.ImpVMSnapshotReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ImpVMSnapshot")
		os.Exit(1)
	}

	if err = (&controller.ImpVMMigrationReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ImpVMMigration")
		os.Exit(1)
	}

	if err = (&controller.ImpWarmPoolReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ImpWarmPool")
		os.Exit(1)
	}

	if err = builder.WebhookManagedBy(mgr, &impv1alpha1.ImpVM{}).
		WithDefaulter(&webhookv1alpha1.ImpVMWebhook{}).
		WithValidator(&webhookv1alpha1.ImpVMWebhook{}).
		Complete(); err != nil {
		setupLog.Error(err, "unable to register webhook", "webhook", "ImpVM")
		os.Exit(1)
	}

	if err = builder.WebhookManagedBy(mgr, &impv1alpha1.ImpVMClass{}).
		WithValidator(&webhookv1alpha1.ImpVMClassWebhook{}).
		Complete(); err != nil {
		setupLog.Error(err, "unable to register webhook", "webhook", "ImpVMClass")
		os.Exit(1)
	}

	if err = builder.WebhookManagedBy(mgr, &impv1alpha1.ImpVMTemplate{}).
		WithValidator(&webhookv1alpha1.ImpVMTemplateWebhook{}).
		Complete(); err != nil {
		setupLog.Error(err, "unable to register webhook", "webhook", "ImpVMTemplate")
		os.Exit(1)
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
		client:   mgr.GetClient(),
		recorder: mgr.GetEventRecorderFor("cni-detector"), //nolint:staticcheck
		store:    cniStore,
	}); err != nil {
		setupLog.Error(err, "unable to register cni-detect runnable")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
