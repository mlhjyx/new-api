package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/QuantumNous/new-api/internal/releaseprovenance"
)

const maxProvenanceFileBytes = 4 * 1024 * 1024

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "release provenance command is required")
		return 2
	}
	var err error
	switch args[0] {
	case "verify-upstream":
		err = runVerifyUpstream(args[1:], stdout)
	case "prepare-source":
		err = runPrepareSource(args[1:])
	case "finalize-receipt":
		err = runFinalizeReceipt(args[1:])
	case "verify-receipt":
		err = runVerifyReceipt(args[1:])
	default:
		fmt.Fprintln(stderr, "unknown release provenance command")
		return 2
	}
	if err != nil {
		fmt.Fprintf(stderr, "release provenance command failed: %s\n", boundedError(err))
		return 1
	}
	return 0
}

func runVerifyUpstream(args []string, stdout io.Writer) error {
	flags := newFlagSet("verify-upstream")
	repo := flags.String("repo", ".", "source repository")
	if err := parseFlags(flags, args); err != nil {
		return err
	}
	manifest, err := releaseprovenance.VerifyPinnedUpstream(context.Background(), *repo)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(stdout, manifest.Commit)
	return err
}

func runPrepareSource(args []string) error {
	flags := newFlagSet("prepare-source")
	repo := flags.String("repo", ".", "source repository")
	revision := flags.String("revision", "", "exact source revision")
	sourceURI := flags.String("source-uri", "", "exact public source URI")
	archiveURI := flags.String("archive-uri", "", "exact public source archive URI")
	archiveOutput := flags.String("archive-output", "", "source archive output")
	sourceSBOM := flags.String("source-sbom", "", "source dependency SBOM")
	output := flags.String("output", "", "source provenance output")
	if err := parseFlags(flags, args); err != nil {
		return err
	}
	if *output == "" {
		return errors.New("output is required")
	}
	provenance, err := releaseprovenance.GenerateSource(context.Background(), releaseprovenance.SourceRequest{
		RepoDir:        *repo,
		Revision:       *revision,
		SourceURI:      *sourceURI,
		ArchiveURI:     *archiveURI,
		ArchivePath:    *archiveOutput,
		SourceSBOMPath: *sourceSBOM,
	})
	if err != nil {
		return err
	}
	data, err := releaseprovenance.MarshalSourceProvenance(provenance)
	if err != nil {
		return err
	}
	return writeExclusive(*output, data)
}

func runFinalizeReceipt(args []string) error {
	flags := newFlagSet("finalize-receipt")
	sourcePath := flags.String("source", "", "source provenance input")
	repository := flags.String("repository", "", "OCI repository")
	digest := flags.String("digest", "", "OCI image digest")
	imageSBOMReference := flags.String("image-sbom-reference", "", "OCI image SBOM reference")
	imageSBOMAttestationSHA := flags.String("image-sbom-attestation-sha256", "", "image SBOM attestation file SHA-256")
	output := flags.String("output", "", "release receipt output")
	if err := parseFlags(flags, args); err != nil {
		return err
	}
	if *output == "" {
		return errors.New("output is required")
	}
	sourceBytes, err := readBoundedRegularFile(*sourcePath)
	if err != nil {
		return err
	}
	source, err := releaseprovenance.DecodeSourceProvenance(sourceBytes)
	if err != nil {
		return err
	}
	receipt, err := releaseprovenance.FinalizeReleaseReceipt(source, releaseprovenance.OCIRelease{
		Repository:                 *repository,
		Digest:                     *digest,
		ImageSBOMReference:         *imageSBOMReference,
		ImageSBOMAttestationSHA256: *imageSBOMAttestationSHA,
	})
	if err != nil {
		return err
	}
	data, err := releaseprovenance.MarshalReleaseReceipt(receipt)
	if err != nil {
		return err
	}
	return writeExclusive(*output, data)
}

func runVerifyReceipt(args []string) error {
	flags := newFlagSet("verify-receipt")
	repo := flags.String("repo", ".", "source repository")
	receiptPath := flags.String("receipt", "", "release receipt")
	sourceSBOM := flags.String("source-sbom", "", "source dependency SBOM")
	if err := parseFlags(flags, args); err != nil {
		return err
	}
	receiptBytes, err := readBoundedRegularFile(*receiptPath)
	if err != nil {
		return err
	}
	receipt, err := releaseprovenance.DecodeReleaseReceipt(receiptBytes)
	if err != nil {
		return err
	}
	return releaseprovenance.VerifyReleaseReceipt(context.Background(), *repo, receipt, *sourceSBOM)
}

func newFlagSet(name string) *flag.FlagSet {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	return flags
}

func parseFlags(flags *flag.FlagSet, args []string) error {
	if err := flags.Parse(args); err != nil {
		return errors.New("invalid command arguments")
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	return nil
}

func readBoundedRegularFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("provenance input must be a regular non-symlink file")
	}
	if info.Size() > maxProvenanceFileBytes {
		return nil, errors.New("provenance input exceeds 4 MiB")
	}
	return os.ReadFile(path)
}

func writeExclusive(path string, data []byte) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("output path is required")
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	remove := true
	defer func() {
		file.Close()
		if remove {
			os.Remove(path)
		}
	}()
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	remove = false
	return nil
}

func boundedError(err error) string {
	const maximum = 512
	message := strings.ReplaceAll(strings.ReplaceAll(err.Error(), "\r", " "), "\n", " ")
	if len(message) > maximum {
		message = message[:maximum]
	}
	return message
}
