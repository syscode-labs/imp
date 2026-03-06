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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

func newWarmPoolTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(s))
	require.NoError(t, impdevv1alpha1.AddToScheme(s))
	return s
}

// TestWarmPoolReconciler_idleWhenNoBaseSnapshot verifies that the pool stays idle
// when the referenced ImpVMSnapshot has no elected base snapshot yet.
func TestWarmPoolReconciler_idleWhenNoBaseSnapshot(t *testing.T) {
	scheme := newWarmPoolTestScheme(t)

	pool := &impdevv1alpha1.ImpWarmPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-pool",
			Namespace: "default",
		},
		Spec: impdevv1alpha1.ImpWarmPoolSpec{
			SnapshotRef:  "my-snap",
			Size:         2,
			TemplateName: "my-template",
		},
	}

	// Snapshot with no BaseSnapshot elected.
	snap := &impdevv1alpha1.ImpVMSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-snap",
			Namespace: "default",
		},
		Spec: impdevv1alpha1.ImpVMSnapshotSpec{
			SourceVMName:      "source-vm",
			SourceVMNamespace: "default",
			Storage:           impdevv1alpha1.SnapshotStorageSpec{Type: "node-local"},
		},
		Status: impdevv1alpha1.ImpVMSnapshotStatus{
			BaseSnapshot: "", // not elected
		},
	}

	tpl := &impdevv1alpha1.ImpVMTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-template",
			Namespace: "default",
		},
		Spec: impdevv1alpha1.ImpVMTemplateSpec{
			ClassRef: impdevv1alpha1.ClusterObjectRef{Name: "standard"},
			Image:    "ghcr.io/example/myvm:latest",
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&impdevv1alpha1.ImpWarmPool{}).
		WithObjects(pool, snap, tpl).
		Build()

	r := &ImpWarmPoolReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: client.ObjectKey{Namespace: "default", Name: "my-pool"},
	})
	require.NoError(t, err)
	assert.True(t, result.RequeueAfter > 0, "should requeue when pool is idle")

	// No VMs should have been created.
	vmList := &impdevv1alpha1.ImpVMList{}
	require.NoError(t, fakeClient.List(context.Background(), vmList, client.InNamespace("default")))
	assert.Len(t, vmList.Items, 0, "no VMs should be created when pool is idle")
}

// TestWarmPoolReconciler_createsPoolVMs verifies that the reconciler creates spec.size
// VMs when the snapshot has an elected base snapshot.
func TestWarmPoolReconciler_createsPoolVMs(t *testing.T) {
	scheme := newWarmPoolTestScheme(t)

	pool := &impdevv1alpha1.ImpWarmPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-pool",
			Namespace: "default",
		},
		Spec: impdevv1alpha1.ImpWarmPoolSpec{
			SnapshotRef:  "my-snap",
			Size:         2,
			TemplateName: "my-template",
		},
	}

	snap := &impdevv1alpha1.ImpVMSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-snap",
			Namespace: "default",
		},
		Spec: impdevv1alpha1.ImpVMSnapshotSpec{
			SourceVMName:      "source-vm",
			SourceVMNamespace: "default",
			Storage:           impdevv1alpha1.SnapshotStorageSpec{Type: "node-local"},
		},
		Status: impdevv1alpha1.ImpVMSnapshotStatus{
			BaseSnapshot: "my-snap-exec",
		},
	}

	// Child execution snapshot with a SnapshotPath set.
	childSnap := &impdevv1alpha1.ImpVMSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-snap-exec",
			Namespace: "default",
		},
		Spec: impdevv1alpha1.ImpVMSnapshotSpec{
			SourceVMName:      "source-vm",
			SourceVMNamespace: "default",
			Storage:           impdevv1alpha1.SnapshotStorageSpec{Type: "node-local"},
		},
		Status: impdevv1alpha1.ImpVMSnapshotStatus{
			Phase:        "Succeeded",
			SnapshotPath: "/var/lib/imp/snapshots/my-snap-exec",
		},
	}

	tpl := &impdevv1alpha1.ImpVMTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-template",
			Namespace: "default",
		},
		Spec: impdevv1alpha1.ImpVMTemplateSpec{
			ClassRef: impdevv1alpha1.ClusterObjectRef{Name: "standard"},
			Image:    "ghcr.io/example/myvm:latest",
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&impdevv1alpha1.ImpWarmPool{}).
		WithObjects(pool, snap, childSnap, tpl).
		Build()

	r := &ImpWarmPoolReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: client.ObjectKey{Namespace: "default", Name: "my-pool"},
	})
	require.NoError(t, err)
	assert.True(t, result.RequeueAfter > 0, "should requeue after pool reconciliation")

	// 2 VMs should have been created.
	vmList := &impdevv1alpha1.ImpVMList{}
	require.NoError(t, fakeClient.List(context.Background(), vmList,
		client.InNamespace("default"),
		client.MatchingLabels{impdevv1alpha1.LabelWarmPool: "my-pool"},
	))
	assert.Len(t, vmList.Items, 2, "should create spec.size VMs")

	for _, vm := range vmList.Items {
		assert.Equal(t, "my-pool", vm.Labels[impdevv1alpha1.LabelWarmPool],
			"VM should carry the warm-pool label")
		assert.Equal(t, "my-snap-exec", vm.Spec.SnapshotRef,
			"VM should reference the elected base snapshot")
	}
}

