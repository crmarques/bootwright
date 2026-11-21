// Command runtimewheels places every file the controller runtime lock names
// into the directory the executable embeds, so a build carries the wheels its
// install builds the runtime from and installation downloads none.
// Each file is checked against the lock's size and SHA-256 before it takes its
// name; a cache directory keeps verified files across builds and worktrees.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/crmarques/bootwright/tools/verifiedfetch"
)

const (
	lockOrigin = "https://files.pythonhosted.org/"
	maxFile    = 1 << 30
)

type file struct {
	Filename string `json:"filename"`
	URL      string `json:"url"`
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size"`
}

type lock struct {
	Installer file              `json:"installer"`
	ABIs      map[string][]file `json:"abis"`
}

type options struct {
	lock, dest, cache, files string
	client                   *http.Client
}

type summary struct{ present, cached, downloaded, removed int }

func main() {
	var o options
	flag.StringVar(&o.lock, "lock", "", "the runtime lock")
	flag.StringVar(&o.dest, "dest", "", "the directory the executable embeds")
	flag.StringVar(&o.cache, "cache", "", "a directory that keeps verified files across builds")
	flag.StringVar(&o.files, "files", strings.TrimSuffix(lockOrigin, "/"), "an https:// or file:// base serving the lock's files.pythonhosted.org paths")
	flag.Parse()
	o.client = &http.Client{Timeout: 10 * time.Minute, Transport: http.DefaultTransport}
	done, err := place(o)
	if err != nil {
		fmt.Fprintln(os.Stderr, "runtimewheels:", err)
		os.Exit(1)
	}
	fmt.Printf("runtimewheels: %s holds the %d locked files (%d downloaded, %d from the cache, %d already there, %d stale removed)\n",
		o.dest, done.present+done.cached+done.downloaded, done.downloaded, done.cached, done.present, done.removed)
}

func place(o options) (summary, error) {
	var done summary
	if o.lock == "" || o.dest == "" {
		return done, errors.New("-lock and -dest are required")
	}
	files, err := lockedFiles(o.lock)
	if err != nil {
		return done, err
	}
	for _, directory := range []string{o.dest, o.cache} {
		if directory != "" {
			if err := os.MkdirAll(directory, 0o755); err != nil {
				return done, err
			}
		}
	}
	for _, f := range files {
		destination := filepath.Join(o.dest, f.Filename)
		cached := filepath.Join(o.cache, f.SHA256, f.Filename)
		switch {
		case verifiedfetch.Verified(destination, f.pin()) == nil:
			done.present++
			continue
		case o.cache != "" && verifiedfetch.Verified(cached, f.pin()) == nil:
			done.cached++
		default:
			if o.cache == "" {
				cached = destination
			}
			if err := os.MkdirAll(filepath.Dir(cached), 0o755); err != nil {
				return done, err
			}
			if err := verifiedfetch.Write(cached, f.pin(), 0o644, fetch(o, f)); err != nil {
				return done, err
			}
			done.downloaded++
			if cached == destination {
				continue
			}
		}
		if err := verifiedfetch.Write(destination, f.pin(), 0o644, verifiedfetch.CopyFile(cached)); err != nil {
			return done, err
		}
	}
	entries, err := os.ReadDir(o.dest)
	if err != nil {
		return done, err
	}
	for _, entry := range entries {
		name := entry.Name()
		if name == ".gitignore" || slices.ContainsFunc(files, func(f file) bool { return f.Filename == name }) {
			continue
		}
		if err := os.RemoveAll(filepath.Join(o.dest, name)); err != nil {
			return done, err
		}
		done.removed++
	}
	return done, nil
}

func lockedFiles(path string) ([]file, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var parsed lock
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("%s is not a runtime lock: %w", path, err)
	}
	unique := map[string]file{}
	all := []file{parsed.Installer}
	for _, files := range parsed.ABIs {
		all = append(all, files...)
	}
	for _, f := range all {
		if f.Filename == "" || f.Filename != filepath.Base(f.Filename) || !strings.HasSuffix(f.Filename, ".whl") ||
			!strings.HasPrefix(f.URL, lockOrigin) || !strings.HasSuffix(f.URL, "/"+f.Filename) || len(f.SHA256) != 64 || f.Size < 1 || f.Size > maxFile {
			return nil, fmt.Errorf("%s names a file that is not a files.pythonhosted.org wheel with a size and SHA-256: %q", path, f.Filename)
		}
		if seen, found := unique[f.Filename]; found && seen != f {
			return nil, fmt.Errorf("%s names %s twice with different identities", path, f.Filename)
		}
		unique[f.Filename] = f
	}
	files := make([]file, 0, len(unique))
	for _, f := range unique {
		files = append(files, f)
	}
	slices.SortFunc(files, func(a, b file) int { return strings.Compare(a.Filename, b.Filename) })
	return files, nil
}

func fetch(o options, f file) func(io.Writer) error {
	base, err := url.Parse(o.files)
	if err != nil || (base.Scheme != "https" && base.Scheme != "http" && base.Scheme != "file") {
		return func(io.Writer) error {
			return fmt.Errorf("-files %q is not an https://, http:// or file:// base", o.files)
		}
	}
	suffix := strings.TrimPrefix(f.URL, lockOrigin)
	if base.Scheme == "file" {
		return verifiedfetch.CopyFile(filepath.Join(base.Path, filepath.FromSlash(suffix)))
	}
	return verifiedfetch.Download(o.client, strings.TrimSuffix(o.files, "/")+"/"+suffix, f.pin())
}

func (f file) pin() verifiedfetch.Pin {
	return verifiedfetch.Pin{Name: f.Filename, Bytes: f.Size, SHA256: f.SHA256}
}
