package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	gitgud "github.com/igor-makarov/git-gud"
	"github.com/igor-makarov/git-gud/internal/command"
	"github.com/igor-makarov/git-gud/internal/remote"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "git gud: %v\n", err)
		os.Exit(1)
	}
}

func run(arguments []string, stdout, stderr io.Writer) error {
	global := flag.NewFlagSet("git gud", flag.ContinueOnError)
	global.SetOutput(stderr)
	cacheDir := global.String("cache-dir", os.Getenv("GIT_GUD_CACHE_DIR"), "bare repository cache directory")
	batchSize := global.Int("batch-size", remote.DefaultBatchSize, "maximum object wants per smart HTTP fetch")
	showProgress := global.Bool("progress", false, "show remote Git progress")
	showVersion := global.Bool("version", false, "show version")
	showHelp := global.Bool("help", false, "show complete documentation")
	showReadme := global.Bool("readme", false, "show complete documentation")
	global.Usage = func() { usage(stderr) }
	if err := global.Parse(arguments); err != nil {
		return err
	}
	if *showVersion {
		fmt.Fprintln(stdout, version())
		return nil
	}
	if *showHelp || *showReadme {
		_, err := fmt.Fprint(stdout, gitgud.README)
		return err
	}
	args := global.Args()
	if len(args) < 2 {
		usage(stderr)
		return fmt.Errorf("expected REPOSITORY and COMMAND")
	}

	target, err := remote.ParseTarget(args[0])
	if err != nil {
		return err
	}
	var progress io.Writer
	if *showProgress {
		progress = stderr
	}
	client, err := remote.NewClient(remote.Options{
		CacheDir:  *cacheDir,
		BatchSize: *batchSize,
		Progress:  progress,
	})
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	repository, err := client.Open(ctx, target)
	if err != nil {
		return err
	}
	defer repository.Close()

	switch args[1] {
	case "ls":
		return command.List(ctx, repository, args[2:], stdout)
	case "find":
		return command.Find(ctx, repository, args[2:], stdout)
	case "cat":
		return command.Cat(ctx, repository, args[2:], stdout)
	case "download":
		return command.Download(ctx, repository, args[2:])
	default:
		return fmt.Errorf("unknown command %q", args[1])
	}
}

func usage(writer io.Writer) {
	fmt.Fprintln(writer, `Usage:
  git gud [FLAGS] REPOSITORY[@REF] COMMAND [ARGS]

Commands:
  ls [-R|--recursive] [DIR]
  find [--from DIR] GLOB
  cat PATH
  download [-o|--output DIR] [--jobs N] PATH_OR_GLOB

Flags:
  --cache-dir DIR
  --batch-size N
  --progress
  --version
  --help, --readme

Run 'git gud --help' for complete documentation.`)
}

var buildVersion string

func version() string {
	if buildVersion != "" {
		return buildVersion
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "devel"
}
