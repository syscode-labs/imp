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
	"context"
	"fmt"
	"strconv"

	"sigs.k8s.io/controller-runtime/pkg/client"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
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

// resolveClassSpec returns the ImpVMClassSpec for vm by following either
// vm.Spec.ClassRef (direct) or vm.Spec.TemplateRef → template.Spec.ClassRef.
// Returns an error if neither ref is set or if the referenced objects are missing.
func resolveClassSpec(ctx context.Context, c client.Client, vm *impdevv1alpha1.ImpVM) (*impdevv1alpha1.ImpVMClassSpec, error) {
	if vm.Spec.ClassRef != nil {
		var class impdevv1alpha1.ImpVMClass
		if err := c.Get(ctx, client.ObjectKey{Name: vm.Spec.ClassRef.Name}, &class); err != nil {
			return nil, fmt.Errorf("get class %q: %w", vm.Spec.ClassRef.Name, err)
		}
		return &class.Spec, nil
	}
	if vm.Spec.TemplateRef != nil {
		var tmpl impdevv1alpha1.ImpVMTemplate
		if err := c.Get(ctx, client.ObjectKey{Namespace: vm.Namespace, Name: vm.Spec.TemplateRef.Name}, &tmpl); err != nil {
			return nil, fmt.Errorf("get template %q: %w", vm.Spec.TemplateRef.Name, err)
		}
		var class impdevv1alpha1.ImpVMClass
		if err := c.Get(ctx, client.ObjectKey{Name: tmpl.Spec.ClassRef.Name}, &class); err != nil {
			return nil, fmt.Errorf("get class %q (via template %q): %w", tmpl.Spec.ClassRef.Name, tmpl.Name, err)
		}
		return &class.Spec, nil
	}
	return nil, fmt.Errorf("vm %s/%s has neither classRef nor templateRef", vm.Namespace, vm.Name)
}
