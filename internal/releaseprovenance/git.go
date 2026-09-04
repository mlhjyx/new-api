package releaseprovenance

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

var buildRecipeFiles = []string{
	".dockerignore",
	"Dockerfile",
	UpstreamManifestPath,
}

var moduleGraphFiles = []string{
	"go.mod",
	"go.sum",
	"web/bun.lock",
	"web/classic/package.json",
	"web/default/package.json",
	"web/package.json",
}

func VerifyPinnedUpstream(ctx context.Context, repoDir string) (UpstreamBase, error) {
	manifest, err := loadUpstreamBase(repoDir)
	if err != nil {
		return UpstreamBase{}, err
	}
	if err := validatePinnedUpstream(manifest); err != nil {
		return UpstreamBase{}, err
	}
	if err := verifyUpstreamObject(ctx, repoDir, manifest); err != nil {
		return UpstreamBase{}, err
	}
	return manifest, nil
}

func GenerateSource(ctx context.Context, request SourceRequest) (SourceProvenance, error) {
	repoDir, err := filepath.Abs(request.RepoDir)
	if err != nil {
		return SourceProvenance{}, err
	}
	if err := requireCleanExactRevision(ctx, repoDir, request.Revision); err != nil {
		return SourceProvenance{}, err
	}
	if err := ValidateSourceURIs(request.Revision, request.SourceURI, request.ArchiveURI); err != nil {
		return SourceProvenance{}, err
	}
	if err := requireExternalOutput(repoDir, request.ArchivePath); err != nil {
		return SourceProvenance{}, err
	}
	if err := requireExternalInput(repoDir, request.SourceSBOMPath); err != nil {
		return SourceProvenance{}, err
	}

	upstream, err := loadUpstreamBase(repoDir)
	if err != nil {
		return SourceProvenance{}, err
	}
	if err := verifyUpstreamObject(ctx, repoDir, upstream); err != nil {
		return SourceProvenance{}, err
	}
	if err := gitRun(ctx, repoDir, "merge-base", "--is-ancestor", upstream.Commit, request.Revision); err != nil {
		return SourceProvenance{}, errors.New("release revision must be a descendant of the approved upstream base")
	}

	gitTree, err := gitOutput(ctx, repoDir, "rev-parse", request.Revision+"^{tree}")
	if err != nil {
		return SourceProvenance{}, err
	}
	patchedTreeSHA, err := gitArchiveDigest(ctx, repoDir, request.Revision, "", "")
	if err != nil {
		return SourceProvenance{}, err
	}
	patchSeriesSHA, err := gitStreamDigest(ctx, repoDir, "diff", "--binary", "--full-index", "--no-renames", upstream.Commit+".."+request.Revision)
	if err != nil {
		return SourceProvenance{}, err
	}
	recipeSHA, err := digestRepoFiles(repoDir, buildRecipeFiles)
	if err != nil {
		return SourceProvenance{}, err
	}
	moduleGraphSHA, err := digestModuleGraph(ctx, repoDir)
	if err != nil {
		return SourceProvenance{}, err
	}
	sourceSBOMSHA, err := digestRegularFile(request.SourceSBOMPath)
	if err != nil {
		return SourceProvenance{}, fmt.Errorf("source SBOM: %w", err)
	}
	archiveSHA, err := gitArchiveDigest(ctx, repoDir, request.Revision, "new-api-"+request.Revision+"/", request.ArchivePath)
	if err != nil {
		return SourceProvenance{}, err
	}

	releaseLicenseSHA, err := gitObjectDigest(ctx, repoDir, request.Revision, "LICENSE")
	if err != nil {
		return SourceProvenance{}, err
	}
	releaseNoticeSHA, err := gitObjectDigest(ctx, repoDir, request.Revision, "NOTICE")
	if err != nil {
		return SourceProvenance{}, err
	}
	releaseThirdPartySHA, err := gitObjectDigest(ctx, repoDir, request.Revision, "THIRD-PARTY-LICENSES.md")
	if err != nil {
		return SourceProvenance{}, err
	}

	provenance := SourceProvenance{
		SchemaVersion: "new-api-source-provenance/v1",
		Upstream:      upstream,
		Fork: ForkIdentity{
			CanonicalURL:      CanonicalForkURL,
			Commit:            request.Revision,
			GitTree:           strings.TrimSpace(gitTree),
			PatchedTreeSHA256: patchedTreeSHA,
			PatchSeriesSHA256: patchSeriesSHA,
		},
		Build: BuildIdentity{
			RecipeSHA256:      recipeSHA,
			ModuleGraphSHA256: moduleGraphSHA,
		},
		CorrespondingSource: CorrespondingSourceIdentity{
			URI:           request.SourceURI,
			ArchiveURI:    request.ArchiveURI,
			ArchiveSHA256: archiveSHA,
		},
		License: LicenseIdentity{
			SPDX:             "AGPL-3.0-only",
			LicenseSHA256:    releaseLicenseSHA,
			NoticeSHA256:     releaseNoticeSHA,
			ThirdPartySHA256: releaseThirdPartySHA,
		},
		SBOM: SourceSBOMIdentity{SourceDependencySHA256: sourceSBOMSHA},
	}
	if err := validateSourceProvenance(provenance); err != nil {
		return SourceProvenance{}, err
	}
	return provenance, nil
}

