//go:build linux

package agent

import (
	"context"
	"os"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	impv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

// detectAndPatchCPUModel reads /proc/cpuinfo, extracts the CPU model string,
// and patches it onto the ClusterImpNodeProfile for this node.
// This is best-effort — errors are logged at V(1) and do not affect agent startup.
func detectAndPatchCPUModel(ctx context.Context, c client.Client, nodeName string) {
	log := logf.FromContext(ctx)
	data, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		log.V(1).Info("could not read /proc/cpuinfo", "err", err)
		return
	}
	model := parseCPUModelFromProcInfo(string(data))
	if model == "" {
		log.V(1).Info("could not parse CPU model from /proc/cpuinfo")
		return
	}

	profile := &impv1alpha1.ClusterImpNodeProfile{}
	err = c.Get(ctx, client.ObjectKey{Name: nodeName}, profile)
	if apierrors.IsNotFound(err) {
		log.V(1).Info("no ClusterImpNodeProfile for node, skipping CPU model patch", "node", nodeName)
		return
	}
	if err != nil {
		log.Error(err, "failed to get ClusterImpNodeProfile for CPU model patch", "node", nodeName)
		return
	}

	if profile.Spec.CPUModel == model {
		return // already up to date
	}

	base := profile.DeepCopy()
	profile.Spec.CPUModel = model
	if err := c.Patch(ctx, profile, client.MergeFrom(base)); err != nil {
		log.Error(err, "failed to patch CPUModel onto ClusterImpNodeProfile", "node", nodeName)
		return
	}
	log.Info("patched CPU model onto ClusterImpNodeProfile", "node", nodeName, "model", model)
}

// parseCPUModelFromProcInfo extracts the "model name" line from /proc/cpuinfo content.
func parseCPUModelFromProcInfo(content string) string {
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "model name") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return ""
}
