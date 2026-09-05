package releaseprovenance

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

const (
	UpstreamManifestPath = "release/upstream-base.json"
	CanonicalUpstreamURL = "https://github.com/QuantumNous/new-api"
	CanonicalForkURL     = "https://github.com/mlhjyx/new-api"
	CanonicalOCIRepo     = "ghcr.io/mlhjyx/new-api"

	PinnedUpstreamCommit              = "bde9b2f44887d34ec54799ae191d50f97914359e"
	PinnedUpstreamGitTree             = "8d25730d7f58a83778ef23b3a8ccd255d2d91701"
	PinnedUpstreamTreeArchiveSHA256   = "07923f60654b9eadda476ea4f269524273b0edeb5287b50c602c4e6921afb6d4"
	PinnedUpstreamSourceArchiveSHA256 = "3f532d1b4f48153277342e98c54b06665b0c472118b0f032ddcc70233c288331"
	PinnedUpstreamLicenseSHA256       = "8486a10c4393cee1c25392769ddd3b2d6c242d6ec7928e1414efff7dfb2f07ef"
	PinnedUpstreamNoticeSHA256        = "528067fcdf4f9d7e3fdb489d02cbdd36a0efa63fc2eb1686340612c26beb9f33"
	PinnedUpstreamThirdPartySHA256    = "33d93b4c0522a727be82f1a0cd12b09d8b7d10ed8117529dc373f4d7e2f37aa3"
)

