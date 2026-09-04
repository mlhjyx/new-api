package licenseinventory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRepositoryDirectInventoryExactlyMatchesLocksAndReviewedTable(t *testing.T) {
	repo := filepath.Clean(filepath.Join("..", ".."))

	report, err := VerifyRepository(repo, true)

	require.NoError(t, err)
	assert.Equal(t, 59, report.GoDirect)
	assert.Equal(t, 73, report.DefaultWebDirect)
	assert.Equal(t, 51, report.ClassicWebDirect)
	assert.Equal(t, 3, report.ElectronDirect)
	assert.Equal(t, "HOLD", report.ReviewStatus)
	assert.Equal(t, 2, report.UnresolvedPackages)
}

func TestReleaseVerificationRejectsTheDocumentedLicenseHold(t *testing.T) {
	repo := filepath.Clean(filepath.Join("..", ".."))

	_, err := VerifyRepository(repo, false)

	assert.ErrorContains(t, err, "license review remains HOLD")
}

func TestInventoryRejectsVersionDriftOrMissingRows(t *testing.T) {
	tests := []struct {
		name        string
		oldValue    string
		newValue    string
		expectedErr string
	}{
		{name: "version drift", oldValue: "| backend     | production  | Go        | `github.com/waffo-com/waffo-go`", newValue: "| backend     | production  | Go        | `github.com/waffo-com/waffo-go-renamed`", expectedErr: "direct dependency inventory"},
		{name: "missing row", oldValue: "| web/default | production  | npm       | `react`", newValue: "| web/default | production  | npm       | `react-removed`", expectedErr: "direct dependency inventory"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := copyLicenseFixture(t)
			path := filepath.Join(repo, "THIRD-PARTY-LICENSES.md")
			replaceLicenseOnce(t, path, test.oldValue, test.newValue)

			_, err := VerifyRepository(repo, true)

			assert.ErrorContains(t, err, test.expectedErr)
		})
	}
}

func TestLicenseAndOriginalNoticeArePreserved(t *testing.T) {
	repo := copyLicenseFixture(t)
	licensePath := filepath.Join(repo, "LICENSE")
	require.NoError(t, os.WriteFile(licensePath, []byte("replacement license\n"), 0o644))
	_, err := VerifyRepository(repo, true)
	assert.ErrorContains(t, err, "AGPL license")

	repo = copyLicenseFixture(t)
	noticePath := filepath.Join(repo, "NOTICE")
	replaceLicenseOnce(t, noticePath, "Copyright (c) QuantumNous and contributors.", "Copyright removed.")
	_, err = VerifyRepository(repo, true)
	assert.ErrorContains(t, err, "original NOTICE")
}

func TestLicenseReviewIsClosedAndBindsExactUnresolvedPackages(t *testing.T) {
	repo := copyLicenseFixture(t)
	path := filepath.Join(repo, "release/license-review.json")
	replaceLicenseOnce(t, path, "\n}", ",\n  \"unexpected\": true\n}")
	_, err := VerifyRepository(repo, true)
	assert.ErrorContains(t, err, "closed license review")

	repo = copyLicenseFixture(t)
	path = filepath.Join(repo, "release/license-review.json")
	replaceLicenseOnce(t, path, "@splinetool/runtime", "@splinetool/runtime-other")
	_, err = VerifyRepository(repo, true)
	assert.ErrorContains(t, err, "unresolved package")
}

func copyLicenseFixture(t *testing.T) string {
	t.Helper()
	sourceRoot := filepath.Clean(filepath.Join("..", ".."))
	targetRoot := t.TempDir()
	for _, name := range []string{
		"go.mod",
		"web/bun.lock",
		"web/package.json",
		"web/default/package.json",
		"web/classic/package.json",
		"electron/package.json",
		"electron/package-lock.json",
		"LICENSE",
		"NOTICE",
		"THIRD-PARTY-LICENSES.md",
		"release/license-review.json",
	} {
		data, err := os.ReadFile(filepath.Join(sourceRoot, filepath.FromSlash(name)))
		require.NoError(t, err, name)
		target := filepath.Join(targetRoot, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
		require.NoError(t, os.WriteFile(target, data, 0o644))
	}
	return targetRoot
}

func replaceLicenseOnce(t *testing.T, path string, oldValue string, newValue string) {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	content := string(data)
	require.Equal(t, 1, strings.Count(content, oldValue), "fixture mutation must be exact")
	require.NoError(t, os.WriteFile(path, []byte(strings.Replace(content, oldValue, newValue, 1)), 0o644))
}
