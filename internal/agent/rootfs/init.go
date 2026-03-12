// internal/agent/rootfs/init.go
package rootfs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	v1 "github.com/google/go-containerregistry/pkg/v1"
)

// writeInit writes a /sbin/init shell wrapper that execs the OCI CMD/ENTRYPOINT.
// If the image has no CMD or ENTRYPOINT, the file is not written (the rootfs
// must already contain a working /sbin/init).
func writeInit(img v1.Image, dir string) error {
	cfg, err := img.ConfigFile()
	if err != nil {
		return fmt.Errorf("config file: %w", err)
	}

	args := make([]string, 0, len(cfg.Config.Entrypoint)+len(cfg.Config.Cmd))
	args = append(args, cfg.Config.Entrypoint...)
	args = append(args, cfg.Config.Cmd...)
	if len(args) == 0 {
		return nil // no init to write
	}

	// Shell-quote each argument.
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = fmt.Sprintf("%q", a)
	}

	script := "#!/bin/sh\n[ -f /.imp/env ] && . /.imp/env\nexec " + strings.Join(quoted, " ") + " \"$@\"\n"

	initPath := filepath.Join(dir, "sbin", "init")
	if err := os.MkdirAll(filepath.Dir(initPath), 0o755); err != nil { //nolint:gosec // G301: sbin directory must be world-traversable in the VM rootfs
		return fmt.Errorf("mkdir sbin: %w", err)
	}
	if err := os.WriteFile(initPath, []byte(script), 0o755); err != nil { //nolint:gosec // G306: init script must be executable
		return fmt.Errorf("write init: %w", err)
	}
	return nil
}
