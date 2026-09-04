package licenseinventory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

const (
	approvedLicenseSHA256    = "8486a10c4393cee1c25392769ddd3b2d6c242d6ec7928e1414efff7dfb2f07ef"
	upstreamNoticeSHA256     = "528067fcdf4f9d7e3fdb489d02cbdd36a0efa63fc2eb1686340612c26beb9f33"
	forkNoticeMarker         = "\n==== GrowthOS Fork Modification Notice ===="
	runtimeEpayModule        = "github.com/Calcium-Ion/go-epay"
	runtimeEpayVersion       = "v0.0.5-0.20260612155053-774330a93901"
	runtimeEpayModuleSum     = "h1:RsR3YZPHrMvyiPAOIfxIPNEvrIIk1vBZNu/3LmxC/4Q="
	runtimeEpayLicenseSHA256 = "86f028deb5895d8994571a0393face710851c29bb938e9b23fe7a9828efd99a8"
)

var bunPackagePattern = regexp.MustCompile(`(?m)^\s{4}"([^"]+)": \["([^"]+)"[^\n]*"(sha512-[A-Za-z0-9+/=]+)"\],?$`)

var reviewedLicenseExpressions = map[string]struct{}{
	"0BSD":                    {},
	"Apache-2.0":              {},
	"Apache-2.0 OR MIT":       {},
	"BSD-2-Clause":            {},
	"BSD-3-Clause":            {},
	"ISC":                     {},
	"MIT":                     {},
	"MPL-2.0":                 {},
	"(MPL-2.0 OR Apache-2.0)": {},
	"OFL-1.1":                 {},
	"Unlicense":               {},
}

type Report struct {
	GoDirect                 int
	DefaultWebDirect         int
	ClassicWebDirect         int
	ElectronDirect           int
	ReviewStatus             string
	UnresolvedPackages       int
	RuntimeEpayVersion       string
	RuntimeEpayLicenseSHA256 string
}

// RuntimeModuleEvidence is re-derived from the exact module selected by the
// Go toolchain. It prevents a review record from granting a license that is
// absent from the bytes used by the product build.
type RuntimeModuleEvidence struct {
	Version       string
	ModuleSum     string
	LicenseSHA256 string
}

type dependency struct {
	Area      string
	Scope     string
	Ecosystem string
	Name      string
	Version   string
	Integrity string
	License   string
}

type licenseReview struct {
	SchemaVersion              string                 `json:"schema_version"`
	Status                     string                 `json:"status"`
	Distribution               string                 `json:"distribution"`
	ReviewedLicenseExpressions []string               `json:"reviewed_license_expressions"`
	DependencyResolutions      []dependencyResolution `json:"dependency_resolutions"`
	Evidence                   licenseReviewEvidence  `json:"evidence"`
	Unresolved                 []unresolvedPackage    `json:"unresolved"`
}

type dependencyResolution struct {
	Ecosystem          string `json:"ecosystem"`
	Name               string `json:"name"`
	PreviousVersion    string `json:"previous_version"`
	ResolvedVersion    string `json:"resolved_version"`
	ArtifactIntegrity  string `json:"artifact_integrity"`
	LicenseExpression  string `json:"license_expression"`
	LicenseFileSHA256  string `json:"license_file_sha256"`
	BundleReachability string `json:"bundle_reachability"`
	Decision           string `json:"decision"`
	EvidenceURI        string `json:"evidence_uri"`
}

type licenseReviewEvidence struct {
	GoModSHA256          string `json:"go_mod_sha256"`
	GoSumSHA256          string `json:"go_sum_sha256"`
	BunLockSHA256        string `json:"bun_lock_sha256"`
	ElectronLockSHA256   string `json:"electron_lock_sha256"`
	LicenseSHA256        string `json:"license_sha256"`
	UpstreamNoticeSHA256 string `json:"upstream_notice_sha256"`
}

type unresolvedPackage struct {
	Ecosystem               string `json:"ecosystem"`
	Name                    string `json:"name"`
	Version                 string `json:"version"`
	Integrity               string `json:"integrity"`
	Scope                   string `json:"scope"`
	ReasonCode              string `json:"reason_code"`
	DistributionObservation string `json:"distribution_observation"`
}

