package releaseprovenance

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/QuantumNous/new-api/common"
)

func MarshalSourceProvenance(provenance SourceProvenance) ([]byte, error) {
	if err := validateSourceProvenance(provenance); err != nil {
		return nil, err
	}
	return marshalCanonical(provenance)
}

func DecodeSourceProvenance(data []byte) (SourceProvenance, error) {
	if err := requireSourceShape(data); err != nil {
		return SourceProvenance{}, err
	}
	var provenance SourceProvenance
	if err := common.Unmarshal(data, &provenance); err != nil {
		return SourceProvenance{}, fmt.Errorf("decode source provenance: %w", err)
	}
	if err := validateSourceProvenance(provenance); err != nil {
		return SourceProvenance{}, err
	}
	return provenance, nil
}

func MarshalReleaseReceipt(receipt ReleaseReceipt) ([]byte, error) {
	if receipt.SchemaVersion != "new-api-release-receipt/v1" {
		return nil, errors.New("release receipt schema version is invalid")
	}
	if err := validateSourceProvenance(receipt.Source); err != nil {
		return nil, err
	}
	if err := validateOCIRelease(receipt.OCI); err != nil {
		return nil, err
	}
	return marshalCanonical(receipt)
}

func DecodeReleaseReceipt(data []byte) (ReleaseReceipt, error) {
	root, err := decodeObject(data, []string{"oci", "schema_version", "source"})
	if err != nil {
		return ReleaseReceipt{}, err
	}
	if err := requireSourceShape(root["source"]); err != nil {
		return ReleaseReceipt{}, err
	}
	if _, err := decodeObject(root["oci"], []string{"digest", "image_sbom_attestation_sha256", "image_sbom_reference", "repository"}); err != nil {
		return ReleaseReceipt{}, err
	}
	var receipt ReleaseReceipt
	if err := common.Unmarshal(data, &receipt); err != nil {
		return ReleaseReceipt{}, fmt.Errorf("decode release receipt: %w", err)
	}
	if receipt.SchemaVersion != "new-api-release-receipt/v1" {
		return ReleaseReceipt{}, errors.New("release receipt schema version is invalid")
	}
	if err := validateSourceProvenance(receipt.Source); err != nil {
		return ReleaseReceipt{}, err
	}
	if err := validateOCIRelease(receipt.OCI); err != nil {
		return ReleaseReceipt{}, err
	}
	return receipt, nil
}

func DecodeUpstreamBase(data []byte) (UpstreamBase, error) {
	if _, err := decodeObject(data, []string{
		"canonical_url",
		"commit",
		"git_tree",
		"license_sha256",
		"notice_sha256",
		"schema_version",
		"source_archive_sha256",
		"third_party_sha256",
		"tree_archive_sha256",
	}); err != nil {
		return UpstreamBase{}, err
	}
	var manifest UpstreamBase
	if err := common.Unmarshal(data, &manifest); err != nil {
		return UpstreamBase{}, fmt.Errorf("decode upstream manifest: %w", err)
	}
	if err := validateUpstreamBase(manifest); err != nil {
		return UpstreamBase{}, err
	}
	return manifest, nil
}

func requireSourceShape(data []byte) error {
	root, err := decodeObject(data, []string{"build", "corresponding_source", "fork", "license", "sbom", "schema_version", "upstream"})
	if err != nil {
		return err
	}
	if _, err := decodeObject(root["upstream"], []string{
		"canonical_url",
		"commit",
		"git_tree",
		"license_sha256",
		"notice_sha256",
		"schema_version",
		"source_archive_sha256",
		"third_party_sha256",
		"tree_archive_sha256",
	}); err != nil {
		return err
	}
	if _, err := decodeObject(root["fork"], []string{"canonical_url", "commit", "git_tree", "patch_series_sha256", "patched_tree_sha256"}); err != nil {
		return err
	}
	if _, err := decodeObject(root["build"], []string{"module_graph_sha256", "recipe_sha256"}); err != nil {
		return err
	}
	if _, err := decodeObject(root["corresponding_source"], []string{"archive_sha256", "archive_uri", "uri"}); err != nil {
		return err
	}
	if _, err := decodeObject(root["license"], []string{"license_sha256", "notice_sha256", "spdx", "third_party_sha256"}); err != nil {
		return err
	}
	_, err = decodeObject(root["sbom"], []string{"source_dependency_sha256"})
	return err
}

func decodeObject(data []byte, expectedKeys []string) (map[string]json.RawMessage, error) {
	if err := common.ValidateJSONNoDuplicateKeys(data); err != nil {
		return nil, err
	}
	var object map[string]json.RawMessage
	if err := common.Unmarshal(data, &object); err != nil {
		return nil, fmt.Errorf("decode closed JSON object: %w", err)
	}
	if object == nil {
		return nil, errors.New("closed JSON value must be an object")
	}
	actualKeys := make([]string, 0, len(object))
	for key := range object {
		actualKeys = append(actualKeys, key)
	}
	sort.Strings(actualKeys)
	wanted := append([]string(nil), expectedKeys...)
	sort.Strings(wanted)
	if len(actualKeys) != len(wanted) {
		return nil, fmt.Errorf("closed JSON keys differ: got %v want %v", actualKeys, wanted)
	}
	for index := range wanted {
		if actualKeys[index] != wanted[index] {
			return nil, fmt.Errorf("closed JSON keys differ: got %v want %v", actualKeys, wanted)
		}
	}
	return object, nil
}

func marshalCanonical(value any) ([]byte, error) {
	encoded, err := common.Marshal(value)
	if err != nil {
		return nil, err
	}
	return append(bytes.TrimSpace(encoded), '\n'), nil
}