func VerifyReleaseReceipt(ctx context.Context, repoDir string, receipt ReleaseReceipt, sourceSBOMPath string) error {
	if receipt.SchemaVersion != "new-api-release-receipt/v1" {
		return errors.New("release receipt schema version is invalid")
	}
	if err := validateOCIRelease(receipt.OCI); err != nil {
		return err
	}
	if _, err := VerifyPinnedUpstream(ctx, repoDir); err != nil {
		return err
	}
	temporaryDir, err := os.MkdirTemp("", "new-api-release-verify-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temporaryDir)
	recomputed, err := GenerateSource(ctx, SourceRequest{
		RepoDir:        repoDir,
		Revision:       receipt.Source.Fork.Commit,
		SourceURI:      receipt.Source.CorrespondingSource.URI,
		ArchiveURI:     receipt.Source.CorrespondingSource.ArchiveURI,
		ArchivePath:    filepath.Join(temporaryDir, "source.tar.gz"),
		SourceSBOMPath: sourceSBOMPath,
	})
	if err != nil {
		return err
	}
	if recomputed != receipt.Source {
		return errors.New("release receipt source provenance does not match the exact repository")
	}
	return nil
}

func loadUpstreamBase(repoDir string) (UpstreamBase, error) {
	data, err := os.ReadFile(filepath.Join(repoDir, filepath.FromSlash(UpstreamManifestPath)))
	if err != nil {
		return UpstreamBase{}, fmt.Errorf("read upstream manifest: %w", err)
	}
	return DecodeUpstreamBase(data)
}

func verifyUpstreamObject(ctx context.Context, repoDir string, manifest UpstreamBase) error {
	gitTree, err := gitOutput(ctx, repoDir, "rev-parse", manifest.Commit+"^{tree}")
	if err != nil {
		return err
	}
	if strings.TrimSpace(gitTree) != manifest.GitTree {
		return errors.New("upstream Git tree digest mismatch")
	}
	treeArchiveSHA, err := gitArchiveDigest(ctx, repoDir, manifest.Commit, "", "")
	if err != nil {
		return err
	}
	if treeArchiveSHA != manifest.TreeArchiveSHA256 {
		return errors.New("upstream tree archive digest mismatch")
	}
	sourceArchiveSHA, err := gitArchiveDigest(ctx, repoDir, manifest.Commit, "new-api-"+manifest.Commit+"/", "")
	if err != nil {
		return err
	}
	if sourceArchiveSHA != manifest.SourceArchiveSHA256 {
		return errors.New("upstream source archive digest mismatch")
	}
	for name, expected := range map[string]string{
		"LICENSE":                 manifest.LicenseSHA256,
		"NOTICE":                  manifest.NoticeSHA256,
		"THIRD-PARTY-LICENSES.md": manifest.ThirdPartySHA256,
	} {
		actual, err := gitObjectDigest(ctx, repoDir, manifest.Commit, name)
		if err != nil {
			return err
		}
		if actual != expected {
			return fmt.Errorf("upstream %s digest mismatch", name)
		}
	}
	return nil
}

func requireCleanExactRevision(ctx context.Context, repoDir string, revision string) error {
	if !gitCommitPattern.MatchString(revision) {
		return errors.New("release revision must be a lowercase full Git commit")
	}
	head, err := gitOutput(ctx, repoDir, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	if strings.TrimSpace(head) != revision {
		return errors.New("release revision must equal the checked-out HEAD")
	}
	status, err := gitOutput(ctx, repoDir, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return err
	}
	if strings.TrimSpace(status) != "" {
		return errors.New("release source tree must be clean")
	}
	return nil
}

func requireExternalOutput(repoDir string, path string) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("archive output path is required")
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(repoDir, absPath)
	if err != nil {
		return err
	}
	if relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return errors.New("archive output must be outside the clean source repository")
	}
	if _, err := os.Lstat(absPath); !errors.Is(err, os.ErrNotExist) {
		if err == nil {
			return errors.New("archive output already exists")
		}
		return err
	}
	return nil
}

