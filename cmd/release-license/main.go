package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/licenseinventory"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "verify" {
		fmt.Fprintln(stderr, "license verification command is required")
		return 2
	}
	flags := flag.NewFlagSet("verify", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	repo := flags.String("repo", ".", "source repository")
	allowHold := flags.Bool("allow-hold", false, "permit a documented hold for non-release verification")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 {
		fmt.Fprintln(stderr, "license verification arguments are invalid")
		return 2
	}
	report, err := licenseinventory.VerifyRepository(*repo, *allowHold)
	if err != nil {
		message := strings.ReplaceAll(strings.ReplaceAll(err.Error(), "\r", " "), "\n", " ")
		if len(message) > 512 {
			message = message[:512]
		}
		fmt.Fprintln(stderr, message)
		return 1
	}
	data, err := common.Marshal(struct {
		SchemaVersion      string `json:"schema_version"`
		Status             string `json:"status"`
		UnresolvedPackages int    `json:"unresolved_packages"`
	}{
		SchemaVersion:      "new-api-license-verification/v1",
		Status:             report.ReviewStatus,
		UnresolvedPackages: report.UnresolvedPackages,
	})
	if err != nil {
		fmt.Fprintln(stderr, "license verification output failed")
		return 1
	}
	_, _ = fmt.Fprintln(stdout, string(data))
	return 0
}
