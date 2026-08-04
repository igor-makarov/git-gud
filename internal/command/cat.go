package command

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/igor-makarov/git-gud/internal/remote"
)

// Cat runs the cat subcommand.
func Cat(ctx context.Context, repository *remote.Repository, arguments []string, stdout io.Writer) error {
	var positional []string
	for index := 0; index < len(arguments); index++ {
		argument := arguments[index]
		switch {
		case argument == "--":
			positional = append(positional, arguments[index+1:]...)
			index = len(arguments)
		case strings.HasPrefix(argument, "-"):
			return fmt.Errorf("unknown cat option %q", argument)
		default:
			positional = append(positional, argument)
		}
	}
	if len(positional) != 1 {
		return fmt.Errorf("usage: git gud REPOSITORY cat PATH")
	}
	return repository.Cat(ctx, positional[0], stdout)
}