func VerifyRepository(repoDir string, allowHold bool) (Report, error) {
	if err := verifyProjectLicense(repoDir); err != nil {
		return Report{}, err
	}
	if err := verifyNotice(repoDir); err != nil {
		return Report{}, err
	}
	expected, counts, bunPackages, err := expectedDirectDependencies(repoDir)
	if err != nil {
		return Report{}, err
	}
	actual, err := parseThirdPartyInventory(filepath.Join(repoDir, "THIRD-PARTY-LICENSES.md"))
	if err != nil {
		return Report{}, err
	}
	if err := compareDirectInventory(expected, actual); err != nil {
		return Report{}, err
	}
	review, err := loadLicenseReview(repoDir)
	if err != nil {
		return Report{}, err
	}
	if err := validateLicenseReview(repoDir, review, bunPackages); err != nil {
		return Report{}, err
	}
	runtimeEpay, err := VerifyRuntimeEpayModule(context.Background(), repoDir)
	if err != nil {
		return Report{}, err
	}
	if review.Status == "HOLD" && !allowHold {
		return Report{}, errors.New("license review remains HOLD")
	}
	return Report{
		GoDirect:                 counts["backend"],
		DefaultWebDirect:         counts["web/default"],
		ClassicWebDirect:         counts["web/classic"],
		ElectronDirect:           counts["electron"],
		ReviewStatus:             review.Status,
		UnresolvedPackages:       len(review.Unresolved),
		RuntimeEpayVersion:       runtimeEpay.Version,
		RuntimeEpayLicenseSHA256: runtimeEpay.LicenseSHA256,
	}, nil
}

// VerifyRuntimeEpayModule validates the selected module version, its checksum
// entry and the actual license file in the module cache. It intentionally uses
// `go list -m` rather than a repository web page so verification follows the
// same immutable module bytes as `go build -mod=readonly`.
func VerifyRuntimeEpayModule(ctx context.Context, repoDir string) (RuntimeModuleEvidence, error) {
	resolvedRepo, err := filepath.Abs(repoDir)
	if err != nil {
		return RuntimeModuleEvidence{}, fmt.Errorf("resolve repository: %w", err)
	}
	command := exec.CommandContext(ctx, "go", "list", "-m", "-json", runtimeEpayModule)
	command.Dir = resolvedRepo
	command.Env = append(os.Environ(), "GOWORK=off")
	output, err := command.Output()
	if err != nil {
		return RuntimeModuleEvidence{}, fmt.Errorf("resolve exact runtime payment module: %w", err)
	}
	var selected struct {
		Path    string `json:"Path"`
		Version string `json:"Version"`
		Sum     string `json:"Sum"`
	}
	if err := common.Unmarshal(output, &selected); err != nil {
		return RuntimeModuleEvidence{}, fmt.Errorf("decode runtime payment module evidence: %w", err)
	}
	if selected.Path != runtimeEpayModule || selected.Version != runtimeEpayVersion || selected.Sum != runtimeEpayModuleSum {
		return RuntimeModuleEvidence{}, errors.New("runtime payment module identity or checksum differs")
	}
	download := exec.CommandContext(ctx, "go", "mod", "download", "-json", runtimeEpayModule+"@"+runtimeEpayVersion)
	download.Dir = resolvedRepo
	download.Env = append(os.Environ(), "GOWORK=off")
	downloadOutput, err := download.Output()
	if err != nil {
		return RuntimeModuleEvidence{}, fmt.Errorf("download exact runtime payment module: %w", err)
	}
	var artifact struct {
		Path    string `json:"Path"`
		Version string `json:"Version"`
		Sum     string `json:"Sum"`
		Dir     string `json:"Dir"`
		Error   string `json:"Error"`
	}
	if err := common.Unmarshal(downloadOutput, &artifact); err != nil {
		return RuntimeModuleEvidence{}, fmt.Errorf("decode runtime payment module artifact: %w", err)
	}
	if artifact.Error != "" || artifact.Path != selected.Path || artifact.Version != selected.Version || artifact.Sum != selected.Sum {
		return RuntimeModuleEvidence{}, errors.New("runtime payment module artifact differs from selected module")
	}
	if !filepath.IsAbs(artifact.Dir) {
		return RuntimeModuleEvidence{}, errors.New("runtime payment module directory is not absolute")
	}
	licensePath := filepath.Join(artifact.Dir, "LICENSE")
	info, err := os.Lstat(licensePath)
	if err != nil {
		return RuntimeModuleEvidence{}, fmt.Errorf("runtime payment module license: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return RuntimeModuleEvidence{}, errors.New("runtime payment module license is not a regular file")
	}
	licenseDigest, err := fileSHA256(licensePath)
	if err != nil {
		return RuntimeModuleEvidence{}, fmt.Errorf("hash runtime payment module license: %w", err)
	}
	if licenseDigest != runtimeEpayLicenseSHA256 {
		return RuntimeModuleEvidence{}, errors.New("runtime payment module license bytes differ")
	}
	return RuntimeModuleEvidence{
		Version:       selected.Version,
		ModuleSum:     selected.Sum,
		LicenseSHA256: licenseDigest,
	}, nil
}

func expectedDirectDependencies(repoDir string) (map[string]dependency, map[string]int, map[string]dependency, error) {
	result := make(map[string]dependency)
	counts := make(map[string]int)
	goDependencies, err := parseGoMod(filepath.Join(repoDir, "go.mod"))
	if err != nil {
		return nil, nil, nil, err
	}
	for _, item := range goDependencies {
		result[dependencyKey(item)] = item
		counts[item.Area]++
	}

	bunLock, err := os.ReadFile(filepath.Join(repoDir, "web", "bun.lock"))
	if err != nil {
		return nil, nil, nil, err
	}
	bunPackages := parseBunPackages(bunLock)
	for _, area := range []string{"default", "classic"} {
		items, err := parseWebPackage(filepath.Join(repoDir, "web", area, "package.json"), area, bunPackages)
		if err != nil {
			return nil, nil, nil, err
		}
		for _, item := range items {
			result[dependencyKey(item)] = item
			counts[item.Area]++
		}
	}

	electron, err := parseElectron(filepath.Join(repoDir, "electron", "package.json"), filepath.Join(repoDir, "electron", "package-lock.json"))
	if err != nil {
		return nil, nil, nil, err
	}
	for _, item := range electron {
		result[dependencyKey(item)] = item
		counts[item.Area]++
	}
	return result, counts, bunPackages, nil
}

func parseGoMod(path string) ([]dependency, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	items := make([]dependency, 0, 64)
	inBlock := false
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "require (" {
			inBlock = true
			continue
		}
		if inBlock && trimmed == ")" {
			inBlock = false
			continue
		}
		if strings.Contains(trimmed, "// indirect") {
			continue
		}
		var fields []string
		if inBlock {
			fields = strings.Fields(trimmed)
		} else if strings.HasPrefix(trimmed, "require ") {
			fields = strings.Fields(strings.TrimPrefix(trimmed, "require "))
		}
		if len(fields) == 2 {
			items = append(items, dependency{Area: "backend", Scope: "production", Ecosystem: "Go", Name: fields[0], Version: fields[1]})
		}
	}
	return items, nil
}

