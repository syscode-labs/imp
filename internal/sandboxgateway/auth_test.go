package sandboxgateway

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestToken_deterministicAndDistinct(t *testing.T) {
	a := Token([]byte("key"), "uid-1", "ns-a", "sandbox-a")
	assert.Equal(t, a, Token([]byte("key"), "uid-1", "ns-a", "sandbox-a"))
	assert.NotEqual(t, a, Token([]byte("key"), "uid-2", "ns-a", "sandbox-a"))
	assert.NotEqual(t, a, Token([]byte("key"), "uid-1", "ns-b", "sandbox-a"))
	assert.NotEqual(t, a, Token([]byte("key"), "uid-1", "ns-a", "sandbox-b"))
	assert.NotEqual(t, a, Token([]byte("other"), "uid-1", "ns-a", "sandbox-a"))
	assert.Len(t, a, 64) // hex sha256
}

func TestVerify(t *testing.T) {
	token := Token([]byte("k"), "uid", "ns", "vm")
	assert.True(t, Verify([]byte("k"), "uid", "ns", "vm", token))
	assert.False(t, Verify([]byte("k"), "uid", "ns", "vm", "deadbeef"))
	assert.False(t, Verify([]byte("k"), "uid", "other", "vm", token))
	assert.False(t, Verify([]byte("other"), "uid", "ns", "vm", token))
}

func TestGuestToken_isDistinctFromSessionToken(t *testing.T) {
	key := []byte("k")
	session := Token(key, "uid", "ns", "vm")
	guest := GuestToken(key, "uid", "ns", "vm")
	assert.NotEqual(t, session, guest)
	assert.NotEqual(t, guest, GuestToken(key, "uid", "ns", "other-vm"))
}

func TestVSOCKPath(t *testing.T) {
	assert.Equal(t,
		"/run/imp/team-a-sb1.vsock",
		VSOCKPath(SocketDirDefault, "team-a", "sb1"))
	assert.Equal(t,
		"/data/ns-vm.vsock",
		VSOCKPath("/data", "ns", "vm"))
}
