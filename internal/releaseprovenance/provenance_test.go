package releaseprovenance

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testBaseArchiveSHA = "3f532d1b4f48153277342e98c54b06665b0c472118b0f032ddcc70233c288331"
	testBaseCommit     = "bde9b2f44887d34ec54799ae191d50f97914359e"
)

func TestPinnedUpstreamBaseMatchesApprovedGitObject(t *testing.T) {
	repo := filepath.Clean(filepath.Join("..", ".."))

	manifest, err := VerifyPinnedUpstream(context.Background(), repo)
	require.NoError(t, err)
	assert.Equal(t, "https://github.com/QuantumNous/new-api", manifest.CanonicalURL)
	assert.Equal(t, testBaseCommit, manifest.Commit)
	assert.Equal(t, "8d25730d7f58a83778ef23b3a8ccd255d2d91701", manifest.GitTree)
	assert.Equal(t, testBaseArchiveSHA, manifest.SourceArchiveSHA256)
	assert.Equal(t, "8486a10c4393cee1c25392769ddd3b2d6c242d6ec7928e1414efff7dfb2f07ef", manifest.LicenseSHA256)
	assert.Equal(t, "528067fcdf4f9d7e3fdb489d02cbdd36a0efa63fc2eb1686340612c26beb9f33", manifest.NoticeSHA256)
	assert.Equal(t, "33d93b4c0522a727be82f1a0cd12b09d8b7d10ed8117529dc373f4d7e2f37aa3", manifest.ThirdPartySHA256)
}

func TestGenerateSourceProvenanceBindsCleanExactRevision(t *testing.T) {
	fixture := newGitFixture(t)
	revision := fixture.releaseCommit(t)
	sourceSBOM := fixture.externalFile(t, "source-sbom.spdx.json", []byte(`{"spdxVersion":"SPDX-2.3"}`))
	archivePath := filepath.Join(t.TempDir(), "new-api-"+revision+".tar.gz")

	provenance, err := GenerateSource(context.Background(), SourceRequest{
		RepoDir:        fixture.dir,
		Revision:       revision,
		SourceURI:      "https://github.com/mlhjyx/new-api/tree/" + revision,
		ArchiveURI:     "https://github.com/mlhjyx/new-api/releases/download/source-" + revision + "/new-api-" + revision + ".tar.gz",
		ArchivePath:    archivePath,
		SourceSBOMPath: sourceSBOM,
	})
	require.NoError(t, err)

	assert.Equal(t, "new-api-source-provenance/v1", provenance.SchemaVersion)
	assert.Equal(t, fixture.baseCommit, provenance.Upstream.Commit)
	assert.Equal(t, revision, provenance.Fork.Commit)
	assert.Equal(t, fixture.git(t, "rev-parse", revision+"^{tree}"), provenance.Fork.GitTree)
	assert.Equal(t, sha256File(t, archivePath), provenance.CorrespondingSource.ArchiveSHA256)
	assert.Equal(t, sha256File(t, sourceSBOM), provenance.SBOM.SourceDependencySHA256)
	assert.Equal(t, fixture.gitObjectFileSHA(t, revision, "LICENSE"), provenance.License.LicenseSHA256)
	assert.Equal(t, fixture.gitObjectFileSHA(t, revision, "NOTICE"), provenance.License.NoticeSHA256)
	assert.Equal(t, fixture.gitObjectFileSHA(t, revision, "THIRD-PARTY-LICENSES.md"), provenance.License.ThirdPartySHA256)
	assert.Regexp(t, `^[0-9a-f]{64}$`, provenance.Fork.PatchSeriesSHA256)
	assert.Regexp(t, `^[0-9a-f]{64}$`, provenance.Fork.PatchedTreeSHA256)
	assert.Regexp(t, `^[0-9a-f]{64}$`, provenance.Build.RecipeSHA256)
	assert.Regexp(t, `^[0-9a-f]{64}$`, provenance.Build.ModuleGraphSHA256)

	bytesOne, err := MarshalSourceProvenance(provenance)
	require.NoError(t, err)
	bytesTwo, err := MarshalSourceProvenance(provenance)
	require.NoError(t, err)
	assert.Equal(t, bytesOne, bytesTwo)

	decoded, err := DecodeSourceProvenance(bytesOne)
	require.NoError(t, err)
	assert.Equal(t, provenance, decoded)
}

