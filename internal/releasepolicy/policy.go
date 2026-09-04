package releasepolicy

import (
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
)

var actionReferencePattern = regexp.MustCompile(`(?m)^\s*-?\s*uses:\s*[^\s#]+@([^\s#]+)`)
var commitReferencePattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

type Report struct {
	ForkImageRepository      string
	ReleaseBranch            string
	GuardedUpstreamWorkflows int
	PinnedActionReferences   int
	RuntimeUser              string
	RuntimeBase              string
	BoundedModuleDownload    bool
	ChecksummedModuleProxy   bool
}

func VerifyRepository(repoDir string) (Report, error) {
	report := Report{
		ForkImageRepository: canonicalForkImage,
		ReleaseBranch:       exactReleaseBranch,
	}
	runtimeBase, runtimeUser, err := verifyProductionDockerfile(repoDir)
	if err != nil {
		return Report{}, err
	}
	report.RuntimeBase = runtimeBase
	report.RuntimeUser = runtimeUser
	report.BoundedModuleDownload = true
	report.ChecksummedModuleProxy = true
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
	return report, nil
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