// TestWarmPoolReconciler_doesNotExceedSize verifies that no new VMs are created
// when the pool already has spec.size active members.
func TestWarmPoolReconciler_doesNotExceedSize(t *testing.T) {
	scheme := newWarmPoolTestScheme(t)

	pool := &impdevv1alpha1.ImpWarmPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-pool",
			Namespace: "default",
		},
		Spec: impdevv1alpha1.ImpWarmPoolSpec{
			SnapshotRef:  "my-snap",
			Size:         2,
			TemplateName: "my-template",
		},
	}

	snap := &impdevv1alpha1.ImpVMSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-snap",
			Namespace: "default",
		},
		Spec: impdevv1alpha1.ImpVMSnapshotSpec{
			SourceVMName:      "source-vm",
			SourceVMNamespace: "default",
			Storage:           impdevv1alpha1.SnapshotStorageSpec{Type: "node-local"},
		},
		Status: impdevv1alpha1.ImpVMSnapshotStatus{
			BaseSnapshot: "my-snap-exec",
		},
	}

	tpl := &impdevv1alpha1.ImpVMTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-template",
			Namespace: "default",
		},
		Spec: impdevv1alpha1.ImpVMTemplateSpec{
			ClassRef: impdevv1alpha1.ClusterObjectRef{Name: "standard"},
			Image:    "ghcr.io/example/myvm:latest",
		},
	}

	// Two existing active VMs labeled with this pool.
	vm1 := &impdevv1alpha1.ImpVM{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-pool-vm-1",
			Namespace: "default",
			Labels:    map[string]string{impdevv1alpha1.LabelWarmPool: "my-pool"},
		},
		Spec: impdevv1alpha1.ImpVMSpec{
			SnapshotRef: "my-snap-exec",
		},
		Status: impdevv1alpha1.ImpVMStatus{
			Phase: impdevv1alpha1.VMPhaseRunning,
		},
	}
	vm2 := &impdevv1alpha1.ImpVM{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-pool-vm-2",
			Namespace: "default",
			Labels:    map[string]string{impdevv1alpha1.LabelWarmPool: "my-pool"},
		},
		Spec: impdevv1alpha1.ImpVMSpec{
			SnapshotRef: "my-snap-exec",
		},
		Status: impdevv1alpha1.ImpVMStatus{
			Phase: impdevv1alpha1.VMPhasePending,
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&impdevv1alpha1.ImpWarmPool{}, &impdevv1alpha1.ImpVM{}).
		WithObjects(pool, snap, tpl, vm1, vm2).
		Build()

	r := &ImpWarmPoolReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: client.ObjectKey{Namespace: "default", Name: "my-pool"},
	})
	require.NoError(t, err)

	// Still only 2 VMs — none created.
	vmList := &impdevv1alpha1.ImpVMList{}
	require.NoError(t, fakeClient.List(context.Background(), vmList,
		client.InNamespace("default"),
		client.MatchingLabels{impdevv1alpha1.LabelWarmPool: "my-pool"},
	))
	assert.Len(t, vmList.Items, 2, "should not create additional VMs when pool is at capacity")
}
