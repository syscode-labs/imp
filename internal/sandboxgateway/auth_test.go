package sandboxgateway

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestToken_deterministicAndDistinct(t *testing.T) {
	a := Token([]byte("key"), "uid-1")
	assert.Equal(t, a, Token([]byte("key"), "uid-1"))
	assert.NotEqual(t, a, Token([]byte("key"), "uid-2"))
	assert.NotEqual(t, a, Token([]byte("other"), "uid-1"))
	assert.Len(t, a, 64) // hex sha256
}

func TestVerify(t *testing.T) {
	assert.True(t, Verify([]byte("k"), "uid", Token([]byte("k"), "uid")))
	assert.False(t, Verify([]byte("k"), "uid", "deadbeef"))
	assert.False(t, Verify([]byte("other"), "uid", Token([]byte("k"), "uid")))
}

func TestVSOCKPath(t *testing.T) {
	assert.Equal(t,
		"/run/imp/team-a-sb1.vsock",
		VSOCKPath(SocketDirDefault, "team-a", "sb1"))
	assert.Equal(t,
		"/data/ns-vm.vsock",
		VSOCKPath("/data", "ns", "vm"))
}
