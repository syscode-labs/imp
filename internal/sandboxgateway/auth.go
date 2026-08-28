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

// Token computes the per-sandbox session token:
// HMAC-SHA256(clusterKey, "sandbox:"+uid). The controller mints tokens into
// per-sandbox Secrets; the gateway recomputes and compares constant-time.
// Both sides share the cluster key via a chart-managed Secret.
func Token(clusterKey []byte, sandboxUID string) string {
	mac := hmac.New(sha256.New, clusterKey)
	mac.Write([]byte("sandbox:" + sandboxUID))
	return hex.EncodeToString(mac.Sum(nil))
}

// Verify reports whether presented matches the expected token for uid,
// without leaking timing information.
func Verify(clusterKey []byte, sandboxUID, presented string) bool {
	expected := Token(clusterKey, sandboxUID)
	return hmac.Equal([]byte(expected), []byte(presented))
}
