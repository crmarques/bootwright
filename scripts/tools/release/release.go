package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"time"
)

const (
	sums     = "SHA256SUMS"
	bom      = "bootwright.cdx.json"
	notices  = "bootwright.notices.txt"
	binary   = "bootwright"
	buildDir = ".build."
)

var (
	pinnedImage = regexp.MustCompile(`^[^@\s]+@sha256:[0-9a-f]{64}$`)
	passed      = regexp.MustCompile(`^passed [0-9]{4}-[0-9]{2}-[0-9]{2}$`)
)

type commands interface {
	output(name string, arguments ...string) ([]byte, error)
	run(ctx context.Context, environment []string, stdout, stderr io.Writer, name string, arguments ...string) error
}

type systemCommands struct{}

func (systemCommands) output(name string, arguments ...string) ([]byte, error) {
	return exec.Command(name, arguments...).Output()
}

func (systemCommands) run(ctx context.Context, environment []string, stdout, stderr io.Writer, name string, arguments ...string) error {
	command := exec.CommandContext(ctx, name, arguments...)
	command.Env = append([]string{}, environment...)
	command.Stdout, command.Stderr = stdout, stderr
	return command.Run()
}

type identity struct {
	Result struct {
		Version            string `json:"version"`
		Commit             string `json:"commit"`
		Source             string `json:"source"`
		VulnerabilityCheck string `json:"vulnerabilityCheck"`
	} `json:"result"`
}

func release(ctx context.Context, c config, sys commands, environ []string, stdout, stderr io.Writer) error {
	if err := refuseInvocation(c); err != nil {
		return err
	}
	head, err := identify(c, sys)
	if err != nil {
		return err
	}
	files := strings.Fields(c.Files)
	if err := outputsUsable(c.Dir, append(append([]string{}, files...), sums)); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(c.Dir, buildDir)
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if err := build(ctx, c, sys, passedEnvironment(c, environ), stage, files, stdout, stderr); err != nil {
		return err
	}
	if err := cleanTree(sys); err != nil {
		return err
	}
	if err := verify(c, sys, stage, head, files); err != nil {
		return err
	}
	if err := ship(c.Dir, stage); err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "release: %s holds %s %s of %s, commit %s\n", c.Dir, c.Files, sums, c.Version, head)
	return err
}

func refuseInvocation(c config) error {
	if c.MakeFlags != "" {
		return fmt.Errorf("make was run with -%s, which would run only part of the recipe or ignore a refusal; run it without -n, -i, -t and -q", c.MakeFlags)
	}
	if len(strings.Fields(c.MakefileList)) != 1 || c.Eval {
		extra := ""
		if c.Eval {
			extra = " and --eval"
		}
		return fmt.Errorf("make reads more than the Makefile (%s%s), which could set any variable; a release reads the Makefile alone", c.MakefileList, extra)
	}
	if len(c.Foreign) != 0 {
		var origins []string
		for _, entry := range c.Foreign {
			name, origin, _ := strings.Cut(entry, "=")
			origins = append(origins, name+" comes from the "+origin+";")
		}
		return fmt.Errorf("%s each could change what is built, checked or stamped; a release takes it from the Makefile and only %s from you", strings.Join(origins, " "), c.Inputs)
	}
	if c.GoImage != "" && !pinnedImage.MatchString(c.GoImage) {
		return fmt.Errorf("GO_IMAGE=%s is not pinned by @sha256:<digest>, so the release would compile with whatever its tag names today", c.GoImage)
	}
	if c.Vulncheck != "on" {
		return fmt.Errorf("VULNCHECK=%s is refused; a release is built with the vulnerability gate on", c.Vulncheck)
	}
	if c.Build != "native" && c.Build != "container" {
		return fmt.Errorf("RELEASE_BUILD=%s is neither native nor container", c.Build)
	}
	if c.Version == "" || strings.Contains(c.Version, "dirty") {
		return fmt.Errorf("name the release with VERSION=<version>; '%s' is none or names a dirty tree", c.Version)
	}
	return nil
}

