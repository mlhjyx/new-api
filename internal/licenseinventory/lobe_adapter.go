package licenseinventory

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

const (
	lobeUIAdapterDependency = "@lobehub/ui"
	lobeUIAdapterVersion    = "5.0.0"
	lobeUIAdapterLock       = "workspace:shared/lobe-ui-adapter"
	lobeUIAdapterInventory  = "workspace:shared/lobe-ui-adapter@5.0.0"
)

var lobeUIAdapterFiles = []string{
	"web/shared/lobe-ui-adapter/package.json",
	"web/shared/lobe-ui-adapter/index.tsx",
	"web/shared/lobe-ui-adapter/icons.tsx",
}

type AdapterEvidence struct {
	ArtifactSHA256 string
	Version        string
}

type lobeUIAdapterManifest struct {
	Name             string            `json:"name"`
	Version          string            `json:"version"`
	Private          bool              `json:"private"`
	Description      string            `json:"description"`
	License          string            `json:"license"`
	Type             string            `json:"type"`
	Exports          map[string]string `json:"exports"`
	PeerDependencies map[string]string `json:"peerDependencies"`
}

func VerifyLobeUIAdapter(repoDir string) (AdapterEvidence, error) {
	manifestBytes, err := readRegularFile(filepath.Join(repoDir, filepath.FromSlash(lobeUIAdapterFiles[0])))
	if err != nil {
		return AdapterEvidence{}, fmt.Errorf("read private Lobe UI adapter manifest: %w", err)
	}
	if err := common.ValidateJSONNoDuplicateKeys(manifestBytes); err != nil {
		return AdapterEvidence{}, err
	}
	var manifest lobeUIAdapterManifest
	if err := common.Unmarshal(manifestBytes, &manifest); err != nil {
		return AdapterEvidence{}, err
	}
	if manifest.Name != lobeUIAdapterDependency || manifest.Version != lobeUIAdapterVersion || !manifest.Private ||
		manifest.Description != "GrowthOS private compatibility adapter for @lobehub/icons" ||
		manifest.License != "AGPL-3.0-only" || manifest.Type != "module" ||
		len(manifest.Exports) != 2 || manifest.Exports["."] != "./index.tsx" || manifest.Exports["./icons"] != "./icons.tsx" ||
		len(manifest.PeerDependencies) != 1 || manifest.PeerDependencies["react"] != "^19.0.0" {
		return AdapterEvidence{}, errors.New("private Lobe UI adapter manifest differs")
	}

	hash := sha256.New()
	for _, relative := range lobeUIAdapterFiles {
		data, err := readRegularFile(filepath.Join(repoDir, filepath.FromSlash(relative)))
		if err != nil {
			return AdapterEvidence{}, err
		}
		_, _ = hash.Write([]byte(relative))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write(data)
		_, _ = hash.Write([]byte{0})
	}
	return AdapterEvidence{ArtifactSHA256: hex.EncodeToString(hash.Sum(nil)), Version: manifest.Version}, nil
}

func resolveLobeUIWorkspaceDependency(manifestPath string, name string, locked dependency) (dependency, error) {
	if name != lobeUIAdapterDependency || locked.Name != lobeUIAdapterDependency || locked.Version != lobeUIAdapterLock {
		return dependency{}, fmt.Errorf("web dependency %s has an unauthorized workspace implementation", name)
	}
	webRoot := filepath.Dir(filepath.Dir(manifestPath))
	evidence, err := VerifyLobeUIAdapter(filepath.Dir(webRoot))
	if err != nil {
		return dependency{}, err
	}
	return dependency{
		Name:      lobeUIAdapterDependency,
		Version:   lobeUIAdapterInventory,
		Integrity: "sha256:" + evidence.ArtifactSHA256,
	}, nil
}

func readRegularFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("adapter source %s is not a regular file", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if strings.Contains(string(data), "@giscus/react") || strings.Contains(string(data), "@splinetool/runtime") {
		return nil, errors.New("private Lobe UI adapter imports an unresolved package")
	}
	return data, nil
}
