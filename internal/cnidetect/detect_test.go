package cnidetect_test

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
	"github.com/syscode-labs/imp/internal/cnidetect"
)

func buildScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	_ = impdevv1alpha1.AddToScheme(s)
	return s
}

// buildMapper returns a REST mapper that reports the given CRD groups as present.
func buildMapper(crdGroups ...string) meta.RESTMapper {
	mapper := meta.NewDefaultRESTMapper(nil)
	for _, group := range crdGroups {
		switch group {
		case "cilium.io":
			mapper.Add(schema.GroupVersionKind{
				Group: "cilium.io", Version: "v2", Kind: "CiliumNetworkPolicy",
			}, meta.RESTScopeNamespace)
		case "projectcalico.org":
			mapper.Add(schema.GroupVersionKind{
				Group: "projectcalico.org", Version: "v3", Kind: "GlobalNetworkPolicy",
			}, meta.RESTScopeRoot)
		}
	}
	return mapper
}

func ds(name string) *appsv1.DaemonSet {
	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "kube-system"},
	}
}

func TestDetect(t *testing.T) {
	tests := []struct {
		name        string
		crdGroups   []string
		daemonSets  []*appsv1.DaemonSet
		explicitCfg string // ClusterImpConfig.spec.networking.cni.provider
		want        cnidetect.Result
	}{
		{
			name: "no signals → unknown with iptables",
			want: cnidetect.Result{Provider: cnidetect.ProviderUnknown, NATBackend: cnidetect.NATBackendIPTables},
		},
		{
			name:      "CiliumNetworkPolicy CRD → cilium with nftables",
			crdGroups: []string{"cilium.io"},
			want:      cnidetect.Result{Provider: cnidetect.ProviderCilium, NATBackend: cnidetect.NATBackendNftables},
		},
		{
			name:      "GlobalNetworkPolicy CRD → calico with iptables",
			crdGroups: []string{"projectcalico.org"},
			want:      cnidetect.Result{Provider: cnidetect.ProviderCalico, NATBackend: cnidetect.NATBackendIPTables},
		},
		{
			name:       "cilium-agent DaemonSet → cilium with nftables",
			daemonSets: []*appsv1.DaemonSet{ds("cilium-agent")},
			want:       cnidetect.Result{Provider: cnidetect.ProviderCilium, NATBackend: cnidetect.NATBackendNftables},
		},
		{
			name:       "kube-flannel-ds DaemonSet → flannel with iptables",
			daemonSets: []*appsv1.DaemonSet{ds("kube-flannel-ds")},
			want:       cnidetect.Result{Provider: cnidetect.ProviderFlannel, NATBackend: cnidetect.NATBackendIPTables},
		},
		{
			name:       "calico-node DaemonSet → calico with iptables",
			daemonSets: []*appsv1.DaemonSet{ds("calico-node")},
			want:       cnidetect.Result{Provider: cnidetect.ProviderCalico, NATBackend: cnidetect.NATBackendIPTables},
		},
		{
			name:       "cilium CRD + calico DaemonSet → ambiguous with iptables",
			crdGroups:  []string{"cilium.io"},
			daemonSets: []*appsv1.DaemonSet{ds("calico-node")},
			want:       cnidetect.Result{Provider: cnidetect.ProviderUnknown, NATBackend: cnidetect.NATBackendIPTables, Ambiguous: true},
		},
		{
			name:        "explicit provider cilium-kubeproxy-free → used as-is with nftables",
			explicitCfg: "cilium-kubeproxy-free",
			want:        cnidetect.Result{Provider: cnidetect.ProviderCiliumKubeProxyFree, NATBackend: cnidetect.NATBackendNftables},
		},
		{
			name:        "explicit provider overrides CRD signals",
			explicitCfg: "flannel",
			crdGroups:   []string{"cilium.io"},
			want:        cnidetect.Result{Provider: cnidetect.ProviderFlannel, NATBackend: cnidetect.NATBackendIPTables},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			scheme := buildScheme()
			mapper := buildMapper(tc.crdGroups...)

			var objs []runtime.Object
			for _, d := range tc.daemonSets {
				objs = append(objs, d)
			}
			if tc.explicitCfg != "" {
				objs = append(objs, &impdevv1alpha1.ClusterImpConfig{
					ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
					Spec: impdevv1alpha1.ClusterImpConfigSpec{
						Networking: impdevv1alpha1.NetworkingConfig{
							CNI: impdevv1alpha1.CNIConfig{Provider: tc.explicitCfg},
						},
					},
				})
			}

			c := fake.NewClientBuilder().
				WithScheme(scheme).
				WithRESTMapper(mapper).
				WithRuntimeObjects(objs...).
				Build()

			got, err := cnidetect.Detect(context.Background(), c)
			if err != nil {
				t.Fatalf("Detect() error = %v", err)
			}
			if got != tc.want {
				t.Errorf("Detect() = %+v, want %+v", got, tc.want)
			}
		})
	}
}
