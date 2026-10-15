package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/internal/releaseprovenance"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunVerifiesThePinnedUpstreamManifest(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	repo := filepath.Clean(filepath.Join("..", ".."))
	skipShallowCheckoutWithoutPinnedBase(t, repo)

	exitCode := run([]string{"verify-upstream", "--repo", repo}, &stdout, &stderr)

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

// skipShallowCheckoutWithoutPinnedBase lets upstream's shallow CI checkout
// (actions/checkout fetch-depth: 1 in ci.yml) run the suite. The fork release
// workflows check out full history, where the pinned base must resolve, so
// this never skips there and the gate stays fail-closed.
func skipShallowCheckoutWithoutPinnedBase(t *testing.T, repo string) {
	t.Helper()
	shallow, err := exec.Command("git", "-C", repo, "rev-parse", "--is-shallow-repository").Output()
	if err != nil || strings.TrimSpace(string(shallow)) != "true" {
		return
	}
	if exec.Command("git", "-C", repo, "cat-file", "-e", releaseprovenance.PinnedUpstreamCommit+"^{commit}").Run() == nil {
		return
	}
	t.Skip("shallow checkout lacks the pinned upstream base; the fork release workflow verifies it with full history")
}
