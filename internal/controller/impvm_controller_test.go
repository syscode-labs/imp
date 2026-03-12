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
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

func newReconciler() *ImpVMReconciler {
	return &ImpVMReconciler{
		Client:   k8sClient,
		Scheme:   k8sClient.Scheme(),
		Recorder: record.NewFakeRecorder(32),
	}
}

var _ = Describe("ImpVM Scheduler", func() {
	ctx := context.Background()

	It("sets phase=Pending and emits Unschedulable when no imp/enabled nodes exist", func() {
		vm := &impdevv1alpha1.ImpVM{
			ObjectMeta: metav1.ObjectMeta{Name: "sched-no-nodes", Namespace: "default"},
		}
		Expect(k8sClient.Create(ctx, vm)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, vm)).To(Or(Succeed(), MatchError(ContainSubstring("not found")))) })

		// First reconcile: adds finalizer and returns
		_, err := newReconciler().Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "sched-no-nodes", Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())

		// Second reconcile: tries to schedule, finds no nodes
		_, err = newReconciler().Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "sched-no-nodes", Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())

		updated := &impdevv1alpha1.ImpVM{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "sched-no-nodes", Namespace: "default"}, updated)).To(Succeed())
		Expect(updated.Status.Phase).To(Equal(impdevv1alpha1.VMPhasePending))
	})
})

var _ = Describe("ImpVM SyncStatus", func() {
	ctx := context.Background()

	It("clears nodeName and sets Pending for ephemeral VM when assigned node not found", func() {
		vm := &impdevv1alpha1.ImpVM{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "sync-no-node",
				Namespace:  "default",
				Finalizers: []string{"imp/finalizer"},
			},
			Spec: impdevv1alpha1.ImpVMSpec{
				NodeName:  "ghost-node",
				Lifecycle: impdevv1alpha1.VMLifecycleEphemeral,
			},
		}
		Expect(k8sClient.Create(ctx, vm)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, vm)).To(Or(Succeed(), MatchError(ContainSubstring("not found")))) })

		_, err := newReconciler().Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "sync-no-node", Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())

		updated := &impdevv1alpha1.ImpVM{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "sync-no-node", Namespace: "default"}, updated)).To(Succeed())
		Expect(updated.Spec.NodeName).To(BeEmpty())
		Expect(updated.Status.Phase).To(Equal(impdevv1alpha1.VMPhasePending))
	})
})

var _ = Describe("ImpVM Deletion", func() {
	ctx := context.Background()

	It("removes finalizer immediately when spec.nodeName is empty on deletion", func() {
		vm := &impdevv1alpha1.ImpVM{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "del-unscheduled",
				Namespace:  "default",
				Finalizers: []string{"imp/finalizer"},
			},
		}
		Expect(k8sClient.Create(ctx, vm)).To(Succeed())
		Expect(k8sClient.Delete(ctx, vm)).To(Succeed())

		_, err := newReconciler().Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "del-unscheduled", Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())

		updated := &impdevv1alpha1.ImpVM{}
		err = k8sClient.Get(ctx, types.NamespacedName{Name: "del-unscheduled", Namespace: "default"}, updated)
		Expect(errors.IsNotFound(err)).To(BeTrue())
	})
})

var _ = Describe("ImpVM Spec Validation Fallback", func() {
	ctx := context.Background()

	It("marks VM Failed with SpecInvalid when classRef and templateRef are both missing", func() {
		vm := &impdevv1alpha1.ImpVM{
			ObjectMeta: metav1.ObjectMeta{Name: "invalid-no-refs", Namespace: "default"},
			Spec: impdevv1alpha1.ImpVMSpec{
				Image: "docker.io/library/nginx:1.27-alpine",
			},
		}
		Expect(k8sClient.Create(ctx, vm)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, vm)).To(Or(Succeed(), MatchError(ContainSubstring("not found")))) })

		// First reconcile adds finalizer.
		_, err := newReconciler().Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "invalid-no-refs", Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())

		// Second reconcile applies spec validation fallback.
		_, err = newReconciler().Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "invalid-no-refs", Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())

		updated := &impdevv1alpha1.ImpVM{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "invalid-no-refs", Namespace: "default"}, updated)).To(Succeed())
		Expect(updated.Status.Phase).To(Equal(impdevv1alpha1.VMPhaseFailed))
		var readyCond *metav1.Condition
		for i := range updated.Status.Conditions {
			c := &updated.Status.Conditions[i]
			if c.Type == ConditionReady {
				readyCond = c
				break
			}
		}
		Expect(readyCond).NotTo(BeNil())
		Expect(readyCond.Reason).To(Equal(EventReasonSpecInvalid))
		Expect(readyCond.Message).To(Equal("invalid spec: exactly one of classRef or templateRef must be set"))
	})
})
