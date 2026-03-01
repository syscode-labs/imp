package v1alpha1_test

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	impv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

func TestAllTypesRegisterWithScheme(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := impv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}

	gv := schema.GroupVersion{Group: "imp.dev", Version: "v1alpha1"}

	kinds := []string{
		"ImpVM", "ImpVMList",
		"ImpVMClass", "ImpVMClassList",
		"ImpVMTemplate", "ImpVMTemplateList",
		"ImpNetwork", "ImpNetworkList",
		"ClusterImpConfig", "ClusterImpConfigList",
		"ClusterImpNodeProfile", "ClusterImpNodeProfileList",
	}

	for _, kind := range kinds {
		gvk := gv.WithKind(kind)
		if !scheme.Recognizes(gvk) {
			t.Errorf("scheme does not recognise %s", gvk)
		}
	}
}
