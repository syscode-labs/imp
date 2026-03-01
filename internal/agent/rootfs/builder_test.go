// internal/agent/rootfs/builder_test.go
package rootfs

import (
	"archive/tar"
	"bytes"
	"context"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
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

func TestCachePath(t *testing.T) {
	b := &Builder{CacheDir: "/var/lib/imp/images"}
	got := b.cachePath("abc123")
	want := "/var/lib/imp/images/abc123.ext4"
	if got != want {
		t.Errorf("cachePath = %q, want %q", got, want)
	}
}

func TestBuild_CacheHit(t *testing.T) {
	b := newBuilder(t)

	// Pre-populate the cache with a fake .ext4 file.
	fakeDigest := "deadbeef1234"
	cachedPath := b.cachePath(fakeDigest)
	if err := os.MkdirAll(b.CacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachedPath, []byte("fake ext4"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Build should return the cached path without pulling anything.
	// We use a non-existent registry to confirm no network call is made.
	// (This will only work once cache-hit detection is implemented before any fetch.)
	// For now, just test cachePath returns the right value.
	got := b.cachePath(fakeDigest)
	if got != cachedPath {
		t.Errorf("cachePath = %q, want %q", got, cachedPath)
	}
	if _, err := os.Stat(got); err != nil {
		t.Errorf("cache file not found: %v", err)
	}
}

func TestPullImage(t *testing.T) {
	_, addr := startRegistry(t)
	img := makeImage(t, []string{"/bin/app"}, map[string]string{"/bin/app": "#!/bin/sh\necho hello"})
	ref := pushImage(t, addr, img)

	b := newBuilder(t)
	pulled, err := b.pullImage(context.Background(), ref)
	if err != nil {
		t.Fatalf("pullImage: %v", err)
	}
	digest, err := pulled.Digest()
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}
	if digest.Hex == "" {
		t.Error("expected non-empty digest hex")
	}
}

func TestExtractLayers(t *testing.T) {
	_, addr := startRegistry(t)
	img := makeImage(t, nil, map[string]string{
		"/etc/hostname": "my-vm\n",
		"/bin/hello":    "#!/bin/sh\necho hello",
	})
	ref := pushImage(t, addr, img)

	b := newBuilder(t)
	pulled, err := b.pullImage(context.Background(), ref)
	if err != nil {
		t.Fatalf("pullImage: %v", err)
	}

	dir := t.TempDir()
	if err := extractLayers(pulled, dir); err != nil {
		t.Fatalf("extractLayers: %v", err)
	}

	// Verify files exist in the extracted directory.
	for _, want := range []string{"etc/hostname", "bin/hello"} {
		path := filepath.Join(dir, want)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected %s to exist: %v", want, err)
		}
	}
}

func TestBuildExt4(t *testing.T) {
	if !hasMke2fs() {
		t.Skip("mke2fs/mkfs.ext4 not in PATH — skipping ext4 assembly test")
	}

	// Create a temp dir with a few files to pack into ext4.
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "hello.txt"), []byte("hello from ext4"), 0o644); err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(t.TempDir(), "test.ext4")
	if err := buildExt4(context.Background(), src, dest, 64); err != nil {
		t.Fatalf("buildExt4: %v", err)
	}

	info, err := os.Stat(dest)
	if err != nil {
		t.Fatalf("stat ext4: %v", err)
	}
	if info.Size() == 0 {
		t.Error("ext4 file is empty")
	}
}

func TestBuild_FullPipeline(t *testing.T) {
	if !hasMke2fs() {
		t.Skip("mke2fs/mkfs.ext4 not in PATH — skipping full pipeline test")
	}

	_, addr := startRegistry(t)
	img := makeImage(t, []string{"/bin/app"}, map[string]string{
		"/bin/app": "#!/bin/sh\necho hello from vm",
	})
	ref := pushImage(t, addr, img)

	b := newBuilder(t)
	path, err := b.Build(context.Background(), ref)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// Verify the .ext4 file exists in the cache dir.
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("output file not found: %v", err)
	}
	if filepath.Dir(path) != b.CacheDir {
		t.Errorf("output path %q is not inside CacheDir %q", path, b.CacheDir)
	}
	if filepath.Ext(path) != ".ext4" {
		t.Errorf("output path %q should end in .ext4", path)
	}
}

func TestBuild_CacheHitSkipsNetwork(t *testing.T) {
	if !hasMke2fs() {
		t.Skip("mke2fs/mkfs.ext4 not in PATH")
	}

	_, addr := startRegistry(t)
	img := makeImage(t, []string{"/bin/app"}, map[string]string{"/bin/app": "#!/bin/sh"})
	ref := pushImage(t, addr, img)

	b := newBuilder(t)
	path1, err := b.Build(context.Background(), ref)
	if err != nil {
		t.Fatalf("first Build: %v", err)
	}

	// Second call should return the same path (cache hit).
	path2, err := b.Build(context.Background(), ref)
	if err != nil {
		t.Fatalf("second Build: %v", err)
	}
	if path1 != path2 {
		t.Errorf("cache hit returned different path: %q vs %q", path1, path2)
	}
}

func TestWriteInit(t *testing.T) {
	tests := []struct {
		name       string
		entrypoint []string
		cmd        []string
		wantInit   bool
		wantScript string
	}{
		{
			name:       "cmd only",
			cmd:        []string{"/bin/app", "--port", "8080"},
			wantInit:   true,
			wantScript: "exec \"/bin/app\" \"--port\" \"8080\"",
		},
		{
			name:       "entrypoint + cmd",
			entrypoint: []string{"/tini", "--"},
			cmd:        []string{"/bin/app"},
			wantInit:   true,
			wantScript: "exec \"/tini\" \"--\" \"/bin/app\"",
		},
		{
			name:     "no cmd or entrypoint",
			wantInit: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			img := makeImage(t, tc.cmd, nil)
			if len(tc.entrypoint) > 0 {
				cf, _ := img.ConfigFile()
				cf = cf.DeepCopy()
				cf.Config.Entrypoint = tc.entrypoint
				img, _ = mutate.ConfigFile(img, cf)
			}

			dir := t.TempDir()
			if err := writeInit(img, dir); err != nil {
				t.Fatalf("writeInit: %v", err)
			}

			initPath := filepath.Join(dir, "sbin", "init")
			_, err := os.Stat(initPath)
			exists := err == nil

			if exists != tc.wantInit {
				t.Errorf("init exists = %v, want %v", exists, tc.wantInit)
			}
			if tc.wantInit {
				content, _ := os.ReadFile(initPath)
				if !strings.Contains(string(content), tc.wantScript) {
					t.Errorf("init script = %q, want to contain %q", content, tc.wantScript)
				}
			}
		})
	}
}