func TestGenerateSourceProvenanceFailsClosedForDirtyOrIncompleteInputs(t *testing.T) {
	fixture := newGitFixture(t)
	revision := fixture.releaseCommit(t)
	sourceSBOM := fixture.externalFile(t, "source-sbom.spdx.json", []byte(`{"spdxVersion":"SPDX-2.3"}`))
	request := SourceRequest{
		RepoDir:        fixture.dir,
		Revision:       revision,
		SourceURI:      "https://github.com/mlhjyx/new-api/tree/" + revision,
		ArchiveURI:     "https://github.com/mlhjyx/new-api/releases/download/source-" + revision + "/new-api-" + revision + ".tar.gz",
		ArchivePath:    filepath.Join(t.TempDir(), "source.tar.gz"),
		SourceSBOMPath: sourceSBOM,
	}

	require.NoError(t, os.WriteFile(filepath.Join(fixture.dir, "Dockerfile"), []byte("dirty\n"), 0o644))
	_, err := GenerateSource(context.Background(), request)
	assert.ErrorContains(t, err, "clean")

	fixture.git(t, "checkout", "--", "Dockerfile")
	require.NoError(t, os.Remove(filepath.Join(fixture.dir, "go.sum")))
	fixture.git(t, "add", "go.sum")
	fixture.git(t, "commit", "-m", "test: omit locked build input")
	request.Revision = fixture.git(t, "rev-parse", "HEAD")
	request.SourceURI = "https://github.com/mlhjyx/new-api/tree/" + request.Revision
	request.ArchiveURI = "https://github.com/mlhjyx/new-api/releases/download/source-" + request.Revision + "/new-api-" + request.Revision + ".tar.gz"
	request.ArchivePath = filepath.Join(t.TempDir(), "missing-lock.tar.gz")
	_, err = GenerateSource(context.Background(), request)
	assert.ErrorContains(t, err, "go.sum")
}

func TestBuildRecipeDigestCoversTheForkReleaseWorkflow(t *testing.T) {
	fixture := newGitFixture(t)
	firstRevision := fixture.releaseCommit(t)
	firstSBOM := fixture.externalFile(t, "first.spdx.json", []byte(`{"spdxVersion":"SPDX-2.3"}`))
	first, err := GenerateSource(context.Background(), SourceRequest{
		RepoDir:        fixture.dir,
		Revision:       firstRevision,
		SourceURI:      "https://github.com/mlhjyx/new-api/tree/" + firstRevision,
		ArchiveURI:     "https://github.com/mlhjyx/new-api/releases/download/source-" + firstRevision + "/new-api-" + firstRevision + ".tar.gz",
		ArchivePath:    filepath.Join(t.TempDir(), "first.tar.gz"),
		SourceSBOMPath: firstSBOM,
	})
	require.NoError(t, err)

	fixture.write(t, ".github/workflows/growthos-new-api-release.yml", "name: changed release recipe\n")
	fixture.git(t, "add", ".github/workflows/growthos-new-api-release.yml")
	fixture.git(t, "commit", "-q", "-m", "ci: change release recipe")
	secondRevision := fixture.git(t, "rev-parse", "HEAD")
	secondSBOM := fixture.externalFile(t, "second.spdx.json", []byte(`{"spdxVersion":"SPDX-2.3"}`))
	second, err := GenerateSource(context.Background(), SourceRequest{
		RepoDir:        fixture.dir,
		Revision:       secondRevision,
		SourceURI:      "https://github.com/mlhjyx/new-api/tree/" + secondRevision,
		ArchiveURI:     "https://github.com/mlhjyx/new-api/releases/download/source-" + secondRevision + "/new-api-" + secondRevision + ".tar.gz",
		ArchivePath:    filepath.Join(t.TempDir(), "second.tar.gz"),
		SourceSBOMPath: secondSBOM,
	})
	require.NoError(t, err)
	assert.NotEqual(t, first.Build.RecipeSHA256, second.Build.RecipeSHA256)
}

