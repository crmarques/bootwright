package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
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

	"github.com/crmarques/bootwright/tools/verifiedfetch"

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
	release, err := s.fetch(asset.URL, verifiedfetch.Pin{Name: entry.Kind + " " + entry.Default.Version, Bytes: entry.Default.Bytes, SHA256: entry.Default.SHA256})
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
	file, err := s.fetch(module.URL, verifiedfetch.Pin{Name: entry.Kind + " " + entry.Default.Version + " go.mod", Bytes: module.Bytes, SHA256: module.SHA256})
	if err != nil {
		return nil, fmt.Errorf("%s %s go.mod: %w", entry.Kind, entry.Default.Version, err)
	}
	defer file.Close()
	return io.ReadAll(io.LimitReader(file, module.Bytes+1))
}

func (s clientSource) fetch(url string, want verifiedfetch.Pin) (*os.File, error) {
	return verifiedfetch.Cached(s.client, url, filepath.Join(s.cache, want.SHA256), want)
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