func identify(c config, sys commands) (string, error) {
	out, err := sys.output("git", "rev-parse", "HEAD")
	if err != nil {
		return "", errors.New("this is no Git checkout, so no commit identifies the build")
	}
	head := strings.TrimSpace(string(out))
	if head != c.Commit {
		return "", fmt.Errorf("COMMIT=%s is not the checkout's commit %s", c.Commit, head)
	}
	if err := cleanTree(sys); err != nil {
		return "", err
	}
	if c.Source != "clean" {
		return "", fmt.Errorf("SOURCE=%s is not clean", c.Source)
	}
	if c.Build == "native" {
		embedded, err := sys.output("git", "status", "--porcelain", "--ignored", "--", "ansible/collections")
		if err != nil {
			return "", errors.New("git status failed")
		}
		if len(bytes.TrimSpace(embedded)) != 0 {
			return "", fmt.Errorf("ignored files under ansible/collections would be embedded in the executable:\n%s", bytes.TrimRight(embedded, "\n"))
		}
	}
	return head, nil
}

func cleanTree(sys commands) error {
	changes, err := sys.output("git", "status", "--porcelain")
	if err != nil {
		return errors.New("git status failed")
	}
	if len(changes) != 0 {
		return fmt.Errorf("the tree has modified or untracked files, so the build would not be its commit:\n%s", bytes.TrimRight(changes, "\n"))
	}
	return nil
}

func outputsUsable(dir string, names []string) error {
	if err := os.MkdirAll(dir, 0o777); err != nil {
		return err
	}
	for _, name := range names {
		info, err := os.Stat(filepath.Join(dir, name))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		refused := fmt.Errorf("%s/%s is a directory or belongs to another user; remove it or choose another RELEASE_DIR", dir, name)
		if err != nil || info.IsDir() {
			return refused
		}
		if owner, known := info.Sys().(*syscall.Stat_t); !known || int(owner.Uid) != os.Geteuid() {
			return refused
		}
	}
	return nil
}

func passedEnvironment(c config, environ []string) []string {
	wanted := map[string]bool{}
	for _, name := range strings.Fields(c.Environment) {
		wanted[name] = true
	}
	var kept []string
	for _, entry := range environ {
		if name, _, _ := strings.Cut(entry, "="); wanted[name] {
			kept = append(kept, entry)
		}
	}
	return kept
}