func TestGenerateSourceProvenanceRejectsNonDescendantRevision(t *testing.T) {
	fixture := newGitFixture(t)
	fixture.releaseCommit(t)
	fixture.git(t, "checkout", "--orphan", "unrelated")
	require.NoError(t, os.RemoveAll(filepath.Join(fixture.dir, "release")))
	fixture.writeRequiredBuildFiles(t)
	fixture.writeManifest(t)
	fixture.git(t, "add", ".")
	fixture.git(t, "commit", "-m", "test: unrelated release")
	revision := fixture.git(t, "rev-parse", "HEAD")
	sourceSBOM := fixture.externalFile(t, "source-sbom.spdx.json", []byte(`{"spdxVersion":"SPDX-2.3"}`))

	_, err := GenerateSource(context.Background(), SourceRequest{
		RepoDir:        fixture.dir,
		Revision:       revision,
		SourceURI:      "https://github.com/mlhjyx/new-api/tree/" + revision,
		ArchiveURI:     "https://github.com/mlhjyx/new-api/releases/download/source-" + revision + "/new-api-" + revision + ".tar.gz",
		ArchivePath:    filepath.Join(t.TempDir(), "source.tar.gz"),
		SourceSBOMPath: sourceSBOM,
	})
	assert.ErrorContains(t, err, "descendant")
}

func TestSourceIdentityRejectsMutableOrUnsafeURIs(t *testing.T) {
	revision := strings.Repeat("a", 40)
	validArchive := "https://github.com/mlhjyx/new-api/releases/download/source-" + revision + "/new-api-" + revision + ".tar.gz"
	tests := []struct {
		name       string
		sourceURI  string
		archiveURI string
	}{
		{name: "main", sourceURI: "https://github.com/mlhjyx/new-api/tree/main", archiveURI: validArchive},
		{name: "tag", sourceURI: "https://github.com/mlhjyx/new-api/tree/v1", archiveURI: validArchive},
		{name: "latest", sourceURI: "https://github.com/mlhjyx/new-api/tree/latest", archiveURI: validArchive},
		{name: "wrong repository", sourceURI: "https://github.com/QuantumNous/new-api/tree/" + revision, archiveURI: validArchive},
		{name: "http", sourceURI: "http://github.com/mlhjyx/new-api/tree/" + revision, archiveURI: validArchive},
		{name: "header injection", sourceURI: "https://github.com/mlhjyx/new-api/tree/" + revision + "\r\nX-Evil: 1", archiveURI: validArchive},
		{name: "mutable archive", sourceURI: "https://github.com/mlhjyx/new-api/tree/" + revision, archiveURI: "https://github.com/mlhjyx/new-api/archive/main.tar.gz"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Error(t, ValidateSourceURIs(revision, test.sourceURI, test.archiveURI))
		})
	}
	assert.NoError(t, ValidateSourceURIs(revision, "https://github.com/mlhjyx/new-api/tree/"+revision, validArchive))
}

func TestClosedProvenanceAndReceiptRejectUnknownOrMissingFields(t *testing.T) {
	assert.Error(t, func() error {
		_, err := DecodeSourceProvenance([]byte(`{"schema_version":"new-api-source-provenance/v1","unexpected":true}`))
		return err
	}())

	assert.Error(t, func() error {
		_, err := DecodeReleaseReceipt([]byte(`{"schema_version":"new-api-release-receipt/v1","source":{},"oci":{},"unexpected":true}`))
		return err
	}())

	duplicate := []byte(`{"schema_version":"new-api-upstream-base/v1","canonical_url":"https://github.com/QuantumNous/new-api","canonical_url":"https://github.com/QuantumNous/new-api","commit":"bde9b2f44887d34ec54799ae191d50f97914359e","git_tree":"8d25730d7f58a83778ef23b3a8ccd255d2d91701","tree_archive_sha256":"07923f60654b9eadda476ea4f269524273b0edeb5287b50c602c4e6921afb6d4","source_archive_sha256":"3f532d1b4f48153277342e98c54b06665b0c472118b0f032ddcc70233c288331","license_sha256":"8486a10c4393cee1c25392769ddd3b2d6c242d6ec7928e1414efff7dfb2f07ef","notice_sha256":"528067fcdf4f9d7e3fdb489d02cbdd36a0efa63fc2eb1686340612c26beb9f33","third_party_sha256":"33d93b4c0522a727be82f1a0cd12b09d8b7d10ed8117529dc373f4d7e2f37aa3"}`)
	_, err := DecodeUpstreamBase(duplicate)
	assert.ErrorContains(t, err, "duplicate")
}

