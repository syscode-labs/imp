package controller

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

func pressureTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	sch := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(sch); err != nil {
		t.Fatal(err)
	}
	if err := impdevv1alpha1.AddToScheme(sch); err != nil {
		t.Fatal(err)
	}
	return sch
}

func runningVM(name, node string, phase impdevv1alpha1.VMPhase, desired impdevv1alpha1.VMDesiredState, ann map[string]string, created metav1.Time) *impdevv1alpha1.ImpVM {
	return &impdevv1alpha1.ImpVM{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Namespace:         "default",
			CreationTimestamp: created,
			Annotations:       ann,
			Labels:            map[string]string{},
		},
		Spec: impdevv1alpha1.ImpVMSpec{
			NodeName:     node,
			DesiredState: desired,
			ClassRef:     &impdevv1alpha1.ClusterObjectRef{Name: "c"},
		},
		Status: impdevv1alpha1.ImpVMStatus{Phase: phase},
	}
}

var _ = Describe("PressureReconciler election", func() {
	ctx := context.Background()

	It("skips foreign nodes and non-running phases", func() {
		foreign := runningVM("foreign", "n2", impdevv1alpha1.VMPhaseRunning,
			impdevv1alpha1.VMDesiredStateRunning, nil, metav1.Now())
		done := runningVM("done", "n1", impdevv1alpha1.VMPhaseSuspended,
			impdevv1alpha1.VMDesiredStateRunning, nil, metav1.Now())
		c := fake.NewClientBuilder().WithScheme(pressureTestScheme(&testing.T{})).
			WithObjects(foreign, done).Build()
		r := &PressureReconciler{Client: c}

		victim, err := r.electVictim(ctx, "n1")
		Expect(err).NotTo(HaveOccurred())
		Expect(victim).To(BeNil())
	})

	It("skips already pressure-suspended VMs", func() {
		handled := runningVM("handled", "n1", impdevv1alpha1.VMPhaseRunning,
			impdevv1alpha1.VMDesiredStateSuspended,
			map[string]string{annotationPressureSuspended: "from-running"}, metav1.Now())
		c := fake.NewClientBuilder().WithScheme(pressureTestScheme(&testing.T{})).
			WithObjects(handled).Build()
		r := &PressureReconciler{Client: c}

		victim, err := r.electVictim(ctx, "n1")
		Expect(err).NotTo(HaveOccurred())
		Expect(victim).To(BeNil())
	})
})

var _ = Describe("isSafePressureVictim", func() {
	It("rejects ScaleToZero and busy VMs", func() {
		stz := runningVM("stz", "n1", impdevv1alpha1.VMPhaseRunning,
			impdevv1alpha1.VMDesiredStateScaleToZero, nil, metav1.Now())
		busy := runningVM("busy", "n1", impdevv1alpha1.VMPhaseRunning,
			impdevv1alpha1.VMDesiredStateRunning,
			map[string]string{"imp.dev/snapshot-in-progress": "1"}, metav1.Now())
		Expect(isSafePressureVictim(stz)).To(BeFalse())
		Expect(isSafePressureVictim(busy)).To(BeFalse())
	})
})

var _ = Describe("PressureReconciler suspendVictim", func() {
	It("sets Suspended desired state and records restore annotations", func(ctx SpecContext) {
		vm := runningVM("victim", "n1", impdevv1alpha1.VMPhaseRunning,
			impdevv1alpha1.VMDesiredStateRunning, nil, metav1.Now())
		c := fake.NewClientBuilder().WithScheme(pressureTestScheme(&testing.T{})).
			WithObjects(vm).WithStatusSubresource(vm).Build()
		r := &PressureReconciler{Client: c}

		live := &impdevv1alpha1.ImpVM{}
		Expect(c.Get(ctx, types.NamespacedName{Namespace: "default", Name: "victim"}, live)).To(Succeed())
		Expect(r.suspendVictim(ctx, "n1", live)).To(Succeed())

		Expect(c.Get(ctx, types.NamespacedName{Namespace: "default", Name: "victim"}, live)).To(Succeed())
		Expect(live.Spec.DesiredState).To(Equal(impdevv1alpha1.VMDesiredStateSuspended))
		Expect(live.Annotations[annotationPressureSuspended]).To(Equal("from-running"))
		Expect(live.Annotations[annotationPressureRestore]).To(Equal("Running"))
	})
})
