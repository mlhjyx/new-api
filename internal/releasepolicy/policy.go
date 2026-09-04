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
}

func VerifyRepository(repoDir string) (Report, error) {
	report := Report{
		ForkImageRepository: canonicalForkImage,
		ReleaseBranch:       exactReleaseBranch,
	}
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
