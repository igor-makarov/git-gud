package command

import (
	"context"
	"fmt"
	"strings"

	"github.com/igor-makarov/git-gud/internal/remote"
)

// Download runs the download subcommand.
func Download(ctx context.Context, repository *remote.Repository, arguments []string) error {
	output := "."
	var positional []string
	for index := 0; index < len(arguments); index++ {
		argument := arguments[index]
		switch {
		case argument == "-o" || argument == "--output":
			index++
			if index == len(arguments) {
				return fmt.Errorf("%s requires a directory", argument)
			}
			output = arguments[index]
		case strings.HasPrefix(argument, "--output="):
			output = strings.TrimPrefix(argument, "--output=")
		case argument == "--":
			positional = append(positional, arguments[index+1:]...)
			index = len(arguments)
		case strings.HasPrefix(argument, "-"):
			return fmt.Errorf("unknown download option %q", argument)
		default:
			positional = append(positional, argument)
		}
	}
	if len(positional) != 1 {
		return fmt.Errorf("usage: git gud REPOSITORY download [-o DIR] DIR")
	}
	return repository.Download(ctx, positional[0], output)
}