type packageManifest struct {
	Name            string            `json:"name"`
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
}

func parseWebPackage(path string, workspace string, locked map[string]dependency) ([]dependency, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var manifest packageManifest
	if err := common.Unmarshal(data, &manifest); err != nil {
		return nil, err
	}
	items := make([]dependency, 0, len(manifest.Dependencies)+len(manifest.DevDependencies))
	for _, scoped := range []struct {
		name  string
		items map[string]string
	}{{name: "production", items: manifest.Dependencies}, {name: "development", items: manifest.DevDependencies}} {
		names := sortedMapKeys(scoped.items)
		for _, name := range names {
			lockedItem, ok := locked[manifest.Name+"/"+name]
			if !ok {
				lockedItem, ok = locked[name]
			}
			if !ok || lockedItem.Name != name || lockedItem.Version == "" {
				return nil, fmt.Errorf("web/%s direct dependency %s has no exact Bun lock entry", workspace, name)
			}
			lockedItem.Area = "web/" + workspace
			lockedItem.Scope = scoped.name
			lockedItem.Ecosystem = "npm"
			items = append(items, lockedItem)
		}
	}
	return items, nil
}

func parseBunPackages(data []byte) map[string]dependency {
	packages := make(map[string]dependency)
	for _, match := range bunPackagePattern.FindAllSubmatch(data, -1) {
		key := string(match[1])
		spec := string(match[2])
		name, version := splitNPMNameVersion(spec)
		if name == "" || version == "" {
			continue
		}
		packages[key] = dependency{Name: name, Version: version, Integrity: string(match[3])}
	}
	return packages
}