func build(ctx context.Context, c config, sys commands, passed []string, stage string, files []string, stdout, stderr io.Writer) error {
	stamp := []string{"--no-print-directory"}
	environment := append(append([]string{}, passed...), "GOENV=off")
	if c.Build == "native" {
		for _, name := range files {
			if err := os.Remove(filepath.Join("bin", name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
		}
		environment = append(environment, "GOFLAGS=-mod=readonly")
		arguments := append(stamp, "build", "VULNCHECK=on", "VERSION="+c.Version, "COMMIT="+c.Commit, "SOURCE="+c.Source)
		if err := sys.run(ctx, environment, stdout, stderr, c.Make, arguments...); err != nil {
			return fmt.Errorf("the build failed: %w", err)
		}
		for _, name := range files {
			if err := copyFile(filepath.Join("bin", name), filepath.Join(stage, name)); err != nil {
				return err
			}
		}
		return nil
	}
	arguments := append(stamp, "container-build", "VULNCHECK=on", "VERSION="+c.Version, "COMMIT="+c.Commit, "SOURCE="+c.Source,
		"CONTAINER_OUT="+stage, "CONTAINER_ENGINE="+c.Engine, "GO_IMAGE="+c.GoImage, "PROXY_CA="+c.ProxyCA)
	if err := sys.run(ctx, environment, stdout, stderr, c.Make, arguments...); err != nil {
		return fmt.Errorf("the build failed: %w", err)
	}
	return nil
}

func copyFile(from, to string) error {
	info, err := os.Stat(from)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(from)
	if err != nil {
		return err
	}
	return os.WriteFile(to, data, info.Mode().Perm())
}

func verify(c config, sys commands, stage, head string, files []string) error {
	for _, name := range files {
		info, err := os.Stat(filepath.Join(stage, name))
		if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
			return fmt.Errorf("%s/%s is missing or empty", stage, name)
		}
	}
	executable := filepath.Join(stage, binary)
	if info, err := os.Stat(executable); err != nil || info.Mode().Perm()&0o100 == 0 {
		return fmt.Errorf("%s is not executable", executable)
	}
	if err := verifyIdentity(c, sys, executable, head); err != nil {
		return err
	}
	if err := verifyDocuments(sys, stage, executable); err != nil {
		return err
	}
	return writeSums(stage, files)
}

func verifyIdentity(c config, sys commands, executable, head string) error {
	out, err := sys.output(executable, "version", "--output", "json")
	if err != nil {
		return errors.New("the built executable cannot report its version")
	}
	var reported identity
	if err := json.Unmarshal(out, &reported); err != nil {
		return fmt.Errorf("the built executable's version output is not JSON: %w", err)
	}
	result := reported.Result
	if !passed.MatchString(result.VulnerabilityCheck) {
		return fmt.Errorf("the executable's vulnerability check is '%s', not a pass; a release needs the gate to have passed", result.VulnerabilityCheck)
	}
	if result.VulnerabilityCheck != "passed "+time.Now().UTC().Format(time.DateOnly) {
		return fmt.Errorf("the executable's vulnerability check is '%s'; rebuild with a fresh UTC-day verdict", result.VulnerabilityCheck)
	}
	if result.Version != c.Version {
		return fmt.Errorf("the executable reports version '%s', not '%s'", result.Version, c.Version)
	}
	if result.Commit != head {
		return fmt.Errorf("the executable reports commit '%s', not this checkout's %s", result.Commit, head)
	}
	if result.Source != "clean" {
		return fmt.Errorf("the executable reports source '%s', not clean", result.Source)
	}
	return nil
}

func verifyDocuments(sys commands, stage, executable string) error {
	written, err := sys.output(executable, "sbom", "--format", "cyclonedx-json")
	if err != nil {
		return errors.New("the built executable cannot write its bill of materials")
	}
	shipped, err := os.ReadFile(filepath.Join(stage, bom))
	if err != nil || !bytes.Equal(written, shipped) {
		return fmt.Errorf("%s is not the bill of materials of this executable", bom)
	}
	digest := sha256.Sum256(shipped)
	text, err := os.ReadFile(filepath.Join(stage, notices))
	if err != nil || !slices.Contains(strings.Split(string(text), "\n"), "Bill of materials: sha256:"+hex.EncodeToString(digest[:])) {
		return fmt.Errorf("%s was not composed from this bill of materials", notices)
	}
	return nil
}

func fileDigest(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func writeSums(stage string, files []string) error {
	var lines strings.Builder
	for _, name := range files {
		digest, err := fileDigest(filepath.Join(stage, name))
		if err != nil {
			return err
		}
		fmt.Fprintf(&lines, "%s  %s\n", digest, name)
	}
	if err := os.WriteFile(filepath.Join(stage, sums), []byte(lines.String()), 0o644); err != nil {
		return err
	}
	return checkSums(stage, files)
}

func checkSums(stage string, files []string) error {
	data, err := os.ReadFile(filepath.Join(stage, sums))
	if err != nil {
		return fmt.Errorf("%s does not verify the staged files", sums)
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(lines) != len(files) {
		return fmt.Errorf("%s does not verify the staged files", sums)
	}
	for index, name := range files {
		digest, err := fileDigest(filepath.Join(stage, name))
		if err != nil || lines[index] != digest+"  "+name {
			return fmt.Errorf("%s does not verify the staged files", sums)
		}
	}
	return nil
}

func ship(dir, stage string) error {
	if err := os.Remove(filepath.Join(dir, sums)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	for _, name := range []string{bom, notices, binary, sums} {
		if err := os.Rename(filepath.Join(stage, name), filepath.Join(dir, name)); err != nil {
			return err
		}
	}
	return nil
}
