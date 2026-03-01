//go:build linux

package agent

import (
	"os"
	"os/exec"
	"testing"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

// hasFirecrackerBin returns true if the firecracker binary is available.
func hasFirecrackerBin() bool {
	_, err := exec.LookPath("firecracker")
	return err == nil || os.Getenv("FC_BIN") != ""
}

// hasKVM returns true if /dev/kvm is accessible.
func hasKVM() bool {
	_, err := os.Stat("/dev/kvm")
	return err == nil
}

func TestFirecrackerDriverPlaceholder(t *testing.T) {
	t.Log("FirecrackerDriver test file compiles correctly")
}

func TestFirecrackerDriver_SocketPath(t *testing.T) {
	d := &FirecrackerDriver{SocketDir: "/run/imp/sockets"}

	vm := &impdevv1alpha1.ImpVM{}
	vm.Namespace = "default"
	vm.Name = "my-vm"

	got := d.socketPath(vm)
	want := "/run/imp/sockets/default-my-vm.sock"
	if got != want {
		t.Errorf("socketPath = %q, want %q", got, want)
	}
}

func TestFirecrackerDriver_BuildConfig(t *testing.T) {
	d := &FirecrackerDriver{
		KernelPath: "/boot/vmlinux",
		KernelArgs: "console=ttyS0 reboot=k panic=1 pci=off",
	}

	class := &impdevv1alpha1.ImpVMClass{}
	class.Spec.VCPU = 2
	class.Spec.MemoryMiB = 512

	cfg := d.buildConfig(class, "/cache/abc.ext4", "/run/imp/sockets/default-vm.sock")

	if cfg.SocketPath != "/run/imp/sockets/default-vm.sock" {
		t.Errorf("SocketPath = %q, want %q", cfg.SocketPath, "/run/imp/sockets/default-vm.sock")
	}
	if cfg.KernelImagePath != "/boot/vmlinux" {
		t.Errorf("KernelImagePath = %q, want %q", cfg.KernelImagePath, "/boot/vmlinux")
	}
	if cfg.KernelArgs != "console=ttyS0 reboot=k panic=1 pci=off" {
		t.Errorf("KernelArgs = %q, want %q", cfg.KernelArgs, "console=ttyS0 reboot=k panic=1 pci=off")
	}

	if len(cfg.Drives) != 1 {
		t.Fatalf("len(cfg.Drives) = %d, want 1", len(cfg.Drives))
	}
	if *cfg.Drives[0].PathOnHost != "/cache/abc.ext4" {
		t.Errorf("Drives[0].PathOnHost = %q, want %q", *cfg.Drives[0].PathOnHost, "/cache/abc.ext4")
	}
	if *cfg.Drives[0].IsRootDevice != true {
		t.Errorf("Drives[0].IsRootDevice = %v, want true", *cfg.Drives[0].IsRootDevice)
	}

	if *cfg.MachineCfg.VcpuCount != 2 {
		t.Errorf("MachineCfg.VcpuCount = %d, want 2", *cfg.MachineCfg.VcpuCount)
	}
	if *cfg.MachineCfg.MemSizeMib != 512 {
		t.Errorf("MachineCfg.MemSizeMib = %d, want 512", *cfg.MachineCfg.MemSizeMib)
	}
}
