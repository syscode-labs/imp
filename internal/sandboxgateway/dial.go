package sandboxgateway

import (
	"path/filepath"
	"strings"
)

// SocketDirDefault mirrors agent.hostPaths.socketDir.path in the base chart.
// The gateway DaemonSet mounts the same hostPath read-only.
const SocketDirDefault = "/run/imp"

// VSOCKPath returns the guest-agent VSOCK proxy unix socket for a sandbox's
// backing VM, following the agent's convention
// (<SocketDir>/<ns>-<name>.sock → same file with .vsock suffix).
func VSOCKPath(socketDir, namespace, vmName string) string {
	base := namespace + "-" + vmName + ".sock"
	vsock := strings.TrimSuffix(base, ".sock") + ".vsock"
	return filepath.Join(socketDir, vsock)
}
