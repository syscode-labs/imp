package rootfs

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestUntar_PreservesAbsoluteSymlinkWithinRootfs(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	entries := []struct {
		hdr  *tar.Header
		body string
	}{
		{
			hdr: &tar.Header{Name: "bin", Typeflag: tar.TypeDir, Mode: 0o755},
		},
		{
			hdr:  &tar.Header{Name: "bin/busybox", Typeflag: tar.TypeReg, Mode: 0o755, Size: int64(len("busybox"))},
			body: "busybox",
		},
		{
			hdr: &tar.Header{Name: "bin/sh", Typeflag: tar.TypeSymlink, Linkname: "/bin/busybox"},
		},
	}

	for _, e := range entries {
		if err := tw.WriteHeader(e.hdr); err != nil {
			t.Fatalf("write header %s: %v", e.hdr.Name, err)
		}
		if e.body != "" {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatalf("write body %s: %v", e.hdr.Name, err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}

	outDir := t.TempDir()
	if err := untar(outDir, bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatalf("untar: %v", err)
	}

	linkPath := filepath.Join(outDir, "bin", "sh")
	fi, err := os.Lstat(linkPath)
	if err != nil {
		t.Fatalf("lstat link: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("%s is not a symlink", linkPath)
	}
	target, err := os.Readlink(linkPath)
	if err != nil {
		t.Fatalf("readlink: %v", err)
	}
	if target != "/bin/busybox" {
		t.Fatalf("symlink target = %q, want %q", target, "/bin/busybox")
	}
}

func TestUntar_PreservesHardlinkWithinRootfs(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	entries := []struct {
		hdr  *tar.Header
		body string
	}{
		{
			hdr: &tar.Header{Name: "bin", Typeflag: tar.TypeDir, Mode: 0o755},
		},
		{
			hdr:  &tar.Header{Name: "bin/busybox", Typeflag: tar.TypeReg, Mode: 0o755, Size: int64(len("busybox"))},
			body: "busybox",
		},
		{
			hdr: &tar.Header{Name: "bin/sh", Typeflag: tar.TypeLink, Linkname: "bin/busybox"},
		},
	}

	for _, e := range entries {
		if err := tw.WriteHeader(e.hdr); err != nil {
			t.Fatalf("write header %s: %v", e.hdr.Name, err)
		}
		if e.body != "" {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatalf("write body %s: %v", e.hdr.Name, err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}

	outDir := t.TempDir()
	if err := untar(outDir, bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatalf("untar: %v", err)
	}

	srcInfo, err := os.Stat(filepath.Join(outDir, "bin", "busybox"))
	if err != nil {
		t.Fatalf("stat busybox: %v", err)
	}
	linkInfo, err := os.Stat(filepath.Join(outDir, "bin", "sh"))
	if err != nil {
		t.Fatalf("stat hardlink sh: %v", err)
	}
	if !os.SameFile(srcInfo, linkInfo) {
		t.Fatal("bin/sh is not a hardlink to bin/busybox")
	}
}
