package releasepolicy

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	canonicalUpstreamRepository = "QuantumNous/new-api"
	canonicalForkRepository     = "mlhjyx/new-api"
	canonicalForkImage          = "ghcr.io/mlhjyx/new-api"
	exactReleaseBranch          = "production-parity/settlement-readback-v1"
	protectedReadmeSHA256       = "c5e9ae7fde68d582f1ebfdc191944e1e130443167c1253b94d658819ca53d418"
	protectedPRTemplateSHA256   = "50a9790f8b37ecc3328c6cc4bf1ec6d5d8c251e1b13e839e4f12bfaca5ae6afb"
	protectedGitleaksIgnoreSHA  = "ce9b9151dc515bc7abdfd40eb48e2d768fd222c776408cc067d4cbbc97ea0802"
	protectedGoVetBaselineSHA   = "785654b4c2591a7194630adabdeebeb0260c7b7ce5a923ca4055e9e8052ecde4"
	pinnedGitleaksImage         = "zricethezav/gitleaks:v8.30.0@sha256:691af3c7c5a48b16f187ce3446d5f194838f91238f27270ed36eef6359a574d9"
)

var actionReferencePattern = regexp.MustCompile(`(?m)^\s*-?\s*uses:\s*[^\s#]+@([^\s#]+)`)
var commitReferencePattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

type Report struct {
	ForkImageRepository                string
	ReleaseBranch                      string
	GuardedUpstreamWorkflows           int
	PinnedActionReferences             int
	RuntimeUser                        string
	RuntimeBase                        string
	BoundedModuleDownload              bool
	ChecksummedModuleProxy             bool
	LicenseHoldFailClosed              bool
	ProtectedIdentityPreserved         bool
	PullRequestTemplatePreserved       bool
	AIAssistanceDisclosed              bool
	SecretScanPinned                   bool
	SourceAndImageSBOMChecks           bool
	CorrespondingSourceSmoke           bool
	GoVetBaselineFailClosed            bool
	PrivateLobeAdapterBound            bool
	ForkWorkflowRegistrationDocumented bool
}

