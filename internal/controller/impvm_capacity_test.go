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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

// makeNode creates a node with imp/enabled=true and the given allocatable resources.
func makeNode(ctx context.Context, name string, cpu, memory string) *corev1.Node {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: map[string]string{labelImpEnabled: "true"},
		},
	}
	Expect(k8sClient.Create(ctx, node)).To(Succeed())
	DeferCleanup(func() { k8sClient.Delete(ctx, node) }) //nolint:errcheck

	// Set allocatable resources on node status.
	patch := client.MergeFrom(node.DeepCopy())
	node.Status.Allocatable = corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse(cpu),
		corev1.ResourceMemory: resource.MustParse(memory),
	}
	Expect(k8sClient.Status().Patch(ctx, node, patch)).To(Succeed())
	return node
}

// makeClass creates an ImpVMClass with the given vcpu/mem.
func makeClass(ctx context.Context, name string, vcpu int32, memMiB int32) *impdevv1alpha1.ImpVMClass {
	class := &impdevv1alpha1.ImpVMClass{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: impdevv1alpha1.ImpVMClassSpec{
			VCPU:      vcpu,
			MemoryMiB: memMiB,
			DiskGiB:   10,
		},
	}
	Expect(k8sClient.Create(ctx, class)).To(Succeed())
	DeferCleanup(func() { k8sClient.Delete(ctx, class) }) //nolint:errcheck
	return class
}

