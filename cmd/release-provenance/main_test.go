package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunVerifiesThePinnedUpstreamManifest(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := run([]string{"verify-upstream", "--repo", filepath.Clean(filepath.Join("..", ".."))}, &stdout, &stderr)

	assert.Equal(t, 0, exitCode)
	assert.Equal(t, "633b4c5b190dc1af99a2814391354d86948e7943\n", stdout.String())
	assert.Empty(t, stderr.String())
}

func TestRunRejectsUnknownCommandsWithoutLeakingArguments(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	secret := "must-not-be-echoed"

	exitCode := run([]string{"unknown", secret}, &stdout, &stderr)

	assert.Equal(t, 2, exitCode)
	assert.Empty(t, stdout.String())
	assert.NotContains(t, stderr.String(), secret)
}

func TestWriteExclusiveDoesNotReplaceExistingEvidence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "receipt.json")
	require.NoError(t, os.WriteFile(path, []byte("original\n"), 0o644))

	err := writeExclusive(path, []byte("replacement\n"))

	assert.Error(t, err)
	data, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, "original\n", string(data))
}

func TestPrepareSourceRejectsAnUnboundedTimeout(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := run([]string{"prepare-source", "--timeout", "0s"}, &stdout, &stderr)

	assert.Equal(t, 2, exitCode)
	assert.Empty(t, stdout.String())
	assert.Contains(t, stderr.String(), "timeout must be between 1s and 10m")
}
