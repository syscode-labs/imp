// Package sandboxgateway implements the node-local data-plane gateway for
// sandboxes: per-sandbox token auth, VSOCK socket resolution, and (in
// follow-up commits) the Connect RPC surface proxying to the guest's
// SandboxControl service.
package sandboxgateway

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// Token computes the per-sandbox, per-VM session token. Binding the token to
// the VM reference prevents a valid sandbox token from authorizing a different
// sandbox co-located on the same node.
func Token(clusterKey []byte, sandboxUID, namespace, vmName string) string {
	mac := hmac.New(sha256.New, clusterKey)
	mac.Write([]byte("sandbox:" + sandboxUID + "\x00" + namespace + "\x00" + vmName))
	return hex.EncodeToString(mac.Sum(nil))
}

// Verify reports whether presented matches the expected token for scope,
// without leaking timing information.
func Verify(clusterKey []byte, sandboxUID, namespace, vmName, presented string) bool {
	expected := Token(clusterKey, sandboxUID, namespace, vmName)
	return hmac.Equal([]byte(expected), []byte(presented))
}

// GuestToken derives the control token presented by the gateway to the guest.
// It uses a separate HMAC domain from the client session token so either token
// cannot be replayed at the other authentication boundary.
func GuestToken(clusterKey []byte, sandboxUID, namespace, vmName string) string {
	mac := hmac.New(sha256.New, clusterKey)
	mac.Write([]byte("guest:" + sandboxUID + "\x00" + namespace + "\x00" + vmName))
	return hex.EncodeToString(mac.Sum(nil))
}