var _ = Describe("ImpVM Capacity Scheduler", func() {
	ctx := context.Background()

	reconcileVM := func(name string) error {
		r := &ImpVMReconciler{
			Client:   k8sClient,
			Scheme:   k8sClient.Scheme(),
			Recorder: record.NewFakeRecorder(32),
		}
		// First reconcile: adds finalizer.
		_, err := r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: name, Namespace: "default"},
		})
		if err != nil {
			return err
		}
		// Second reconcile: schedules.
		_, err = r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: name, Namespace: "default"},
		})
		return err
	}

	It("schedules VM when node has sufficient allocatable resources", func() {
		// Node: 4 CPUs, 8GiB memory; Class: 1 vcpu, 512MiB; fraction 0.9
		// effectiveMax = min(floor(4000*0.9/1000), floor(8GiB*0.9/512MiB)) = min(3, 14) = 3
		makeNode(ctx, "cap-node-ok", "4", "8Gi")
		makeClass(ctx, "cap-small", 1, 512)

		vm := &impdevv1alpha1.ImpVM{
			ObjectMeta: metav1.ObjectMeta{Name: "cap-vm-ok", Namespace: "default"},
			Spec: impdevv1alpha1.ImpVMSpec{
				ClassRef: &impdevv1alpha1.ClusterObjectRef{Name: "cap-small"},
			},
		}
		Expect(k8sClient.Create(ctx, vm)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, vm) }) //nolint:errcheck

		Expect(reconcileVM("cap-vm-ok")).To(Succeed())

		updated := &impdevv1alpha1.ImpVM{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "cap-vm-ok", Namespace: "default"}, updated)).To(Succeed())
		Expect(updated.Spec.NodeName).To(Equal("cap-node-ok"))
		Expect(updated.Status.Phase).To(Equal(impdevv1alpha1.VMPhaseScheduled))
	})

	It("refuses to schedule when VM class exceeds node allocatable CPU", func() {
		// Node: 1 CPU, 64GiB; Class: 4 vcpu → 0 fit; should be Unschedulable
		makeNode(ctx, "cap-node-small-cpu", "1", "64Gi")
		makeClass(ctx, "cap-big-cpu", 4, 256)

		vm := &impdevv1alpha1.ImpVM{
			ObjectMeta: metav1.ObjectMeta{Name: "cap-vm-no-cpu", Namespace: "default"},
			Spec: impdevv1alpha1.ImpVMSpec{
				ClassRef: &impdevv1alpha1.ClusterObjectRef{Name: "cap-big-cpu"},
			},
		}
		Expect(k8sClient.Create(ctx, vm)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, vm) }) //nolint:errcheck

		Expect(reconcileVM("cap-vm-no-cpu")).To(Succeed())

		updated := &impdevv1alpha1.ImpVM{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "cap-vm-no-cpu", Namespace: "default"}, updated)).To(Succeed())
		Expect(updated.Spec.NodeName).To(BeEmpty())
		Expect(updated.Status.Phase).To(Equal(impdevv1alpha1.VMPhasePending))
	})

	It("refuses to schedule when VM class exceeds node allocatable memory", func() {
		// Node: 16 CPUs, 256MiB; Class: 1 vcpu, 512MiB → 0 fit by memory
		makeNode(ctx, "cap-node-small-mem", "16", "256Mi")
		makeClass(ctx, "cap-big-mem", 1, 512)

		vm := &impdevv1alpha1.ImpVM{
			ObjectMeta: metav1.ObjectMeta{Name: "cap-vm-no-mem", Namespace: "default"},
			Spec: impdevv1alpha1.ImpVMSpec{
				ClassRef: &impdevv1alpha1.ClusterObjectRef{Name: "cap-big-mem"},
			},
		}
		Expect(k8sClient.Create(ctx, vm)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, vm) }) //nolint:errcheck

		Expect(reconcileVM("cap-vm-no-mem")).To(Succeed())

		updated := &impdevv1alpha1.ImpVM{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "cap-vm-no-mem", Namespace: "default"}, updated)).To(Succeed())
		Expect(updated.Spec.NodeName).To(BeEmpty())
		Expect(updated.Status.Phase).To(Equal(impdevv1alpha1.VMPhasePending))
	})

	It("respects per-node capacityFraction from ClusterImpNodeProfile", func() {
		// Node: 4 CPUs, 8GiB; fraction 0.1 → floor(4000*0.1/1000)=0 → unschedulable
		makeNode(ctx, "cap-node-low-frac", "4", "8Gi")
		makeClass(ctx, "cap-tiny", 1, 256)

		profile := &impdevv1alpha1.ClusterImpNodeProfile{
			ObjectMeta: metav1.ObjectMeta{Name: "cap-node-low-frac"},
			Spec:       impdevv1alpha1.ClusterImpNodeProfileSpec{CapacityFraction: "0.1"},
		}
		Expect(k8sClient.Create(ctx, profile)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, profile) }) //nolint:errcheck

		vm := &impdevv1alpha1.ImpVM{
			ObjectMeta: metav1.ObjectMeta{Name: "cap-vm-low-frac", Namespace: "default"},
			Spec: impdevv1alpha1.ImpVMSpec{
				ClassRef: &impdevv1alpha1.ClusterObjectRef{Name: "cap-tiny"},
			},
		}
		Expect(k8sClient.Create(ctx, vm)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, vm) }) //nolint:errcheck

		Expect(reconcileVM("cap-vm-low-frac")).To(Succeed())

		updated := &impdevv1alpha1.ImpVM{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "cap-vm-low-frac", Namespace: "default"}, updated)).To(Succeed())
		Expect(updated.Spec.NodeName).To(BeEmpty())
		Expect(updated.Status.Phase).To(Equal(impdevv1alpha1.VMPhasePending))
	})

	It("falls back to 0.9 when no ClusterImpConfig exists", func() {
		// No ClusterImpConfig created; node 4 CPUs / 8GiB; class 1vcpu/512MiB
		// effectiveMax with 0.9 = min(3, 14) = 3 → should schedule
		makeNode(ctx, "cap-node-no-cfg", "4", "8Gi")
		makeClass(ctx, "cap-def-frac", 1, 512)

		vm := &impdevv1alpha1.ImpVM{
			ObjectMeta: metav1.ObjectMeta{Name: "cap-vm-no-cfg", Namespace: "default"},
			Spec: impdevv1alpha1.ImpVMSpec{
				ClassRef: &impdevv1alpha1.ClusterObjectRef{Name: "cap-def-frac"},
			},
		}
		Expect(k8sClient.Create(ctx, vm)).To(Succeed())
		DeferCleanup(func() { k8sClient.Delete(ctx, vm) }) //nolint:errcheck

		Expect(reconcileVM("cap-vm-no-cfg")).To(Succeed())

		updated := &impdevv1alpha1.ImpVM{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "cap-vm-no-cfg", Namespace: "default"}, updated)).To(Succeed())
		Expect(updated.Spec.NodeName).To(Equal("cap-node-no-cfg"))
	})
})
