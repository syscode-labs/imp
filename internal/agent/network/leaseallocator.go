package network

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net"
	"strings"

	coordinationv1 "k8s.io/api/coordination/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// LeaseAllocator coordinates IPv4 claims through Kubernetes Lease objects.
// A claim is named deterministically from the ImpNetwork and IP address, so
// Lease creation atomically prevents separate nodes from choosing the same IP.
type LeaseAllocator struct {
	client ctrlclient.Client
}

// NewLeaseAllocator returns an allocator backed by the supplied Kubernetes client.
func NewLeaseAllocator(client ctrlclient.Client) *LeaseAllocator {
	return &LeaseAllocator{client: client}
}

// Allocate claims the next free IP in subnet for holder. gateway is reserved;
// when empty, the first host address is used as the gateway.
func (a *LeaseAllocator) Allocate(ctx context.Context, networkKey, subnet, gateway, holder string) (string, error) {
	namespace, err := networkNamespace(networkKey)
	if err != nil {
		return "", err
	}
	if holder == "" {
		return "", fmt.Errorf("lease holder is required")
	}

	_, cidr, err := net.ParseCIDR(subnet)
	if err != nil {
		return "", fmt.Errorf("parse subnet %q: %w", subnet, err)
	}

	gwIP := nextIP(cidr.IP.To4())
	if gateway != "" {
		gwIP = net.ParseIP(gateway).To4()
		if gwIP == nil {
			return "", fmt.Errorf("parse gateway %q", gateway)
		}
	}

	bcast := broadcastIP(cidr)
	for ip := nextIP(cidr.IP.To4()); cidr.Contains(ip) && !ip.Equal(bcast); ip = nextIP(ip) {
		if ip.Equal(gwIP) {
			continue
		}
		ipString := ip.String()
		lease := &coordinationv1.Lease{
			ObjectMeta: metav1.ObjectMeta{
				Name:      leaseName(networkKey, ipString),
				Namespace: namespace,
			},
			Spec: coordinationv1.LeaseSpec{HolderIdentity: &holder},
		}
		if err := a.client.Create(ctx, lease); err == nil {
			return ipString, nil
		} else if !apierrors.IsAlreadyExists(err) {
			return "", fmt.Errorf("create IP claim for %s: %w", ipString, err)
		} else {
			existing := &coordinationv1.Lease{}
			if err := a.client.Get(ctx, ctrlclient.ObjectKey{Namespace: namespace, Name: lease.Name}, existing); err != nil {
				return "", fmt.Errorf("get existing IP claim for %s: %w", ipString, err)
			}
			if existing.Spec.HolderIdentity != nil && *existing.Spec.HolderIdentity == holder {
				return ipString, nil
			}
		}
	}

	return "", fmt.Errorf("no free IPs in subnet %s", subnet)
}

// Release deletes holder's claim for ip. It refuses to delete a claim owned by
// another VM, preventing a stale runtime from releasing a live VM's address.
func (a *LeaseAllocator) Release(ctx context.Context, networkKey, ip, holder string) error {
	namespace, err := networkNamespace(networkKey)
	if err != nil {
		return err
	}

	lease := &coordinationv1.Lease{}
	key := ctrlclient.ObjectKey{Namespace: namespace, Name: leaseName(networkKey, ip)}
	if err := a.client.Get(ctx, key, lease); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("get IP claim for %s: %w", ip, err)
	}
	if lease.Spec.HolderIdentity == nil || *lease.Spec.HolderIdentity != holder {
		return fmt.Errorf("IP claim for %s is held by another VM", ip)
	}

	if err := a.client.Delete(ctx, lease, &ctrlclient.DeleteOptions{
		Preconditions: &metav1.Preconditions{UID: ptr(lease.UID)},
	}); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete IP claim for %s: %w", ip, err)
	}
	return nil
}

func networkNamespace(networkKey string) (string, error) {
	namespace, name, ok := strings.Cut(networkKey, "/")
	if !ok || namespace == "" || name == "" {
		return "", fmt.Errorf("network key %q must be namespace/name", networkKey)
	}
	return namespace, nil
}

func leaseName(networkKey, ip string) string {
	sum := sha256.Sum256([]byte(networkKey + "\x00" + ip))
	return fmt.Sprintf("imp-ip-%x", sum[:28])
}

func ptr[T any](value T) *T {
	return &value
}
