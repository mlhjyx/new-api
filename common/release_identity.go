package common

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

const (
	releaseUpstreamURL           = "https://github.com/QuantumNous/new-api"
	releaseForkURL               = "https://github.com/mlhjyx/new-api"
	releaseUpstreamRevision      = "bde9b2f44887d34ec54799ae191d50f97914359e"
	releaseUpstreamGitTree       = "8d25730d7f58a83778ef23b3a8ccd255d2d91701"
	releaseUpstreamTreeSHA256    = "07923f60654b9eadda476ea4f269524273b0edeb5287b50c602c4e6921afb6d4"
	releaseUpstreamArchiveSHA256 = "3f532d1b4f48153277342e98c54b06665b0c472118b0f032ddcc70233c288331"
	releaseUpstreamLicenseSHA256 = "8486a10c4393cee1c25392769ddd3b2d6c242d6ec7928e1414efff7dfb2f07ef"
	releaseUpstreamNoticeSHA256  = "528067fcdf4f9d7e3fdb489d02cbdd36a0efa63fc2eb1686340612c26beb9f33"
	releaseUpstreamThirdPartySHA = "33d93b4c0522a727be82f1a0cd12b09d8b7d10ed8117529dc373f4d7e2f37aa3"
)

var releaseGitObjectPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
var releaseSHA256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// These strings are populated only by linker -X flags in the managed OCI
// build. Runtime configuration is intentionally not consulted.
var BuildReleaseProfile = "unmanaged"
var BuildReleaseVersion = ""
var BuildReleaseRevision = ""
var BuildReleaseGitTree = ""
var BuildUpstreamRevision = ""
var BuildPatchSeriesSHA256 = ""
var BuildPatchedTreeSHA256 = ""
var BuildRecipeSHA256 = ""
var BuildModuleGraphSHA256 = ""
var BuildCorrespondingSourceURI = ""
var BuildCorrespondingSourceArchiveURI = ""
var BuildCorrespondingSourceArchiveSHA256 = ""
var BuildSourceSBOMSHA256 = ""
var BuildLicenseSHA256 = ""
var BuildNoticeSHA256 = ""
var BuildThirdPartySHA256 = ""

type ReleaseUpstreamIdentity struct {
	CanonicalURL        string `json:"canonical_url"`
	Revision            string `json:"revision"`
	GitTree             string `json:"git_tree"`
	TreeArchiveSHA256   string `json:"tree_archive_sha256"`
	SourceArchiveSHA256 string `json:"source_archive_sha256"`
	LicenseSHA256       string `json:"license_sha256"`
	NoticeSHA256        string `json:"notice_sha256"`
	ThirdPartySHA256    string `json:"third_party_sha256"`
}

type ReleaseForkIdentity struct {
	CanonicalURL      string `json:"canonical_url"`
	Revision          string `json:"revision"`
	GitTree           string `json:"git_tree"`
	PatchedTreeSHA256 string `json:"patched_tree_sha256"`
	PatchSeriesSHA256 string `json:"patch_series_sha256"`
}

type ReleaseBuildIdentity struct {
	Version           string `json:"version"`
	RecipeSHA256      string `json:"recipe_sha256"`
	ModuleGraphSHA256 string `json:"module_graph_sha256"`
}

type ReleaseCorrespondingSource struct {
	URI           string `json:"uri"`
	ArchiveURI    string `json:"archive_uri"`
	ArchiveSHA256 string `json:"archive_sha256"`
}

type ReleaseLicenseIdentity struct {
	SPDX             string `json:"spdx"`
	LicenseSHA256    string `json:"license_sha256"`
	NoticeSHA256     string `json:"notice_sha256"`
	ThirdPartySHA256 string `json:"third_party_sha256"`
}

type ReleaseSBOMIdentity struct {
	SourceDependencySHA256 string `json:"source_dependency_sha256"`
}

type ReleaseIdentity struct {
	SchemaVersion       string                     `json:"schema_version"`
	Project             string                     `json:"project"`
	Upstream            ReleaseUpstreamIdentity    `json:"upstream"`
	Fork                ReleaseForkIdentity        `json:"fork"`
	Build               ReleaseBuildIdentity       `json:"build"`
	CorrespondingSource ReleaseCorrespondingSource `json:"corresponding_source"`
	License             ReleaseLicenseIdentity     `json:"license"`
	SBOM                ReleaseSBOMIdentity        `json:"sbom"`
}

