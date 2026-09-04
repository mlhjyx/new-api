package main

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestVerifyReportsApprovedReviewForPullRequestEvidence(t *testing.T) {
	repo := filepath.Clean(filepath.Join("..", ".."))
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := run([]string{"verify", "--repo", repo, "--allow-hold"}, &stdout, &stderr)

	assert.Equal(t, 0, exitCode)
	assert.JSONEq(t, `{"schema_version":"new-api-license-verification/v1","status":"APPROVED","unresolved_packages":0}`, stdout.String())
	assert.Empty(t, stderr.String())
}

func TestVerifyAcceptsApprovedReviewForRelease(t *testing.T) {
	repo := filepath.Clean(filepath.Join("..", ".."))
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := run([]string{"verify", "--repo", repo}, &stdout, &stderr)

	assert.Equal(t, 0, exitCode)
	assert.JSONEq(t, `{"schema_version":"new-api-license-verification/v1","status":"APPROVED","unresolved_packages":0}`, stdout.String())
	assert.Empty(t, stderr.String())
}