func VerifyRepository(repoDir string) (Report, error) {
	report := Report{
		ForkImageRepository: canonicalForkImage,
		ReleaseBranch:       exactReleaseBranch,
	}
	if err := verifyProtectedIdentity(repoDir); err != nil {
		return Report{}, err
	}
	report.ProtectedIdentityPreserved = true
	report.PullRequestTemplatePreserved = true
	report.AIAssistanceDisclosed = true
	runtimeBase, runtimeUser, err := verifyProductionDockerfile(repoDir)
	if err != nil {
		return Report{}, err
	}
	report.RuntimeBase = runtimeBase
	report.RuntimeUser = runtimeUser
	report.BoundedModuleDownload = true
	report.ChecksummedModuleProxy = true
	report.LicenseHoldFailClosed = true
	workflowDir := filepath.Join(repoDir, ".github", "workflows")
	entries, err := os.ReadDir(workflowDir)
	if err != nil {
		return Report{}, err
	}
	for _, entry := range entries {
		if entry.IsDir() || (filepath.Ext(entry.Name()) != ".yml" && filepath.Ext(entry.Name()) != ".yaml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(workflowDir, entry.Name()))
		if err != nil {
			return Report{}, err
		}
		matches := actionReferencePattern.FindAllSubmatch(data, -1)
		for _, match := range matches {
			if !commitReferencePattern.Match(match[1]) {
				return Report{}, fmt.Errorf("workflow %s action reference must use a verified 40-character commit", entry.Name())
			}
			report.PinnedActionReferences++
		}
	}

	for _, name := range []string{"docker-build.yml", "docker-image-branch.yml", "electron-build.yml", "release.yml"} {
		data, err := os.ReadFile(filepath.Join(workflowDir, name))
		if err != nil {
			return Report{}, err
		}
		if err := verifyEveryJobRepositoryGuard(data, canonicalUpstreamRepository); err != nil {
			return Report{}, fmt.Errorf("%s: %w", name, err)
		}
		report.GuardedUpstreamWorkflows++
	}

	prWorkflow, err := os.ReadFile(filepath.Join(workflowDir, "growthos-new-api-pr.yml"))
	if err != nil {
		return Report{}, fmt.Errorf("fork PR workflow: %w", err)
	}
	if err := verifyEveryJobRepositoryGuard(prWorkflow, canonicalForkRepository); err != nil {
		return Report{}, fmt.Errorf("fork PR workflow: %w", err)
	}
	prText := string(prWorkflow)
	if strings.Contains(prText, "calciumion/new-api") || !strings.Contains(prText, "push: false") {
		return Report{}, errors.New("fork PR workflow must not push or target an upstream namespace")
	}
	if strings.Contains(prText, "docker/login-action") {
		return Report{}, errors.New("fork PR workflow must not authenticate to a registry")
	}
	if !strings.Contains(prText, "release-license verify --repo . --allow-hold") {
		return Report{}, errors.New("fork PR workflow must surface the documented license HOLD")
	}
	if err := verifyForkPullRequestGates(prText); err != nil {
		return Report{}, err
	}
	report.SecretScanPinned = true
	report.SourceAndImageSBOMChecks = true
	report.CorrespondingSourceSmoke = true
	report.GoVetBaselineFailClosed = true
	report.PrivateLobeAdapterBound = true
	if err := verifyForkWorkflowRegistration(repoDir); err != nil {
		return Report{}, err
	}
	report.ForkWorkflowRegistrationDocumented = true

	releaseWorkflow, err := os.ReadFile(filepath.Join(workflowDir, "growthos-new-api-release.yml"))
	if err != nil {
		return Report{}, fmt.Errorf("fork release workflow: %w", err)
	}
	if err := verifyEveryJobRepositoryGuard(releaseWorkflow, canonicalForkRepository); err != nil {
		return Report{}, fmt.Errorf("fork release workflow: %w", err)
	}
	releaseText := string(releaseWorkflow)
	if strings.Contains(releaseText, "calciumion/new-api") || !strings.Contains(releaseText, canonicalForkImage) {
		return Report{}, errors.New("fork release workflow may use only the authorized GHCR namespace")
	}
	if strings.Contains(strings.ToLower(releaseText), "latest") || !strings.Contains(releaseText, "sha-${REVISION}") {
		return Report{}, errors.New("fork release workflow must emit only an immutable sha tag")
	}
	if strings.Contains(releaseText, "Dockerfile.dev") {
		return Report{}, errors.New("fork release workflow must never use Dockerfile.dev")
	}
	if !strings.Contains(releaseText, "file: ./Dockerfile") {
		return Report{}, errors.New("fork release workflow must use the production Dockerfile")
	}
	if strings.Contains(releaseText, "> VERSION") || strings.Contains(releaseText, ">> VERSION") ||
		!strings.Contains(releaseText, "VERSION_VALUE=production-parity-${REVISION}") {
		return Report{}, errors.New("fork release workflow must not modify VERSION")
	}
	if !strings.Contains(releaseText, exactReleaseBranch) {
		return Report{}, errors.New("fork release workflow must bind the exact release branch")
	}
	if !strings.Contains(releaseText, "release-license verify --repo .") || strings.Contains(releaseText, "release-license verify --repo . --allow-hold") {
		return Report{}, errors.New("fork release workflow must fail closed on any license HOLD")
	}
	if err := verifyForkReleaseGates(releaseText); err != nil {
		return Report{}, err
	}
	return report, nil
}

func verifyForkWorkflowRegistration(repoDir string) error {
	data, err := os.ReadFile(filepath.Join(repoDir, "release", "fork-workflow-registration.md"))
	if err != nil {
		return errors.New("fork workflow registration contract is missing")
	}
	content := string(data)
	for _, required := range []string{
		"`mlhjyx/new-api`",
		"`production-parity/settlement-readback-v1`",
		"`bde9b2f44887d34ec54799ae191d50f97914359e`",
		"--repo mlhjyx/new-api",
		"workflow must exist on the fork default branch",
		"163 unreviewed upstream commits",
	} {
		if !strings.Contains(content, required) {
			return errors.New("fork workflow registration contract is incomplete")
		}
	}
	if strings.Contains(content, "--repo QuantumNous/new-api") {
		return errors.New("fork workflow registration must never target upstream")
	}
	return nil
}

func verifyProtectedIdentity(repoDir string) error {
	moduleBytes, err := os.ReadFile(filepath.Join(repoDir, "go.mod"))
	if err != nil {
		return err
	}
	if !strings.HasPrefix(string(moduleBytes), "module github.com/QuantumNous/new-api\n") {
		return errors.New("protected module identity differs")
	}
	readmeDigest, err := sha256File(filepath.Join(repoDir, "README.md"))
	if err != nil || readmeDigest != protectedReadmeSHA256 {
		return errors.New("protected upstream README differs")
	}
	templateDigest, err := sha256File(filepath.Join(repoDir, ".github", "PULL_REQUEST_TEMPLATE.md"))
	if err != nil || templateDigest != protectedPRTemplateSHA256 {
		return errors.New("protected upstream pull request template differs")
	}
	gitleaksIgnoreDigest, err := sha256File(filepath.Join(repoDir, ".gitleaksignore"))
	if err != nil || gitleaksIgnoreDigest != protectedGitleaksIgnoreSHA {
		return errors.New("reviewed secret allowlist differs")
	}
	vetBaselineDigest, err := sha256File(filepath.Join(repoDir, "release", "go-vet-baseline.txt"))
	if err != nil || vetBaselineDigest != protectedGoVetBaselineSHA {
		return errors.New("reviewed Go vet baseline differs")
	}
	disclosure, err := os.ReadFile(filepath.Join(repoDir, "release", "pull-request-description.md"))
	if err != nil {
		return errors.New("AI assistance disclosure is missing")
	}
	disclosureText := string(disclosure)
	for _, required := range []string{
		"Target branch: `" + exactReleaseBranch + "`",
		"## AI assistance disclosure",
		"OpenAI Codex",
		"Human review status: `NOT_YET_COMPLETED`",
		"Publication status: `NOT_AUTHORIZED_BY_THIS_ARTIFACT`",
	} {
		if !strings.Contains(disclosureText, required) {
			return errors.New("AI assistance disclosure is incomplete")
		}
	}
	return nil
}

func verifyForkPullRequestGates(content string) error {
	required := []string{
		pinnedGitleaksImage,
		"--workdir /repo",
		"dir --no-banner --redact --exit-code 1",
		"--gitleaks-ignore-path .gitleaksignore .",
		"--gitleaks-ignore-path .gitleaksignore web/default/dist",
		"--gitleaks-ignore-path .gitleaksignore web/classic/dist",
		"output-file: ${{ runner.temp }}/new-api-source.spdx.json",
		"output-file: ${{ runner.temp }}/new-api-image.spdx.json",
		"licenses/license-review.json",
		"licenses/license-review.md",
		"--read-only",
		"/api/corresponding-source/v1",
		"go test ./internal/releaseprovenance ./internal/releasepolicy ./internal/licenseinventory ./cmd/release-provenance ./cmd/release-license",
		"bun test shared/lobe-ui-adapter/adapter.test.tsx",
		"test ! -e node_modules/@giscus/react",
		"test ! -e node_modules/@splinetool/runtime",
		"grep -RFl \"growthos-lobe-flex-adapter\"",
		"go vet ./... 2> \"${RUNNER_TEMP}/go-vet.raw.txt\"",
		"LC_ALL=C sort -u release/go-vet-baseline.txt",
		"diff -u \"${RUNNER_TEMP}/go-vet.expected.txt\" \"${RUNNER_TEMP}/go-vet.actual.txt\"",
		"go test -race ./model ./controller ./router -run Settlement -count=1",
		"bun install --filter ./classic --frozen-lockfile",
		"git -C \"${GITHUB_WORKSPACE}\" archive --format=tar HEAD:web",
		"containerd-snapshotter",
	}
	for _, value := range required {
		if !strings.Contains(content, value) {
			return fmt.Errorf("fork PR workflow supply-chain gate is incomplete: missing %s", value)
		}
	}
	if strings.Contains(content, "Dockerfile.dev") || strings.Contains(content, "docker-compose") || strings.Contains(content, "docker compose") {
		return errors.New("fork PR managed checks must not use a development Docker path")
	}
	if strings.Contains(content, "go test -race ./model ./controller ./middleware ./router ./relay/channel ./service") {
		return errors.New("fork PR race gate must remain scoped to settlement behavior")
	}
	return nil
}

func verifyForkReleaseGates(content string) error {
	secretIndex := strings.Index(content, pinnedGitleaksImage)
	licenseIndex := strings.Index(content, "release-license verify --repo .")
	preflightIndex := uniqueMarkerIndex(content, "- name: Preflight immutable publication targets")
	sourceIndex := uniqueMarkerIndex(content, "- name: Publish immutable corresponding source")
	publicReadbackIndex := uniqueMarkerIndex(content, "- name: Verify public corresponding source digest")
	loginIndex := strings.Index(content, "docker/login-action")
	publishIndex := uniqueMarkerIndex(content, "- name: Build and publish one exact image")
	finalizeIndex := uniqueMarkerIndex(content, "- name: Sign exact digest and finalize receipt")
	imageEvidenceIndex := uniqueMarkerIndex(content, "- name: Append immutable image evidence")
	if preflightIndex < 0 || sourceIndex < 0 || publicReadbackIndex < 0 || publishIndex < 0 || finalizeIndex < 0 || imageEvidenceIndex < 0 ||
		!(preflightIndex < sourceIndex && sourceIndex < publicReadbackIndex && publicReadbackIndex < loginIndex && loginIndex < publishIndex && publishIndex < finalizeIndex && finalizeIndex < imageEvidenceIndex) {
		return errors.New("fork release publication transaction order is invalid")
	}
	if secretIndex < 0 || licenseIndex < 0 || loginIndex < 0 || secretIndex > preflightIndex || licenseIndex > preflightIndex {
		return errors.New("fork release must pass pinned secret and fail-closed license gates before publication preflight")
	}
	if !strings.Contains(content, "output-file: ${{ runner.temp }}/new-api-source.spdx.json") ||
		!strings.Contains(content, "output-file: ${{ runner.temp }}/new-api-image.spdx.json") {
		return errors.New("fork release must generate source and final-image SBOMs")
	}
	if !strings.Contains(content, "group: new-api-exact-release-${{ inputs.revision }}") ||
		!strings.Contains(content, "cancel-in-progress: false") {
		return errors.New("fork release must serialize same-revision publication")
	}
	preflight := content[preflightIndex:sourceIndex]
	for _, required := range []string{
		"SOURCE_RELEASE_STATUS=$(curl",
		"IMAGE_MANIFEST_STATUS=$(curl",
		"MANIFEST_UNKNOWN",
		"NAME_UNKNOWN",
		"unable to prove source release absence",
		"unable to prove image tag absence",
		"SOURCE_RELEASE_EXISTS=true",
		"(.assets | length == 3) and",
		"[.assets[] | {name, state}] | sort_by(.name)",
		"--source-sbom \"${RECOVERY_DIR}/new-api-source.spdx.json\"",
		"cmp -- \"${RECOVERY_DIR}/new-api-source-provenance.json\" \"${RECOVERY_DIR}/verified-provenance.json\"",
		"cmp -- \"${RECOVERY_DIR}/new-api-${REVISION}.tar.gz\" \"${RECOVERY_DIR}/verified-source.tar.gz\"",
		"cmp -- \"${RUNNER_TEMP}/new-api-${REVISION}.tar.gz\" \"${RECOVERY_DIR}/verified-source.tar.gz\"",
		"exact image tag already exists; refusing rebind",
	} {
		if !strings.Contains(preflight, required) {
			return errors.New("fork release publication preflight is incomplete")
		}
	}
	if strings.Contains(content, "--clobber") {
		return errors.New("fork release must never clobber immutable release assets")
	}
	sourcePublication := content[sourceIndex:publicReadbackIndex]
	if !strings.Contains(sourcePublication, "if: env.SOURCE_RELEASE_EXISTS != 'true'") ||
		!strings.Contains(sourcePublication, "gh release create \"source-${REVISION}\" --repo mlhjyx/new-api") ||
		strings.Contains(sourcePublication, "new-api-image.spdx.json") ||
		strings.Contains(sourcePublication, "new-api-release-receipt.json") ||
		strings.Contains(sourcePublication, "image-sbom-attestation") {
		return errors.New("fork release must publish source-only immutable assets before the image")
	}
	publicReadback := content[publicReadbackIndex:loginIndex]
	if !strings.Contains(publicReadback, "PUBLIC_SOURCE_ARCHIVE=") ||
		!strings.Contains(publicReadback, "${SOURCE_ARCHIVE_URI}") ||
		!strings.Contains(publicReadback, "sha256sum --check --strict") {
		return errors.New("fork release must verify the public source digest before image publication")
	}
	imageEvidence := content[imageEvidenceIndex:]
	if !strings.Contains(imageEvidence, "gh release upload \"source-${REVISION}\" --repo mlhjyx/new-api") ||
		!strings.Contains(imageEvidence, "new-api-image.spdx.json") ||
		!strings.Contains(imageEvidence, "new-api-release-receipt.json") ||
		!strings.Contains(imageEvidence, "image-sbom-attestation.outputs.bundle-path") {
		return errors.New("fork release GitHub commands must select the exact fork repository")
	}
	return nil
}

func uniqueMarkerIndex(content string, marker string) int {
	if strings.Count(content, marker) != 1 {
		return -1
	}
	return strings.Index(content, marker)
}

func sha256File(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func verifyProductionDockerfile(repoDir string) (string, string, error) {
	data, err := os.ReadFile(filepath.Join(repoDir, "Dockerfile"))
	if err != nil {
		return "", "", err
	}
	content := string(data)
	if strings.Contains(content, "apt-get") || strings.Contains(content, "apk add") || !strings.Contains(content, "FROM scratch AS runtime") {
		return "", "", errors.New("production runtime must be package-manager-free")
	}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "FROM ") || line == "FROM scratch AS runtime" {
			continue
		}
		image := strings.Fields(line)
		if len(image) < 2 || !strings.Contains(image[1], "@sha256:") {
			return "", "", errors.New("all production build bases must use exact image digests")
		}
	}
	if !strings.Contains(content, "CGO_ENABLED=0") || strings.Contains(content, "CGO_ENABLED=1") {
		return "", "", errors.New("production binary must be CGO-disabled")
	}
	if strings.Count(content, "COPY web/shared/lobe-ui-adapter/package.json ./shared/lobe-ui-adapter/package.json") != 2 ||
		strings.Count(content, "COPY web/shared/lobe-ui-adapter ./shared/lobe-ui-adapter") != 2 {
		return "", "", errors.New("production frontend builds must bind the private Lobe UI adapter")
	}
	boundedDownload := "RUN --mount=type=cache,id=new-api-go-mod,target=/go/pkg/mod,sharing=locked \\\n    set -eu; \\\n    for attempt in 1 2 3"
	if !strings.Contains(content, boundedDownload) || !strings.Contains(content, "go mod download") {
		return "", "", errors.New("production build requires a bounded module download with a persistent checksum-verified cache")
	}
	if !strings.Contains(content, "ARG GO_MODULE_PROXY=https://proxy.golang.org,direct") ||
		!strings.Contains(content, "GOPROXY=\"${GO_MODULE_PROXY}\" GOSUMDB=sum.golang.org go mod download") ||
		!strings.Contains(content, "GOPROXY=\"${GO_MODULE_PROXY}\" GOSUMDB=sum.golang.org go build") {
		return "", "", errors.New("production build requires a checksum-verified configurable Go module proxy")
	}
	if !strings.Contains(content, "COPY --from=builder2 --chown=65532:65532 /build/new-api /new-api") ||
		!strings.Contains(content, "COPY --from=builder2 --chown=65532:65532 /runtime/data /data") ||
		!strings.Contains(content, "COPY --from=builder2 --chown=65532:65532 /runtime/licenses /licenses") {
		return "", "", errors.New("production artifacts and writable directories require numeric ownership")
	}
	if !strings.Contains(content, "USER 65532:65532") {
		return "", "", errors.New("production runtime must use the numeric non-root user")
	}
	requiredLabels := []string{
		"org.opencontainers.image.source=",
		"org.opencontainers.image.revision=",
		"org.opencontainers.image.licenses=",
		"io.growthos.new-api.upstream.source=",
		"io.growthos.new-api.fork.git-tree=",
		"io.growthos.new-api.patch-series-sha256=",
		"io.growthos.new-api.patched-tree-sha256=",
		"io.growthos.new-api.build-recipe-sha256=",
		"io.growthos.new-api.module-graph-sha256=",
		"io.growthos.new-api.corresponding-source.uri=",
		"io.growthos.new-api.corresponding-source.sha256=",
		"io.growthos.new-api.source-sbom.sha256=",
	}
	for _, label := range requiredLabels {
		if !strings.Contains(content, label) {
			return "", "", fmt.Errorf("production OCI labels are incomplete: missing %s", label)
		}
	}
	return "scratch", "65532:65532", nil
}