func TestFinalizeReleaseReceiptAcceptsOnlyExactForkDigestAndAttestation(t *testing.T) {
	source := validSourceProvenanceFixture()
	imageDigest := "sha256:" + strings.Repeat("1", 64)
	imageSBOMDigest := "sha256:" + strings.Repeat("2", 64)
	receipt, err := FinalizeReleaseReceipt(source, OCIRelease{
		Repository:                 "ghcr.io/mlhjyx/new-api",
		Digest:                     imageDigest,
		ImageSBOMReference:         "ghcr.io/mlhjyx/new-api@" + imageSBOMDigest,
		ImageSBOMAttestationSHA256: strings.Repeat("3", 64),
	})
	require.NoError(t, err)
	assert.Equal(t, "new-api-release-receipt/v1", receipt.SchemaVersion)
	assert.Equal(t, imageDigest, receipt.OCI.Digest)

	encoded, err := MarshalReleaseReceipt(receipt)
	require.NoError(t, err)
	decoded, err := DecodeReleaseReceipt(encoded)
	require.NoError(t, err)
	assert.Equal(t, receipt, decoded)

	invalid := []OCIRelease{
		{Repository: "calciumion/new-api", Digest: imageDigest, ImageSBOMReference: "calciumion/new-api@" + imageSBOMDigest, ImageSBOMAttestationSHA256: strings.Repeat("3", 64)},
		{Repository: "ghcr.io/mlhjyx/new-api:latest", Digest: imageDigest, ImageSBOMReference: "ghcr.io/mlhjyx/new-api@" + imageSBOMDigest, ImageSBOMAttestationSHA256: strings.Repeat("3", 64)},
		{Repository: "ghcr.io/mlhjyx/new-api", Digest: "latest", ImageSBOMReference: "ghcr.io/mlhjyx/new-api@" + imageSBOMDigest, ImageSBOMAttestationSHA256: strings.Repeat("3", 64)},
		{Repository: "ghcr.io/mlhjyx/new-api", Digest: imageDigest, ImageSBOMReference: "ghcr.io/mlhjyx/new-api:latest", ImageSBOMAttestationSHA256: strings.Repeat("3", 64)},
	}
	for _, input := range invalid {
		_, err := FinalizeReleaseReceipt(source, input)
		assert.Error(t, err)
	}
}

func validSourceProvenanceFixture() SourceProvenance {
	revision := strings.Repeat("a", 40)
	digest := strings.Repeat("b", 64)
	return SourceProvenance{
		SchemaVersion: "new-api-source-provenance/v1",
		Upstream: UpstreamBase{
			SchemaVersion:       "new-api-upstream-base/v1",
			CanonicalURL:        "https://github.com/QuantumNous/new-api",
			Commit:              testBaseCommit,
			GitTree:             strings.Repeat("c", 40),
			TreeArchiveSHA256:   digest,
			SourceArchiveSHA256: testBaseArchiveSHA,
			LicenseSHA256:       digest,
			NoticeSHA256:        digest,
			ThirdPartySHA256:    digest,
		},
		Fork: ForkIdentity{
			CanonicalURL:      "https://github.com/mlhjyx/new-api",
			Commit:            revision,
			GitTree:           strings.Repeat("d", 40),
			PatchedTreeSHA256: digest,
			PatchSeriesSHA256: digest,
		},
		Build: BuildIdentity{RecipeSHA256: digest, ModuleGraphSHA256: digest},
		CorrespondingSource: CorrespondingSourceIdentity{
			URI:           "https://github.com/mlhjyx/new-api/tree/" + revision,
			ArchiveURI:    "https://github.com/mlhjyx/new-api/releases/download/source-" + revision + "/new-api-" + revision + ".tar.gz",
			ArchiveSHA256: digest,
		},
		License: LicenseIdentity{SPDX: "AGPL-3.0-only", LicenseSHA256: digest, NoticeSHA256: digest, ThirdPartySHA256: digest},
		SBOM:    SourceSBOMIdentity{SourceDependencySHA256: digest},
	}
}

type gitFixture struct {
	dir        string
	baseCommit string
}

func newGitFixture(t *testing.T) *gitFixture {
	t.Helper()
	fixture := &gitFixture{dir: t.TempDir()}
	fixture.git(t, "init", "-q")
	fixture.git(t, "config", "user.name", "Release Test")
	fixture.git(t, "config", "user.email", "release-test@example.invalid")
	fixture.writeRequiredBuildFiles(t)
	fixture.write(t, "LICENSE", "test license\n")
	fixture.write(t, "NOTICE", "test notice\n")
	fixture.write(t, "THIRD-PARTY-LICENSES.md", "test third-party\n")
	fixture.git(t, "add", ".")
	fixture.git(t, "commit", "-q", "-m", "test: base")
	fixture.baseCommit = fixture.git(t, "rev-parse", "HEAD")
	return fixture
}

