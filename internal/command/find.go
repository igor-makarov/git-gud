package command

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/igor-makarov/git-gud/internal/remote"
)

// Find runs the find subcommand.
func Find(ctx context.Context, repository *remote.Repository, arguments []string, stdout io.Writer) error {
	from := "."
	var positional []string
	for index := 0; index < len(arguments); index++ {
		argument := arguments[index]
		switch {
		case argument == "--from":
			index++
			if index == len(arguments) {
				return fmt.Errorf("--from requires a directory")
			}
			from = arguments[index]
		case strings.HasPrefix(argument, "--from="):
			from = strings.TrimPrefix(argument, "--from=")
		case argument == "--":
			positional = append(positional, arguments[index+1:]...)
			index = len(arguments)
		case strings.HasPrefix(argument, "-"):
			return fmt.Errorf("unknown find option %q", argument)
		default:
			positional = append(positional, argument)
		}
	}
	if len(positional) != 1 {
		return fmt.Errorf("usage: git gud REPOSITORY find [--from DIR] GLOB")
	}
	return repository.Find(ctx, from, positional[0], func(entry remote.Entry) error {
		_, err := fmt.Fprintln(stdout, entry.Path)
		return err
	})
}
