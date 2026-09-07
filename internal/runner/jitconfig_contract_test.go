package runner

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestJITConfigSecretValueRoundTrip(t *testing.T) {
	want := JITConfig{EncodedConfig: "encoded-jit", RunnerName: "runner-1"}
	payload, err := want.MarshalSecretValue()
	require.NoError(t, err)

	got, err := JITConfigFromSecretValue(JITConfigVersionV1, payload, want.RunnerName)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestJITConfigFromSecretValueAcceptsLegacyUnversionedPayload(t *testing.T) {
	got, err := JITConfigFromSecretValue("", []byte("legacy-jit"), "runner-1")
	require.NoError(t, err)
	require.Equal(t, "legacy-jit", got.EncodedConfig)
}

func TestJITConfigFromSecretValueRejectsUnsupportedVersion(t *testing.T) {
	_, err := JITConfigFromSecretValue("v2", []byte("encoded-jit"), "runner-1")
	require.ErrorContains(t, err, "unsupported JIT config version")
}

func TestJITConfigSecretValueRejectsEmptyPayload(t *testing.T) {
	_, err := (JITConfig{}).MarshalSecretValue()
	require.ErrorContains(t, err, "empty")
}
