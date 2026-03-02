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
	"testing"
)

func TestParseFraction(t *testing.T) {
	cases := []struct {
		in   string
		want float64
	}{
		{"0.9", 0.9},
		{"1.0", 1.0},
		{"0", 0.0},
		{"0.5", 0.5},
		{"", 0.9},        // empty → default
		{"invalid", 0.9}, // bad string → default
		{"1.1", 0.9},     // out of range → default
		{"-0.1", 0.9},    // negative → default
	}
	for _, tc := range cases {
		got := parseFraction(tc.in)
		if got != tc.want {
			t.Errorf("parseFraction(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestEffectiveMaxVMs(t *testing.T) {
	cases := []struct {
		name          string
		allocCPUm     int64 // node allocatable CPU in millicores
		allocMemBytes int64 // node allocatable memory in bytes
		vcpu          int32 // VM vCPUs
		memMiB        int32 // VM memory in MiB
		fraction      float64
		want          int32
	}{
		{
			name: "cpu-bound: 4 CPUs, 0.9 fraction, 1 vcpu VMs",
			// 4000m * 0.9 / 1000m = 3.6 → floor 3
			allocCPUm: 4000, allocMemBytes: 64 * 1024 * 1024 * 1024,
			vcpu: 1, memMiB: 256, fraction: 0.9,
			want: 3,
		},
		{
			name: "memory-bound: 1GiB, 0.9 fraction, 512MiB VMs",
			// cpu: 16000m * 0.9 / 1000m = 14; mem: 1GiB * 0.9 / 512MiB = 1.8 → floor 1
			allocCPUm: 16000, allocMemBytes: 1 * 1024 * 1024 * 1024,
			vcpu: 1, memMiB: 512, fraction: 0.9,
			want: 1,
		},
		{
			name: "both equal: 4 VMs fit by both CPU and memory",
			// cpu: 4000m * 1.0 / 1000m = 4; mem: 4*512MiB * 1.0 / 512MiB = 4
			allocCPUm: 4000, allocMemBytes: 4 * 512 * 1024 * 1024,
			vcpu: 1, memMiB: 512, fraction: 1.0,
			want: 4,
		},
		{
			name:      "fraction zero → 0 VMs",
			allocCPUm: 8000, allocMemBytes: 16 * 1024 * 1024 * 1024,
			vcpu: 1, memMiB: 512, fraction: 0.0,
			want: 0,
		},
		{
			name: "VM larger than node → 0",
			// node has 2 CPUs, VM wants 4 → 0
			allocCPUm: 2000, allocMemBytes: 64 * 1024 * 1024 * 1024,
			vcpu: 4, memMiB: 256, fraction: 0.9,
			want: 0,
		},
		{
			name: "8 CPUs 0.9 fraction 2vcpu VMs",
			// 8000m * 0.9 / 2000m = 3.6 → floor 3; mem plenty
			allocCPUm: 8000, allocMemBytes: 64 * 1024 * 1024 * 1024,
			vcpu: 2, memMiB: 512, fraction: 0.9,
			want: 3,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := effectiveMaxVMs(tc.allocCPUm, tc.allocMemBytes, tc.vcpu, tc.memMiB, tc.fraction)
			if got != tc.want {
				t.Errorf("effectiveMaxVMs = %d, want %d", got, tc.want)
			}
		})
	}
}
