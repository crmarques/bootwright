// Command runtimewheels places every file the controller runtime lock names
// into the directory the executable embeds, so a build carries the wheels its
// controller install builds the runtime from and installation downloads none.
// Each file is checked against the lock's size and SHA-256 before it takes its
// name; a cache directory keeps verified files across builds and worktrees.
package main

import (
	"crypto/sha256"
	"encoding/hex"
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
		case verified(destination, f) == nil:
			done.present++
			continue
		case o.cache != "" && verified(cached, f) == nil:
			done.cached++
		default:
			if o.cache == "" {
				cached = destination
			}
			if err := os.MkdirAll(filepath.Dir(cached), 0o755); err != nil {
				return done, err
			}
			if err := write(cached, f, func(sink io.Writer) error { return fetch(o, f, sink) }); err != nil {
				return done, err
			}
			done.downloaded++
			if cached == destination {
				continue
			}
		}
		if err := write(destination, f, func(sink io.Writer) error { return copyFile(cached, sink) }); err != nil {
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

func fetch(o options, f file, sink io.Writer) error {
	base, err := url.Parse(o.files)
	if err != nil || (base.Scheme != "https" && base.Scheme != "http" && base.Scheme != "file") {
		return fmt.Errorf("-files %q is not an https://, http:// or file:// base", o.files)
	}
	suffix := strings.TrimPrefix(f.URL, lockOrigin)
	if base.Scheme == "file" {
		return copyFile(filepath.Join(base.Path, filepath.FromSlash(suffix)), sink)
	}
	response, err := o.client.Get(strings.TrimSuffix(o.files, "/") + "/" + suffix)
	if err != nil {
		return fmt.Errorf("%s: %w", f.Filename, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: the server answered %s", f.Filename, response.Status)
	}
	_, err = io.Copy(sink, io.LimitReader(response.Body, f.Size+1))
	return err
}

func copyFile(path string, sink io.Writer) error {
	source, err := os.Open(path)
	if err != nil {
		return err
	}
	defer source.Close()
	_, err = io.Copy(sink, source)
	return err
}

// write gives a file its name only once its bytes match the lock.
func write(path string, f file, fill func(io.Writer) error) error {
	partial, err := os.CreateTemp(filepath.Dir(path), ".part-*")
	if err != nil {
		return err
	}
	defer os.Remove(partial.Name())
	defer partial.Close()
	if err := fill(partial); err != nil {
		return err
	}
	if _, err := partial.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if err := matches(partial, f); err != nil {
		return err
	}
	if err := partial.Chmod(0o644); err != nil {
		return err
	}
	if err := partial.Close(); err != nil {
		return err
	}
	return os.Rename(partial.Name(), path)
}

func verified(path string, f file) error {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() != f.Size {
		return errors.New("absent")
	}
	handle, err := os.Open(path)
	if err != nil {
		return err
	}
	defer handle.Close()
	return matches(handle, f)
}

func matches(reader io.Reader, f file) error {
	hash := sha256.New()
	written, err := io.Copy(hash, io.LimitReader(reader, f.Size+1))
	if err != nil || written != f.Size || hex.EncodeToString(hash.Sum(nil)) != f.SHA256 {
		return fmt.Errorf("%s does not match its locked size and SHA-256", f.Filename)
	}
	return nil
}