var (
	gitCommitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
	sha256Pattern    = regexp.MustCompile(`^[0-9a-f]{64}$`)
	ociDigestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

type UpstreamBase struct {
	SchemaVersion       string `json:"schema_version"`
	CanonicalURL        string `json:"canonical_url"`
	Commit              string `json:"commit"`
	GitTree             string `json:"git_tree"`
	TreeArchiveSHA256   string `json:"tree_archive_sha256"`
	SourceArchiveSHA256 string `json:"source_archive_sha256"`
	LicenseSHA256       string `json:"license_sha256"`
	NoticeSHA256        string `json:"notice_sha256"`
	ThirdPartySHA256    string `json:"third_party_sha256"`
}

type ForkIdentity struct {
	CanonicalURL      string `json:"canonical_url"`
	Commit            string `json:"commit"`
	GitTree           string `json:"git_tree"`
	PatchedTreeSHA256 string `json:"patched_tree_sha256"`
	PatchSeriesSHA256 string `json:"patch_series_sha256"`
}

type BuildIdentity struct {
	RecipeSHA256      string `json:"recipe_sha256"`
	ModuleGraphSHA256 string `json:"module_graph_sha256"`
}

type CorrespondingSourceIdentity struct {
	URI           string `json:"uri"`
	ArchiveURI    string `json:"archive_uri"`
	ArchiveSHA256 string `json:"archive_sha256"`
}

type LicenseIdentity struct {
	SPDX             string `json:"spdx"`
	LicenseSHA256    string `json:"license_sha256"`
	NoticeSHA256     string `json:"notice_sha256"`
	ThirdPartySHA256 string `json:"third_party_sha256"`
}

type SourceSBOMIdentity struct {
	SourceDependencySHA256 string `json:"source_dependency_sha256"`
}

type SourceProvenance struct {
	SchemaVersion       string                      `json:"schema_version"`
	Upstream            UpstreamBase                `json:"upstream"`
	Fork                ForkIdentity                `json:"fork"`
	Build               BuildIdentity               `json:"build"`
	CorrespondingSource CorrespondingSourceIdentity `json:"corresponding_source"`
	License             LicenseIdentity             `json:"license"`
	SBOM                SourceSBOMIdentity          `json:"sbom"`
}

type OCIRelease struct {
	Repository                 string `json:"repository"`
	Digest                     string `json:"digest"`
	ImageSBOMReference         string `json:"image_sbom_reference"`
	ImageSBOMAttestationSHA256 string `json:"image_sbom_attestation_sha256"`
}

type ReleaseReceipt struct {
	SchemaVersion string           `json:"schema_version"`
	Source        SourceProvenance `json:"source"`
	OCI           OCIRelease       `json:"oci"`
}

type SourceRequest struct {
	RepoDir        string
	Revision       string
	SourceURI      string
	ArchiveURI     string
	ArchivePath    string
	SourceSBOMPath string
	Progress       func(stage string)
}

func ValidateSourceURIs(revision string, sourceURI string, archiveURI string) error {
	if !gitCommitPattern.MatchString(revision) {
		return errors.New("source revision must be a lowercase full Git commit")
	}
	if hasHeaderControl(sourceURI) || hasHeaderControl(archiveURI) {
		return errors.New("source URI contains a header control character")
	}
	expectedSourceURI := CanonicalForkURL + "/tree/" + revision
	if sourceURI != expectedSourceURI {
		return fmt.Errorf("source URI must be the immutable fork revision %q", expectedSourceURI)
	}
	expectedArchiveURI := CanonicalForkURL + "/releases/download/source-" + revision + "/new-api-" + revision + ".tar.gz"
	if archiveURI != expectedArchiveURI {
		return fmt.Errorf("archive URI must be the immutable fork release asset %q", expectedArchiveURI)
	}
	for _, rawURI := range []string{sourceURI, archiveURI} {
		parsed, err := url.ParseRequestURI(rawURI)
		if err != nil || parsed.Scheme != "https" || parsed.Host != "github.com" || parsed.User != nil {
			return errors.New("source URIs must be HTTPS github.com URLs without userinfo")
		}
	}
	return nil
}

func FinalizeReleaseReceipt(source SourceProvenance, oci OCIRelease) (ReleaseReceipt, error) {
	if err := validateSourceProvenance(source); err != nil {
		return ReleaseReceipt{}, err
	}
	if err := validateOCIRelease(oci); err != nil {
		return ReleaseReceipt{}, err
	}
	return ReleaseReceipt{
		SchemaVersion: "new-api-release-receipt/v1",
		Source:        source,
		OCI:           oci,
	}, nil
}

func validateUpstreamBase(manifest UpstreamBase) error {
	if manifest.SchemaVersion != "new-api-upstream-base/v1" {
		return errors.New("upstream manifest schema version is invalid")
	}
	if manifest.CanonicalURL != CanonicalUpstreamURL {
		return errors.New("upstream canonical URL is invalid")
	}
	if !gitCommitPattern.MatchString(manifest.Commit) || !gitCommitPattern.MatchString(manifest.GitTree) {
		return errors.New("upstream commit and tree must be lowercase full Git object IDs")
	}
	for _, digest := range []string{
		manifest.TreeArchiveSHA256,
		manifest.SourceArchiveSHA256,
		manifest.LicenseSHA256,
		manifest.NoticeSHA256,
		manifest.ThirdPartySHA256,
	} {
		if !sha256Pattern.MatchString(digest) {
			return errors.New("upstream digest must be lowercase SHA-256")
		}
	}
	return nil
}

func validatePinnedUpstream(manifest UpstreamBase) error {
	expected := UpstreamBase{
		SchemaVersion:       "new-api-upstream-base/v1",
		CanonicalURL:        CanonicalUpstreamURL,
		Commit:              PinnedUpstreamCommit,
		GitTree:             PinnedUpstreamGitTree,
		TreeArchiveSHA256:   PinnedUpstreamTreeArchiveSHA256,
		SourceArchiveSHA256: PinnedUpstreamSourceArchiveSHA256,
		LicenseSHA256:       PinnedUpstreamLicenseSHA256,
		NoticeSHA256:        PinnedUpstreamNoticeSHA256,
		ThirdPartySHA256:    PinnedUpstreamThirdPartySHA256,
	}
	if manifest != expected {
		return errors.New("upstream manifest differs from the approved pinned base")
	}
	return nil
}

func validateSourceProvenance(provenance SourceProvenance) error {
	if provenance.SchemaVersion != "new-api-source-provenance/v1" {
		return errors.New("source provenance schema version is invalid")
	}
	if err := validateUpstreamBase(provenance.Upstream); err != nil {
		return err
	}
	if provenance.Fork.CanonicalURL != CanonicalForkURL ||
		!gitCommitPattern.MatchString(provenance.Fork.Commit) ||
		!gitCommitPattern.MatchString(provenance.Fork.GitTree) {
		return errors.New("fork identity is invalid")
	}
	for _, digest := range []string{
		provenance.Fork.PatchedTreeSHA256,
		provenance.Fork.PatchSeriesSHA256,
		provenance.Build.RecipeSHA256,
		provenance.Build.ModuleGraphSHA256,
		provenance.CorrespondingSource.ArchiveSHA256,
		provenance.License.LicenseSHA256,
		provenance.License.NoticeSHA256,
		provenance.License.ThirdPartySHA256,
		provenance.SBOM.SourceDependencySHA256,
	} {
		if !sha256Pattern.MatchString(digest) {
			return errors.New("source provenance digest must be lowercase SHA-256")
		}
	}
	if err := ValidateSourceURIs(provenance.Fork.Commit, provenance.CorrespondingSource.URI, provenance.CorrespondingSource.ArchiveURI); err != nil {
		return err
	}
	if provenance.License.SPDX != "AGPL-3.0-only" ||
		provenance.License.LicenseSHA256 != provenance.Upstream.LicenseSHA256 {
		return errors.New("release license identity does not preserve the pinned AGPL license")
	}
	return nil
}

func validateOCIRelease(oci OCIRelease) error {
	if oci.Repository != CanonicalOCIRepo {
		return errors.New("OCI repository must be the authorized fork namespace")
	}
	if !ociDigestPattern.MatchString(oci.Digest) {
		return errors.New("OCI digest must be an exact sha256 digest")
	}
	expectedReference := CanonicalOCIRepo + "@" + oci.Digest
	if oci.ImageSBOMReference != expectedReference {
		return errors.New("image SBOM reference must bind the same exact image digest")
	}
	if !sha256Pattern.MatchString(oci.ImageSBOMAttestationSHA256) {
		return errors.New("image SBOM attestation digest must be lowercase SHA-256")
	}
	return nil
}

func hasHeaderControl(value string) bool {
	return strings.ContainsAny(value, "\r\n")
}