func (fixture *gitFixture) releaseCommit(t *testing.T) string {
	t.Helper()
	fixture.writeManifest(t)
	fixture.write(t, "feature.go", "package feature\n")
	fixture.write(t, "NOTICE", "test notice\n\nfork modification\n")
	fixture.write(t, "THIRD-PARTY-LICENSES.md", "test third-party\n\nupdated fork inventory\n")
	fixture.git(t, "add", ".")
	fixture.git(t, "commit", "-q", "-m", "feat: release fixture")
	return fixture.git(t, "rev-parse", "HEAD")
}

func (fixture *gitFixture) writeRequiredBuildFiles(t *testing.T) {
	t.Helper()
	fixture.write(t, "Dockerfile", "FROM scratch\n")
	fixture.write(t, ".dockerignore", ".git\n")
	fixture.write(t, ".github/workflows/growthos-new-api-release.yml", "name: release recipe\n")
	fixture.write(t, "go.mod", "module example.invalid/release-fixture\n\ngo 1.25.1\n")
	fixture.write(t, "go.sum", "")
	fixture.write(t, "web/package.json", "{}\n")
	fixture.write(t, "web/default/package.json", "{}\n")
	fixture.write(t, "web/classic/package.json", "{}\n")
	fixture.write(t, "web/bun.lock", "{}\n")
}

func (fixture *gitFixture) writeManifest(t *testing.T) {
	t.Helper()
	tree := fixture.git(t, "rev-parse", fixture.baseCommit+"^{tree}")
	treeArchive := fixture.gitArchiveSHA(t, fixture.baseCommit, "")
	sourceArchive := fixture.gitArchiveSHA(t, fixture.baseCommit, "new-api-"+fixture.baseCommit+"/")
	manifest := fmt.Sprintf(`{
  "schema_version": "new-api-upstream-base/v1",
  "canonical_url": "https://github.com/QuantumNous/new-api",
  "commit": %q,
  "git_tree": %q,
  "tree_archive_sha256": %q,
  "source_archive_sha256": %q,
  "license_sha256": %q,
  "notice_sha256": %q,
  "third_party_sha256": %q
}
`, fixture.baseCommit, tree, treeArchive, sourceArchive,
		fixture.gitObjectFileSHA(t, fixture.baseCommit, "LICENSE"),
		fixture.gitObjectFileSHA(t, fixture.baseCommit, "NOTICE"),
		fixture.gitObjectFileSHA(t, fixture.baseCommit, "THIRD-PARTY-LICENSES.md"))
	fixture.write(t, "release/upstream-base.json", manifest)
}

func (fixture *gitFixture) write(t *testing.T, name string, content string) {
	t.Helper()
	path := filepath.Join(fixture.dir, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func (fixture *gitFixture) externalFile(t *testing.T, name string, content []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, content, 0o644))
	return path
}

func (fixture *gitFixture) git(t *testing.T, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = fixture.dir
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s: %s", strings.Join(command.Args, " "), output)
	return strings.TrimSpace(string(output))
}

func (fixture *gitFixture) gitArchiveSHA(t *testing.T, revision string, prefix string) string {
	t.Helper()
	arguments := []string{"archive", "--format=tar"}
	if prefix != "" {
		arguments = append(arguments, "--prefix="+prefix)
	}
	arguments = append(arguments, revision)
	archive := exec.Command("git", arguments...)
	archive.Dir = fixture.dir
	if prefix == "" {
		output, err := archive.Output()
		require.NoError(t, err)
		digest := sha256.Sum256(output)
		return hex.EncodeToString(digest[:])
	}
	gzip := exec.Command("gzip", "-n")
	pipe, err := archive.StdoutPipe()
	require.NoError(t, err)
	gzip.Stdin = pipe
	var output bytes.Buffer
	gzip.Stdout = &output
	require.NoError(t, archive.Start())
	require.NoError(t, gzip.Run())
	require.NoError(t, archive.Wait())
	digest := sha256.Sum256(output.Bytes())
	return hex.EncodeToString(digest[:])
}

func (fixture *gitFixture) gitObjectFileSHA(t *testing.T, revision string, name string) string {
	t.Helper()
	command := exec.Command("git", "show", revision+":"+name)
	command.Dir = fixture.dir
	output, err := command.Output()
	require.NoError(t, err)
	digest := sha256.Sum256(output)
	return hex.EncodeToString(digest[:])
}

func sha256File(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}
