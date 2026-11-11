package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/vuln/scan"

	"github.com/crmarques/bootwright/tools/clientassets"
)

const maxExecutable = 1 << 30

var scanning sync.Mutex

type clientSource struct {
	cache, db string
	client    *http.Client
	pins      func(kind, version string) (clientassets.Module, error)
}

func (s clientSource) scan(entry pin) ([]byte, error) {
	asset, known := clientassets.Of(entry.Kind, entry.Default.Version)
	if !known {
		return nil, fmt.Errorf("the tool table pins %s, whose publisher file this gate does not know", entry.Kind)
	}
	if s.cache == "" || s.db == "" || s.client == nil {
		return nil, errors.New("-artifacts and -db are required to scan the clients")
	}
	sweep(s.cache)
	release, err := s.fetch(asset.URL, pinned{entry.Default.Bytes, entry.Default.SHA256})
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", entry.Kind, entry.Default.Version, err)
	}
	defer release.Close()
	staging, err := os.MkdirTemp(s.cache, ".scan-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(staging)
	executable := filepath.Join(staging, entry.Kind)
	if asset.Member != "" {
		err = extract(release, asset.Member, executable)
	} else {
		err = stage(release, executable)
	}
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", entry.Kind, entry.Default.Version, err)
	}
	scanning.Lock()
	defer scanning.Unlock()
	var out, problems bytes.Buffer
	command := scan.Command(context.Background(), "-db", s.db, "-mode", "binary", "-format", "json", executable)
	command.Stdout, command.Stderr = &out, &problems
	if err := command.Start(); err != nil {
		return nil, err
	}
	if err := command.Wait(); err != nil {
		return nil, fmt.Errorf("govulncheck could not scan %s %s; VULNCHECK=skip builds an executable stamped unchecked: %w: %s", entry.Kind, entry.Default.Version, err, bytes.TrimSpace(problems.Bytes()))
	}
	return out.Bytes(), nil
}

type pinned struct {
	bytes  int64
	sha256 string
}

func (s clientSource) buildModules(entry pin) ([]byte, error) {
	lookup := s.pins
	if lookup == nil {
		lookup = clientassets.ModulesOf
	}
	module, err := lookup(entry.Kind, entry.Default.Version)
	if err != nil {
		return nil, fmt.Errorf("the %s %s binary names no module, so its dependencies come from its release's go.mod: %w", entry.Kind, entry.Default.Version, err)
	}
	if s.cache == "" || s.client == nil {
		return nil, errors.New("-artifacts is required to read the clients' go.mod files")
	}
	sweep(s.cache)
	file, err := s.fetch(module.URL, pinned{module.Bytes, module.SHA256})
	if err != nil {
		return nil, fmt.Errorf("%s %s go.mod: %w", entry.Kind, entry.Default.Version, err)
	}
	defer file.Close()
	return io.ReadAll(io.LimitReader(file, module.Bytes+1))
}

func (s clientSource) fetch(url string, want pinned) (*os.File, error) {
	directory := filepath.Join(s.cache, want.sha256)
	cached := filepath.Join(directory, path.Base(url))
	if file, err := openVerified(cached, want); err == nil {
		return file, nil
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return nil, err
	}
	partial, err := os.CreateTemp(directory, ".part-*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(partial.Name())
	defer partial.Close()
	response, err := s.client.Get(url)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered %s", url, response.Status)
	}
	if _, err := io.Copy(partial, io.LimitReader(response.Body, want.bytes+1)); err != nil {
		return nil, err
	}
	if err := partial.Close(); err != nil {
		return nil, err
	}
	file, err := openVerified(partial.Name(), want)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", url, err)
	}
	if err := os.Rename(partial.Name(), cached); err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}

func openVerified(file string, want pinned) (*os.File, error) {
	source, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	digest := sha256.New()
	size, err := io.Copy(digest, source)
	if err == nil && (size != want.bytes || hex.EncodeToString(digest.Sum(nil)) != want.sha256) {
		err = fmt.Errorf("the file is %d bytes with another SHA-256 than the pinned %d bytes and %s", size, want.bytes, want.sha256)
	}
	if err == nil {
		_, err = source.Seek(0, io.SeekStart)
	}
	if err != nil {
		source.Close()
		return nil, err
	}
	return source, nil
}

func stage(source io.Reader, destination string) error {
	sink, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o700)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(sink, io.LimitReader(source, maxExecutable))
	return errors.Join(copyErr, sink.Close())
}

func extract(archive io.Reader, member, destination string) error {
	unzipped, err := gzip.NewReader(archive)
	if err != nil {
		return err
	}
	reader := tar.NewReader(unzipped)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("the archive holds no file named %s", member)
		}
		if err != nil {
			return err
		}
		if header.Typeflag == tar.TypeReg && path.Base(header.Name) == member && header.Size <= maxExecutable {
			return stage(io.LimitReader(reader, header.Size), destination)
		}
	}
}

func sweep(cache string) {
	entries, err := os.ReadDir(cache)
	if err != nil {
		return
	}
	stale := func(entry os.DirEntry) bool {
		info, err := entry.Info()
		return err == nil && time.Since(info.ModTime()) > 24*time.Hour
	}
	for _, entry := range entries {
		switch {
		case strings.HasPrefix(entry.Name(), ".scan-") && stale(entry):
			_ = os.RemoveAll(filepath.Join(cache, entry.Name()))
		case entry.IsDir() && len(entry.Name()) == 64:
			inside, _ := os.ReadDir(filepath.Join(cache, entry.Name()))
			for _, file := range inside {
				if strings.HasPrefix(file.Name(), ".part-") && stale(file) {
					_ = os.Remove(filepath.Join(cache, entry.Name(), file.Name()))
				}
			}
		}
	}
}
