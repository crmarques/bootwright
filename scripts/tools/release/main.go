// Command release builds, checks and ships a release into one directory. It
// is the whole body of the Makefile's release recipe, so no make flag, target
// or prerequisite can skip a check: it refuses what make would have run only
// in part, then builds in a private directory of the release directory, holds
// the staged files to every check and replaces the shipped files only when all
// pass. It tags, signs, pushes and publishes nothing.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

type listFlag []string

func (l *listFlag) String() string     { return strings.Join(*l, ",") }
func (l *listFlag) Set(v string) error { *l = append(*l, v); return nil }

type config struct {
	Root         string
	MakeFlags    string
	MakefileList string
	Eval         bool
	Foreign      listFlag
	Inputs       string
	GoImage      string
	Vulncheck    string
	Build        string
	Version      string
	Commit       string
	Source       string
	Dir          string
	Engine       string
	ProxyCA      string
	Make         string
	Files        string
	Environment  string
}

func parse(arguments []string) (config, error) {
	var c config
	set := flag.NewFlagSet("release", flag.ContinueOnError)
	set.StringVar(&c.Root, "root", "", "the checkout")
	set.StringVar(&c.MakeFlags, "make-flags", "", "the flags of make that would run only part of the recipe, as make reports them")
	set.StringVar(&c.MakefileList, "makefile-list", "", "the makefiles make read")
	set.BoolVar(&c.Eval, "eval", false, "make was given --eval")
	set.Var(&c.Foreign, "foreign", "NAME=origin of a variable the release does not take from the caller; repeatable")
	set.StringVar(&c.Inputs, "inputs", "", "the variables a release takes from the caller")
	set.StringVar(&c.GoImage, "go-image", "", "GO_IMAGE")
	set.StringVar(&c.Vulncheck, "vulncheck", "", "VULNCHECK")
	set.StringVar(&c.Build, "build", "", "RELEASE_BUILD")
	set.StringVar(&c.Version, "version", "", "VERSION")
	set.StringVar(&c.Commit, "commit", "", "COMMIT")
	set.StringVar(&c.Source, "source", "", "SOURCE")
	set.StringVar(&c.Dir, "dir", "", "RELEASE_DIR")
	set.StringVar(&c.Engine, "engine", "", "CONTAINER_ENGINE")
	set.StringVar(&c.ProxyCA, "proxy-ca", "", "PROXY_CA")
	set.StringVar(&c.Make, "make", "make", "the make that runs the build")
	set.StringVar(&c.Files, "files", "", "RELEASE_FILES")
	set.StringVar(&c.Environment, "environment", "", "RELEASE_ENVIRONMENT")
	if err := set.Parse(arguments); err != nil {
		return c, err
	}
	if c.Root == "" || c.Dir == "" || c.Files == "" {
		return c, fmt.Errorf("-root, -dir and -files are required")
	}
	return c, nil
}

func main() {
	c, err := parse(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "release:", err)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := os.Chdir(c.Root); err != nil {
		fmt.Fprintln(os.Stderr, "release:", err)
		os.Exit(1)
	}
	if err := release(ctx, c, systemCommands{}, os.Environ(), os.Stdout, os.Stderr); err != nil {
		if ctx.Err() != nil {
			os.Exit(130)
		}
		fmt.Fprintln(os.Stderr, "release:", err)
		os.Exit(1)
	}
}
