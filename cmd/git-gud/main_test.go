package main

import (
	"bytes"
	"strings"
	"testing"

	gitgud "github.com/igor-makarov/git-gud"
)

func TestReadmeFlag(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run([]string{"--readme"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if stdout.String() != gitgud.README {
		t.Fatal("--readme output does not match embedded README")
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", stderr.String())
	}
}

func TestUsagePointsToReadme(t *testing.T) {
	var output bytes.Buffer
	usage(&output)
	if !strings.Contains(output.String(), "git gud --readme") {
		t.Fatalf("usage does not point to embedded documentation:\n%s", output.String())
	}
}
