package rootfs

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// WithEnv writes a shell env file at /.imp/env that exports each key/value.
// The file is sourced by init wrappers before launching guest-agent and PID 1.
func WithEnv(values map[string]string) BuildOption {
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, k := range keys {
		fmt.Fprintf(h, "%s=%s\n", k, values[k]) //nolint:errcheck
	}
	fingerprint := "env-" + hex.EncodeToString(h.Sum(nil)[:8])

	return buildOption{
		key: fingerprint,
		apply: func(tmpDir string) error {
			if len(values) == 0 {
				return nil
			}
			impDir := filepath.Join(tmpDir, ".imp")
			if err := os.MkdirAll(impDir, 0o755); err != nil { //nolint:gosec // rootfs dir must be world-executable for VM init
				return err
			}

			var content strings.Builder
			content.WriteString("#!/bin/sh\n")
			for _, k := range keys {
				fmt.Fprintf(&content, "export %s=%q\n", k, values[k])
			}

			if err := os.WriteFile(filepath.Join(impDir, "env"), []byte(content.String()), 0o644); err != nil { //nolint:gosec // env file must be readable by init wrappers
				return fmt.Errorf("write env: %w", err)
			}
			return nil
		},
	}
}
