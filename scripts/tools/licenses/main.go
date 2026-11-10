// Command licenses keeps and uses the license record of what the executable
// embeds. refresh reads each Go module's license file from the module cache
// and the toolchain's own, classifies it against known texts and writes the
// table the executable embeds; check recomputes the table and fails when it
// differs; notices writes the third-party notices of every component a bill of
// materials marks as embedded, from that table and from the wheels the
// executable carries, and fails when a component names no license or has no
// text. Only refresh and check read the module cache; a build reads the table.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
)

func main() {
	flags := flag.NewFlagSet("licenses", flag.ExitOnError)
	root := flags.String("root", ".", "the repository root, for refresh and check")
	table := flags.String("table", "cmd/bootwright/go-licenses.json", "the Go license table")
	sbom := flags.String("sbom", "", "the bill of materials, for notices")
	wheels := flags.String("wheels", "internal/controller/runtimelock/wheels", "the directory of carried wheels, for notices")
	output := flags.String("out", "", "the notices file, for notices")
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: licenses [flags] refresh|check|notices")
		os.Exit(2)
	}
	if err := flags.Parse(os.Args[2:]); err != nil {
		os.Exit(2)
	}
	if err := run(os.Args[1], *root, *table, *sbom, *wheels, *output); err != nil {
		fmt.Fprintln(os.Stderr, "licenses:", err)
		os.Exit(1)
	}
}

func run(command, root, table, sbom, wheels, output string) error {
	switch command {
	case "refresh", "check":
		fresh, err := composeTable(root)
		if err != nil {
			return err
		}
		if command == "refresh" {
			return os.WriteFile(table, fresh, 0o644)
		}
		recorded, err := os.ReadFile(table)
		if err != nil {
			return err
		}
		if !bytes.Equal(recorded, fresh) {
			return errors.New(table + " differs from the licenses of the modules the build names; run make licenses")
		}
		fmt.Println("licenses:", table, "matches the module cache")
		return nil
	case "notices":
		if sbom == "" || output == "" {
			return errors.New("notices needs -sbom and -out")
		}
		return writeNotices(sbom, table, wheels, output)
	}
	return errors.New("usage: licenses [flags] refresh|check|notices")
}