func splitNPMNameVersion(value string) (string, string) {
	index := strings.LastIndex(value, "@")
	if index <= 0 || index == len(value)-1 {
		return "", ""
	}
	return value[:index], value[index+1:]
}

func parseElectron(manifestPath string, lockPath string) ([]dependency, error) {
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, err
	}
	var manifest packageManifest
	if err := common.Unmarshal(manifestBytes, &manifest); err != nil {
		return nil, err
	}
	lockBytes, err := os.ReadFile(lockPath)
	if err != nil {
		return nil, err
	}
	var lock struct {
		Packages map[string]struct {
			Version   string `json:"version"`
			Integrity string `json:"integrity"`
		} `json:"packages"`
	}
	if err := common.Unmarshal(lockBytes, &lock); err != nil {
		return nil, err
	}
	items := make([]dependency, 0, len(manifest.Dependencies)+len(manifest.DevDependencies))
	for _, scoped := range []struct {
		name  string
		items map[string]string
	}{{name: "production", items: manifest.Dependencies}, {name: "development", items: manifest.DevDependencies}} {
		for _, name := range sortedMapKeys(scoped.items) {
			locked, ok := lock.Packages["node_modules/"+name]
			if !ok || locked.Version == "" {
				return nil, fmt.Errorf("electron direct dependency %s has no exact package-lock entry", name)
			}
			items = append(items, dependency{Area: "electron", Scope: scoped.name, Ecosystem: "npm", Name: name, Version: locked.Version, Integrity: locked.Integrity})
		}
	}
	return items, nil
}

func parseThirdPartyInventory(path string) (map[string]dependency, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	result := make(map[string]dependency)
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "|") {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) != 8 {
			continue
		}
		item := dependency{
			Area:      strings.TrimSpace(parts[1]),
			Scope:     strings.TrimSpace(parts[2]),
			Ecosystem: strings.TrimSpace(parts[3]),
			Name:      strings.Trim(strings.TrimSpace(parts[4]), "`"),
			Version:   strings.Trim(strings.TrimSpace(parts[5]), "`"),
			License:   strings.TrimSpace(parts[6]),
		}
		if item.Name == "Dependency" || strings.HasPrefix(item.Area, "-") || item.Name == "" {
			continue
		}
		key := dependencyKey(item)
		if _, exists := result[key]; exists {
			return nil, fmt.Errorf("direct dependency inventory contains duplicate %s", key)
		}
		result[key] = item
	}
	return result, nil
}

func compareDirectInventory(expected map[string]dependency, actual map[string]dependency) error {
	if len(expected) != len(actual) {
		return fmt.Errorf("direct dependency inventory count differs: got %d want %d", len(actual), len(expected))
	}
	for key, wanted := range expected {
		got, ok := actual[key]
		if !ok || got.Version != wanted.Version || got.Area != wanted.Area || got.Scope != wanted.Scope || got.Ecosystem != wanted.Ecosystem {
			return fmt.Errorf("direct dependency inventory mismatch for %s", key)
		}
		if _, reviewed := reviewedLicenseExpressions[got.License]; !reviewed {
			return fmt.Errorf("direct dependency inventory has unreviewed license expression for %s", key)
		}
	}
	return nil
}

func verifyProjectLicense(repoDir string) error {
	digest, err := fileSHA256(filepath.Join(repoDir, "LICENSE"))
	if err != nil {
		return err
	}
	if digest != approvedLicenseSHA256 {
		return errors.New("approved AGPL license bytes were not preserved")
	}
	return nil
}

func verifyNotice(repoDir string) error {
	data, err := os.ReadFile(filepath.Join(repoDir, "NOTICE"))
	if err != nil {
		return err
	}
	markerIndex := strings.Index(string(data), forkNoticeMarker)
	if markerIndex < 0 {
		return errors.New("original NOTICE must be preserved before an append-only fork notice")
	}
	digest := sha256.Sum256(data[:markerIndex])
	if hex.EncodeToString(digest[:]) != upstreamNoticeSHA256 {
		return errors.New("original NOTICE bytes were not preserved")
	}
	return nil
}