func CurrentReleaseIdentity() (ReleaseIdentity, bool, error) {
	if BuildReleaseProfile == "unmanaged" || BuildReleaseProfile == "" {
		return ReleaseIdentity{}, false, nil
	}
	if BuildReleaseProfile != "managed" {
		return ReleaseIdentity{}, false, errors.New("release profile is invalid")
	}
	identity := ReleaseIdentity{
		SchemaVersion: "new-api-corresponding-source/v1",
		Project:       "new-api",
		Upstream: ReleaseUpstreamIdentity{
			CanonicalURL:        releaseUpstreamURL,
			Revision:            releaseUpstreamRevision,
			GitTree:             releaseUpstreamGitTree,
			TreeArchiveSHA256:   releaseUpstreamTreeSHA256,
			SourceArchiveSHA256: releaseUpstreamArchiveSHA256,
			LicenseSHA256:       releaseUpstreamLicenseSHA256,
			NoticeSHA256:        releaseUpstreamNoticeSHA256,
			ThirdPartySHA256:    releaseUpstreamThirdPartySHA,
		},
		Fork: ReleaseForkIdentity{
			CanonicalURL:      releaseForkURL,
			Revision:          BuildReleaseRevision,
			GitTree:           BuildReleaseGitTree,
			PatchedTreeSHA256: BuildPatchedTreeSHA256,
			PatchSeriesSHA256: BuildPatchSeriesSHA256,
		},
		Build: ReleaseBuildIdentity{
			Version:           BuildReleaseVersion,
			RecipeSHA256:      BuildRecipeSHA256,
			ModuleGraphSHA256: BuildModuleGraphSHA256,
		},
		CorrespondingSource: ReleaseCorrespondingSource{
			URI:           BuildCorrespondingSourceURI,
			ArchiveURI:    BuildCorrespondingSourceArchiveURI,
			ArchiveSHA256: BuildCorrespondingSourceArchiveSHA256,
		},
		License: ReleaseLicenseIdentity{
			SPDX:             "AGPL-3.0-only",
			LicenseSHA256:    BuildLicenseSHA256,
			NoticeSHA256:     BuildNoticeSHA256,
			ThirdPartySHA256: BuildThirdPartySHA256,
		},
		SBOM: ReleaseSBOMIdentity{SourceDependencySHA256: BuildSourceSBOMSHA256},
	}
	if err := validateReleaseIdentity(identity); err != nil {
		return ReleaseIdentity{}, true, err
	}
	return identity, true, nil
}

func ValidateManagedReleaseIdentity() error {
	_, _, err := CurrentReleaseIdentity()
	return err
}

func (identity ReleaseIdentity) LinkHeader() string {
	return "<" + identity.CorrespondingSource.URI + ">; rel=\"source\""
}

func validateReleaseIdentity(identity ReleaseIdentity) error {
	if identity.Fork.Revision != strings.TrimSpace(identity.Fork.Revision) ||
		!releaseGitObjectPattern.MatchString(identity.Fork.Revision) ||
		!releaseGitObjectPattern.MatchString(identity.Fork.GitTree) {
		return errors.New("release revision or Git tree is invalid")
	}
	if BuildUpstreamRevision != releaseUpstreamRevision {
		return errors.New("release upstream revision does not match the approved base")
	}
	if identity.Build.Version != "production-parity-"+identity.Fork.Revision {
		return errors.New("release version is not bound to the exact revision")
	}
	for _, digest := range []string{
		identity.Fork.PatchedTreeSHA256,
		identity.Fork.PatchSeriesSHA256,
		identity.Build.RecipeSHA256,
		identity.Build.ModuleGraphSHA256,
		identity.CorrespondingSource.ArchiveSHA256,
		identity.License.LicenseSHA256,
		identity.License.NoticeSHA256,
		identity.License.ThirdPartySHA256,
		identity.SBOM.SourceDependencySHA256,
	} {
		if !releaseSHA256Pattern.MatchString(digest) {
			return errors.New("release identity digest is invalid")
		}
	}
	if identity.License.LicenseSHA256 != releaseUpstreamLicenseSHA256 {
		return errors.New("managed release does not preserve the approved AGPL license")
	}
	if err := validateReleaseSourceURI(identity.Fork.Revision, identity.CorrespondingSource.URI, identity.CorrespondingSource.ArchiveURI); err != nil {
		return err
	}
	return nil
}

func validateReleaseSourceURI(revision string, sourceURI string, archiveURI string) error {
	if strings.ContainsAny(sourceURI, "\r\n") || strings.ContainsAny(archiveURI, "\r\n") {
		return errors.New("release source URI contains a header control character")
	}
	expectedSourceURI := releaseForkURL + "/tree/" + revision
	expectedArchiveURI := releaseForkURL + "/releases/download/source-" + revision + "/new-api-" + revision + ".tar.gz"
	if sourceURI != expectedSourceURI || archiveURI != expectedArchiveURI {
		return errors.New("release source URI is not bound to the exact fork revision")
	}
	for _, rawURI := range []string{sourceURI, archiveURI} {
		parsed, err := url.ParseRequestURI(rawURI)
		if err != nil || parsed.Scheme != "https" || parsed.Host != "github.com" || parsed.User != nil {
			return fmt.Errorf("release source URI is invalid")
		}
	}
	return nil
}
