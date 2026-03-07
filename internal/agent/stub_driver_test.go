//go:build linux

package agent

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

var _ = Describe("StubDriver: IsAlive and Reattach", func() {
	var d *StubDriver
	var vm *impdevv1alpha1.ImpVM

	BeforeEach(func() {
		d = NewStubDriver()
		vm = &impdevv1alpha1.ImpVM{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-vm",
				Namespace: "default",
			},
		}
	})

	Describe("IsAlive", func() {
		It("returns false when IsAliveResult is false (default)", func() {
			Expect(d.IsAlive(int64(12345))).To(BeFalse())
		})

		It("returns true when IsAliveResult is true", func() {
			d.IsAliveResult = true
			Expect(d.IsAlive(int64(12345))).To(BeTrue())
		})
	})

	Describe("Reattach", func() {
		It("records the vmKey in ReattachCalls", func() {
			Expect(d.Reattach(context.Background(), vm)).To(Succeed())
			Expect(d.ReattachCalls).To(ConsistOf("default/test-vm"))
		})

		It("accumulates multiple calls", func() {
			vm2 := &impdevv1alpha1.ImpVM{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "other-vm",
					Namespace: "default",
				},
			}
			Expect(d.Reattach(context.Background(), vm)).To(Succeed())
			Expect(d.Reattach(context.Background(), vm2)).To(Succeed())
			Expect(d.ReattachCalls).To(ConsistOf("default/test-vm", "default/other-vm"))
		})

		It("returns and clears ReattachErr on error", func() {
			sentinel := errors.New("reattach failed")
			d.ReattachErr = sentinel

			err := d.Reattach(context.Background(), vm)
			Expect(err).To(MatchError(sentinel))

			// error is cleared — second call succeeds
			Expect(d.Reattach(context.Background(), vm)).To(Succeed())
		})
	})
})
