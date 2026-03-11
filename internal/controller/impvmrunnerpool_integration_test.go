package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	impv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

func i32(v int32) *int32 { return &v }

var _ = Describe("ImpVMRunnerPool scaling integration", func() {
	ctx := context.Background()

	It("rejects scaling-enabled pool without explicit ceilings", func() {
		pool := &impv1alpha1.ImpVMRunnerPool{
			ObjectMeta: metav1.ObjectMeta{Name: "scale-invalid", Namespace: "default"},
			Spec: impv1alpha1.ImpVMRunnerPoolSpec{
				TemplateName: "tpl-does-not-matter",
				Platform: impv1alpha1.RunnerPlatformSpec{
					Type:              "github-actions",
					CredentialsSecret: "gh-creds",
					Scope:             &impv1alpha1.RunnerScopeSpec{Repo: "owner/repo"},
				},
				Scaling: &impv1alpha1.RunnerScalingSpec{
					Mode:    impv1alpha1.RunnerScalingModeWebhook,
					Webhook: &impv1alpha1.RunnerWebhookSpec{SecretRef: "wh-secret"},
				},
			},
		}
		err := k8sClient.Create(ctx, pool)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("github-actions scaling requires explicit minIdle"))
	})

	It("creates VMs with webhook mode respecting scaleUpStep", func() {
		tpl := &impv1alpha1.ImpVMTemplate{
			ObjectMeta: metav1.ObjectMeta{Name: "scale-tpl", Namespace: "default"},
			Spec: impv1alpha1.ImpVMTemplateSpec{
				ClassRef: impv1alpha1.ClusterObjectRef{Name: "standard"},
				Image:    "ghcr.io/syscode-labs/test:latest",
			},
		}
		Expect(k8sClient.Create(ctx, tpl)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, tpl) })

		pool := &impv1alpha1.ImpVMRunnerPool{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "scale-valid",
				Namespace: "default",
				Annotations: map[string]string{
					AnnotationRunnerDemand: "5",
				},
			},
			Spec: impv1alpha1.ImpVMRunnerPoolSpec{
				TemplateName: "scale-tpl",
				Platform: impv1alpha1.RunnerPlatformSpec{
					Type:              "github-actions",
					CredentialsSecret: "gh-creds",
					Scope:             &impv1alpha1.RunnerScopeSpec{Repo: "owner/repo"},
				},
				Scaling: &impv1alpha1.RunnerScalingSpec{
					Mode:            impv1alpha1.RunnerScalingModeWebhook,
					MinIdle:         i32(0),
					MaxConcurrent:   i32(10),
					ScaleUpStep:     i32(2),
					CooldownSeconds: i32(30),
					Webhook:         &impv1alpha1.RunnerWebhookSpec{SecretRef: "wh-secret"},
				},
			},
		}
		Expect(k8sClient.Create(ctx, pool)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, pool) })

		r := &ImpVMRunnerPoolReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		_, err := r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: pool.Name, Namespace: pool.Namespace},
		})
		Expect(err).NotTo(HaveOccurred())

		vmList := &impv1alpha1.ImpVMList{}
		Expect(k8sClient.List(ctx, vmList,
			client.InNamespace("default"),
			client.MatchingLabels{impv1alpha1.LabelRunnerPool: pool.Name},
		)).To(Succeed())
		Expect(vmList.Items).To(HaveLen(2))
	})
})
