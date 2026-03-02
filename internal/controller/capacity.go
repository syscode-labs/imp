/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"strconv"
)

// parseFraction parses a fraction string (e.g. "0.9") into a float64 in [0,1].
// Returns 0.9 for empty, unparseable, or out-of-range input.
func parseFraction(s string) float64 {
	if s == "" {
		return 0.9
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v < 0 || v > 1 {
		return 0.9
	}
	return v
}

// effectiveMaxVMs returns the maximum number of VMs that fit on a node given
// the node's allocatable resources, the VM's compute requirements, and the
// capacity fraction.
//
//	effectiveMax = min(
//	  floor(allocCPUMillicores * fraction / vmCPUMillicores),
//	  floor(allocMemBytes      * fraction / vmMemBytes),
//	)
//
// Returns 0 if either dimension cannot fit even one VM.
func effectiveMaxVMs(allocCPUMillis, allocMemBytes int64, vcpu, memMiB int32, fraction float64) int32 {
	vmCPUMillis := int64(vcpu) * 1000
	cpuMax := int64(float64(allocCPUMillis)*fraction) / vmCPUMillis

	vmMemBytes := int64(memMiB) * 1024 * 1024
	memMax := int64(float64(allocMemBytes)*fraction) / vmMemBytes

	result := cpuMax
	if memMax < cpuMax {
		result = memMax
	}
	if result < 0 {
		return 0
	}
	return int32(result) //nolint:gosec
}
