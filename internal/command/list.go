package command

import (
	"context"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/igor-makarov/git-gud/internal/remote"
)

// List runs the ls subcommand.
func List(ctx context.Context, repository *remote.Repository, arguments []string, stdout io.Writer) error {
	recursive := false
	var positional []string
	for index := 0; index < len(arguments); index++ {
		switch arguments[index] {
		case "-R", "--recursive":
			recursive = true
		case "--":
			positional = append(positional, arguments[index+1:]...)
			index = len(arguments)
		default:
			if strings.HasPrefix(arguments[index], "-") {
				return fmt.Errorf("unknown ls option %q", arguments[index])
			}
			positional = append(positional, arguments[index])
		}
	}
	if len(positional) > 1 {
		return fmt.Errorf("usage: git gud REPOSITORY ls [-R] [DIR]")
	}
	directory := "."
	if len(positional) == 1 {
		directory = positional[0]
	}
	return repository.List(ctx, directory, recursive, func(entry remote.Entry) error {
		name := entry.Path
		if !recursive {
			name = path.Base(name)
		}
		_, err := fmt.Fprintln(stdout, name)
		return err
	})
}