func verifyEveryJobRepositoryGuard(data []byte, repository string) error {
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return fmt.Errorf("invalid workflow YAML: %w", err)
	}
	root := document.Content
	if len(root) != 1 || root[0].Kind != yaml.MappingNode {
		return errors.New("workflow root must be a mapping")
	}
	jobs := mappingValue(root[0], "jobs")
	if jobs == nil || jobs.Kind != yaml.MappingNode || len(jobs.Content) == 0 {
		return errors.New("workflow jobs mapping is missing")
	}
	wanted := "github.repository == '" + repository + "'"
	jobNames := make([]string, 0, len(jobs.Content)/2)
	for index := 0; index < len(jobs.Content); index += 2 {
		jobNames = append(jobNames, jobs.Content[index].Value)
		job := jobs.Content[index+1]
		guard := mappingValue(job, "if")
		if guard == nil || guard.Kind != yaml.ScalarNode || !strings.Contains(guard.Value, wanted) {
			sort.Strings(jobNames)
			return fmt.Errorf("job %s lacks the exact canonical upstream guard %q", jobs.Content[index].Value, wanted)
		}
	}
	return nil
}

func mappingValue(mapping *yaml.Node, key string) *yaml.Node {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	for index := 0; index+1 < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value == key {
			return mapping.Content[index+1]
		}
	}
	return nil
}
