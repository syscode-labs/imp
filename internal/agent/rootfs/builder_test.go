// internal/agent/rootfs/builder_test.go
package rootfs

import (
	"archive/tar"
	"bytes"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
)

// startRegistry starts an in-memory OCI registry and returns the server + address prefix.
// The address prefix looks like "127.0.0.1:PORT" — prepend to image refs.
func startRegistry(t *testing.T) (srv *httptest.Server, addr string) {
	t.Helper()
	srv = httptest.NewServer(registry.New())
	t.Cleanup(srv.Close)
	addr = strings.TrimPrefix(srv.URL, "http://")
	return srv, addr
}

// makeImage builds a minimal OCI image with the given files and CMD.
// files is a map of path → content (e.g. "/bin/app" → "#!/bin/sh\necho hi").
func makeImage(t *testing.T, cmd []string, files map[string]string) v1.Image {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for name, content := range files {
		name = strings.TrimPrefix(name, "/")
		hdr := &tar.Header{
			Name: name,
			Mode: 0o755,
			Size: int64(len(content)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("tar header: %v", err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("tar write: %v", err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}

	layer, err := tarball.LayerFromReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("layer: %v", err)
	}
	img, err := mutate.AppendLayers(empty.Image, layer)
	if err != nil {
		t.Fatalf("append layer: %v", err)
	}
	cf, err := img.ConfigFile()
	if err != nil {
		t.Fatalf("config file: %v", err)
	}
	cf = cf.DeepCopy()
	cf.Config.Cmd = cmd
	img, err = mutate.ConfigFile(img, cf)
	if err != nil {
		t.Fatalf("set config: %v", err)
	}
	return img
}

// pushImage pushes img to the test registry and returns the full image reference.
func pushImage(t *testing.T, addr string, img v1.Image) string {
	t.Helper()
	ref := addr + "/test/img:latest"
	if err := crane.Push(img, ref, crane.Insecure); err != nil {
		t.Fatalf("push: %v", err)
	}
	return ref
}

// hasMke2fs returns true if mke2fs or mkfs.ext4 is available in PATH.
func hasMke2fs() bool {
	_, err1 := exec.LookPath("mke2fs")
	_, err2 := exec.LookPath("mkfs.ext4")
	return err1 == nil || err2 == nil
}

// newBuilder creates a Builder with a temp cache dir, cleaned up after the test.
func newBuilder(t *testing.T) *Builder {
	t.Helper()
	dir := t.TempDir()
	return &Builder{CacheDir: dir, Insecure: true}
}
