//go:build linux

package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseCPUModel_standard(t *testing.T) {
	cpuinfo := "processor\t: 0\nvendor_id\t: GenuineIntel\nmodel name\t: Intel(R) Core(TM) i5-8500T CPU @ 2.10GHz\ncpu MHz\t\t: 2100.000\n"
	model := parseCPUModelFromProcInfo(cpuinfo)
	assert.Equal(t, "Intel(R) Core(TM) i5-8500T CPU @ 2.10GHz", model)
}

func TestParseCPUModel_arm64(t *testing.T) {
	cpuinfo := "processor\t: 0\nFeatures\t: fp asimd\nCPU implementer\t: 0x41\nmodel name\t: ARM Cortex-A72\n"
	model := parseCPUModelFromProcInfo(cpuinfo)
	assert.Equal(t, "ARM Cortex-A72", model)
}

func TestParseCPUModel_missing(t *testing.T) {
	model := parseCPUModelFromProcInfo("no model name here\nprocessor: 0\n")
	assert.Equal(t, "", model)
}

func TestParseCPUModel_empty(t *testing.T) {
	model := parseCPUModelFromProcInfo("")
	assert.Equal(t, "", model)
}
