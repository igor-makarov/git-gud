package main

import (
	"bytes"
	"strings"
	"testing"

	gitgud "github.com/igor-makarov/git-gud"
)

func TestDocumentationFlags(t *testing.T) {
	for _, option := range []string{"--help", "--readme"} {
		t.Run(option, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			if err := run([]string{option}, &stdout, &stderr); err != nil {
				t.Fatal(err)
			}
			if stdout.String() != gitgud.README {
				t.Fatalf("%s output does not match embedded README", option)
			}
			if stderr.Len() != 0 {
				t.Fatalf("unexpected stderr: %s", stderr.String())
			}
		})
	}
}

func TestBuildVersion(t *testing.T) {
	previous := buildVersion
	buildVersion = "v1.2.3"
	t.Cleanup(func() { buildVersion = previous })
	if got := version(); got != "v1.2.3" {
		t.Fatalf("version() = %q, want v1.2.3", got)
	}
}

func TestUsagePointsToHelp(t *testing.T) {
	var output bytes.Buffer
	usage(&output)
	if !strings.Contains(output.String(), "git gud --help") {
		t.Fatalf("usage does not point to embedded documentation:\n%s", output.String())
	}
}