func requireExternalInput(repoDir string, path string) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(repoDir, absPath)
	if err != nil {
		return err
	}
	if relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return errors.New("source SBOM must be generated outside the clean source repository")
	}
	return nil
}

func gitArchiveDigest(ctx context.Context, repoDir string, revision string, prefix string, outputPath string) (string, error) {
	arguments := []string{"archive", "--format=tar"}
	if prefix != "" {
		arguments = append(arguments, "--prefix="+prefix)
	}
	arguments = append(arguments, revision)
	archive := exec.CommandContext(ctx, "git", arguments...)
	archive.Dir = repoDir
	hasher := sha256.New()

	if prefix == "" {
		archive.Stdout = hasher
		var stderr bytes.Buffer
		archive.Stderr = &stderr
		if err := archive.Run(); err != nil {
			return "", fmt.Errorf("git archive: %w: %s", err, boundedText(stderr.Bytes()))
		}
		return hex.EncodeToString(hasher.Sum(nil)), nil
	}

	pipe, err := archive.StdoutPipe()
	if err != nil {
		return "", err
	}
	gzip := exec.CommandContext(ctx, "gzip", "-n")
	gzip.Stdin = pipe
	var output io.Writer = hasher
	var destination *os.File
	partialPath := ""
	if outputPath != "" {
		partialPath = outputPath + ".partial"
		destination, err = os.OpenFile(partialPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			return "", err
		}
		defer func() {
			destination.Close()
			os.Remove(partialPath)
		}()
		output = io.MultiWriter(hasher, destination)
	}
	gzip.Stdout = output
	var archiveError bytes.Buffer
	var gzipError bytes.Buffer
	archive.Stderr = &archiveError
	gzip.Stderr = &gzipError
	if err := archive.Start(); err != nil {
		return "", err
	}
	if err := gzip.Run(); err != nil {
		_ = archive.Wait()
		return "", fmt.Errorf("gzip source archive: %w: %s", err, boundedText(gzipError.Bytes()))
	}
	if err := archive.Wait(); err != nil {
		return "", fmt.Errorf("git archive: %w: %s", err, boundedText(archiveError.Bytes()))
	}
	if destination != nil {
		if err := destination.Sync(); err != nil {
			return "", err
		}
		if err := destination.Close(); err != nil {
			return "", err
		}
		if err := os.Rename(partialPath, outputPath); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func gitStreamDigest(ctx context.Context, repoDir string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = repoDir
	hasher := sha256.New()
	command.Stdout = hasher
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, boundedText(stderr.Bytes()))
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func gitObjectDigest(ctx context.Context, repoDir string, revision string, path string) (string, error) {
	return gitStreamDigest(ctx, repoDir, "show", revision+":"+path)
}

func digestRepoFiles(repoDir string, names []string) (string, error) {
	hasher := sha256.New()
	for _, name := range names {
		path := filepath.Join(repoDir, filepath.FromSlash(name))
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read required release input %s: %w", name, err)
		}
		fmt.Fprintf(hasher, "%s\x00%d\x00", name, len(data))
		if _, err := hasher.Write(data); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func digestModuleGraph(ctx context.Context, repoDir string) (string, error) {
	fileDigest, err := digestRepoFiles(repoDir, moduleGraphFiles)
	if err != nil {
		return "", err
	}
	graph, err := commandOutput(ctx, repoDir, "go", "mod", "graph")
	if err != nil {
		return "", err
	}
	lines := strings.Split(strings.TrimSpace(graph), "\n")
	sort.Strings(lines)
	hasher := sha256.New()
	fmt.Fprintf(hasher, "lock-files-sha256\x00%s\x00go-mod-graph\x00", fileDigest)
	for _, line := range lines {
		if line == "" {
			continue
		}
		fmt.Fprintf(hasher, "%s\n", line)
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func digestRegularFile(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("release digest input must be a regular non-symlink file")
	}
	if info.Size() > 64*1024*1024 {
		return "", errors.New("release digest input exceeds 64 MiB")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func gitRun(ctx context.Context, repoDir string, args ...string) error {
	_, err := commandOutput(ctx, repoDir, "git", args...)
	return err
}

func gitOutput(ctx context.Context, repoDir string, args ...string) (string, error) {
	return commandOutput(ctx, repoDir, "git", args...)
}

func commandOutput(ctx context.Context, repoDir string, name string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, name, args...)
	command.Dir = repoDir
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, boundedText(output))
	}
	return strings.TrimSpace(string(output)), nil
}

func boundedText(data []byte) string {
	const limit = 1024
	if len(data) > limit {
		data = data[:limit]
	}
	return strings.TrimSpace(string(data))
}