func loadLicenseReview(repoDir string) (licenseReview, error) {
	data, err := os.ReadFile(filepath.Join(repoDir, "release", "license-review.json"))
	if err != nil {
		return licenseReview{}, err
	}
	if err := common.ValidateJSONNoDuplicateKeys(data); err != nil {
		return licenseReview{}, err
	}
	raw, err := requireClosedLicenseObject(data, []string{"dependency_resolutions", "distribution", "evidence", "reviewed_license_expressions", "schema_version", "status", "unresolved"})
	if err != nil {
		return licenseReview{}, err
	}
	if _, err := requireClosedLicenseObject(raw["evidence"], []string{"bun_lock_sha256", "electron_lock_sha256", "go_mod_sha256", "go_sum_sha256", "license_sha256", "upstream_notice_sha256"}); err != nil {
		return licenseReview{}, err
	}
	if err := requireClosedLicenseArray(raw["dependency_resolutions"], []string{"artifact_integrity", "bundle_reachability", "decision", "ecosystem", "evidence_uri", "license_expression", "license_file_sha256", "name", "previous_version", "resolved_version"}); err != nil {
		return licenseReview{}, err
	}
	if err := requireClosedLicenseArray(raw["unresolved"], []string{"distribution_observation", "ecosystem", "integrity", "name", "reason_code", "scope", "version"}); err != nil {
		return licenseReview{}, err
	}
	var review licenseReview
	if err := common.Unmarshal(data, &review); err != nil {
		return licenseReview{}, err
	}
	return review, nil
}

func requireClosedLicenseObject(data []byte, expected []string) (map[string]json.RawMessage, error) {
	var raw map[string]json.RawMessage
	if err := common.Unmarshal(data, &raw); err != nil || raw == nil {
		return nil, errors.New("closed license review object is invalid")
	}
	actual := sortedMapKeys(raw)
	wanted := append([]string(nil), expected...)
	sort.Strings(wanted)
	if strings.Join(actual, "\n") != strings.Join(wanted, "\n") {
		return nil, errors.New("closed license review keys differ")
	}
	return raw, nil
}

func requireClosedLicenseArray(data []byte, expected []string) error {
	var entries []json.RawMessage
	if err := common.Unmarshal(data, &entries); err != nil {
		return errors.New("closed license review array is invalid")
	}
	for _, entry := range entries {
		if _, err := requireClosedLicenseObject(entry, expected); err != nil {
			return err
		}
	}
	return nil
}

func validateLicenseReview(repoDir string, review licenseReview, bunPackages map[string]dependency) error {
	if review.SchemaVersion != "new-api-license-review/v1" || (review.Status != "HOLD" && review.Status != "APPROVED") {
		return errors.New("license review schema or status is invalid")
	}
	if review.Distribution != "ghcr.io/mlhjyx/new-api OCI image plus public corresponding source" {
		return errors.New("license review distribution is invalid")
	}
	wantedExpressions := sortedMapKeys(reviewedLicenseExpressions)
	actualExpressions := append([]string(nil), review.ReviewedLicenseExpressions...)
	sort.Strings(actualExpressions)
	if strings.Join(wantedExpressions, "\n") != strings.Join(actualExpressions, "\n") {
		return errors.New("reviewed license expression set is incomplete")
	}
	evidence := map[string]string{
		"go.mod":                     review.Evidence.GoModSHA256,
		"go.sum":                     review.Evidence.GoSumSHA256,
		"web/bun.lock":               review.Evidence.BunLockSHA256,
		"electron/package-lock.json": review.Evidence.ElectronLockSHA256,
		"LICENSE":                    review.Evidence.LicenseSHA256,
	}
	for path, expected := range evidence {
		actual, err := fileSHA256(filepath.Join(repoDir, filepath.FromSlash(path)))
		if err != nil || actual != expected {
			return fmt.Errorf("license review evidence digest mismatch for %s", path)
		}
	}
	if review.Evidence.UpstreamNoticeSHA256 != upstreamNoticeSHA256 {
		return errors.New("license review upstream NOTICE digest is invalid")
	}
	if err := validateDependencyResolutions(review.DependencyResolutions); err != nil {
		return err
	}
	if review.Status == "APPROVED" && len(review.Unresolved) != 0 {
		return errors.New("approved license review cannot contain unresolved packages")
	}
	wantedUnresolved := map[string]struct{}{
		"@giscus/react@3.1.0":         {},
		"@splinetool/runtime@0.9.526": {},
	}
	if review.Status == "HOLD" && len(review.Unresolved) != len(wantedUnresolved) {
		return errors.New("license review unresolved package set is incomplete")
	}
	for _, item := range review.Unresolved {
		key := item.Name + "@" + item.Version
		if _, ok := wantedUnresolved[key]; !ok {
			return fmt.Errorf("license review contains unexpected unresolved package %s", key)
		}
		switch item.Ecosystem {
		case "npm":
			locked, ok := bunPackages[item.Name]
			if !ok || locked.Version != item.Version || locked.Integrity != item.Integrity {
				return fmt.Errorf("unresolved package %s does not match the exact Bun lock", key)
			}
			if item.Scope != "build-only-auto-peer" || item.ReasonCode != "PACKAGE_LICENSE_METADATA_MISSING" {
				return fmt.Errorf("unresolved package %s has incomplete review evidence", key)
			}
		default:
			return fmt.Errorf("unresolved package %s has an unknown ecosystem", key)
		}
		if strings.TrimSpace(item.DistributionObservation) == "" {
			return fmt.Errorf("unresolved package %s has incomplete review evidence", key)
		}
	}
	return nil
}

