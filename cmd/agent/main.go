package main

import (
	"context"
	"flag"
	"os"

	_ "k8s.io/client-go/plugin/pkg/client/auth"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/syscode-labs/imp/internal/agent"
)

func main() {
	opts := zap.Options{Development: true}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	log := ctrl.Log.WithName("main")

	nodeName := os.Getenv("NODE_NAME")
	if nodeName == "" {
		log.Error(nil, "NODE_NAME env var not set — run as DaemonSet with fieldRef downward API")
		os.Exit(1)
	}

	a := &agent.Agent{NodeName: nodeName}

	ctx := ctrl.SetupSignalHandler()
	if err := a.Run(ctx); err != nil && err != context.Canceled {
		log.Error(err, "agent exited with error")
		os.Exit(1)
	}
}
