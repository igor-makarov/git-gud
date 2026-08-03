package command

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/igor-makarov/git-gud/internal/remote"
)

// Download runs the download subcommand.
func Download(ctx context.Context, repository *remote.Repository, arguments []string) error {
	output := "."
	jobs := remote.DefaultDownloadJobs
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
		case argument == "--jobs":
			index++
			if index == len(arguments) {
				return fmt.Errorf("--jobs requires a number")
			}
			value, err := strconv.Atoi(arguments[index])
			if err != nil || value < 1 {
				return fmt.Errorf("invalid --jobs value %q", arguments[index])
			}
			jobs = value
		case strings.HasPrefix(argument, "--jobs="):
			value := strings.TrimPrefix(argument, "--jobs=")
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed < 1 {
				return fmt.Errorf("invalid --jobs value %q", value)
			}
			jobs = parsed
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
		return fmt.Errorf("usage: git gud REPOSITORY download [-o DIR] [--jobs N] PATH_OR_GLOB")
	}
	return repository.Download(ctx, positional[0], output, jobs)
}