func validateDependencyResolutions(resolutions []dependencyResolution) error {
	if len(resolutions) != 3 {
		return errors.New("license review dependency resolution set is incomplete")
	}
	wanted := map[string]dependencyResolution{
		"github.com/Calcium-Ion/go-epay": {
			Ecosystem:          "Go",
			Name:               "github.com/Calcium-Ion/go-epay",
			PreviousVersion:    "v0.0.4",
			ResolvedVersion:    "v0.0.5-0.20260612155053-774330a93901",
			ArtifactIntegrity:  "h1:RsR3YZPHrMvyiPAOIfxIPNEvrIIk1vBZNu/3LmxC/4Q=",
			LicenseExpression:  "MIT",
			LicenseFileSHA256:  "86f028deb5895d8994571a0393face710851c29bb938e9b23fe7a9828efd99a8",
			BundleReachability: "runtime-linked",
			Decision:           "UPGRADED_TO_EXACT_LICENSED_COMMIT",
			EvidenceURI:        "https://github.com/Calcium-Ion/go-epay/blob/774330a939012a2baab5776e890456d5f15d586e/LICENSE",
		},
		"@giscus/react": {
			Ecosystem:          "npm",
			Name:               "@giscus/react",
			PreviousVersion:    "3.1.0",
			ResolvedVersion:    "3.1.0",
			ArtifactIntegrity:  "sha512-0TCO2TvL43+oOdyVVGHDItwxD1UMKP2ZYpT6gXmhFOqfAJtZxTzJ9hkn34iAF/b6YzyJ4Um89QIt9z/ajmAEeg==",
			LicenseExpression:  "NOASSERTION",
			BundleReachability: "auto-peer-build-graph; package identifier absent from frozen output literal scan",
			Decision:           "HOLD_REPLACE_OR_ISOLATE",
			EvidenceURI:        "https://registry.npmjs.org/@giscus/react/3.1.0",
		},
		"@splinetool/runtime": {
			Ecosystem:          "npm",
			Name:               "@splinetool/runtime",
			PreviousVersion:    "0.9.526",
			ResolvedVersion:    "0.9.526",
			ArtifactIntegrity:  "sha512-qznHbXA5aKwDbCgESAothCNm1IeEZcmNWG145p5aXj4w5uoqR1TZ9qkTHTKLTsUbHeitCwdhzmRqan1kxboLgQ==",
			LicenseExpression:  "NOASSERTION",
			BundleReachability: "auto-peer-build-graph; package identifier absent from frozen output literal scan",
			Decision:           "HOLD_REPLACE_OR_ISOLATE",
			EvidenceURI:        "https://registry.npmjs.org/@splinetool/runtime/0.9.526",
		},
	}
	for _, resolution := range resolutions {
		expected, ok := wanted[resolution.Name]
		if !ok || resolution != expected {
			return fmt.Errorf("license review dependency resolution differs for %s", resolution.Name)
		}
		delete(wanted, resolution.Name)
	}
	if len(wanted) != 0 {
		return errors.New("license review dependency resolution set is incomplete")
	}
	return nil
}

func dependencyKey(item dependency) string {
	return item.Area + "\x00" + item.Scope + "\x00" + item.Ecosystem + "\x00" + item.Name
}

func fileSHA256(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func sortedMapKeys[V any](value map[string]V) []string {
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
