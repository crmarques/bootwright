package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"

	"github.com/crmarques/bootwright/tools/clientassets"
	"github.com/crmarques/bootwright/tools/verifiedfetch"
	"golang.org/x/vuln/scan"
)

const recordsFormat = "bootwright.client-scan-records-v1"
const maxRecordBytes = 64 << 20
const recordsFilename = "lock.json"

type recordLock struct {
	Format  string   `json:"format"`
	Scanner string   `json:"scanner"`
	Records []record `json:"records"`
}

type record struct {
	Kind          string `json:"kind"`
	Version       string `json:"version"`
	ReleaseSHA256 string `json:"releaseSHA256"`
	RecordSHA256  string `json:"recordSHA256"`
	Bytes         int64  `json:"bytes"`
}

type recordsSource struct {
	directory, db string
	records       map[string]record
}

func scannerVersion() (string, error) {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, dep := range info.Deps {
			if dep.Path == "golang.org/x/vuln" && dep.Replace == nil && dep.Version != "" {
				return dep.Version, nil
			}
		}
	}
	return "", errors.New("the scanner has no pinned module version")
}

func openRecords(directory, db, tablePath string) (recordsSource, error) {
	var source recordsSource
	if directory == "" || db == "" {
		return source, errors.New("-records and -db are required to scan the pinned clients")
	}
	pins, err := readTable(tablePath)
	if err != nil {
		return source, err
	}
	data, err := os.ReadFile(filepath.Join(directory, recordsFilename))
	if err != nil {
		return source, fmt.Errorf("read the client scan lock: %w", err)
	}
	var lock recordLock
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&lock); err != nil {
		return source, fmt.Errorf("read the client scan lock: %w", err)
	}
	version, err := scannerVersion()
	if err != nil {
		return source, err
	}
	if !errors.Is(decoder.Decode(new(any)), io.EOF) || lock.Format != recordsFormat ||
		lock.Scanner != version || len(lock.Records) != len(pins) || len(pins) == 0 {
		return source, errors.New("the client scan lock does not match the pinned scanner or tool table; run make tool-records-refresh")
	}
	records := make(map[string]record, len(pins))
	for i, pin := range pins {
		r := lock.Records[i]
		_, known := clientassets.Of(r.Kind, r.Version)
		digest, err := hex.DecodeString(r.RecordSHA256)
		if !known || r.Kind != pin.Kind || r.Version != pin.Default.Version || r.ReleaseSHA256 != pin.Default.SHA256 ||
			r.Bytes <= 0 || r.Bytes > maxRecordBytes || err != nil || len(digest) != sha256.Size ||
			records[r.Kind].Kind != "" {
			return source, fmt.Errorf("the client scan record does not match %s %s; run make tool-records-refresh", pin.Kind, pin.Default.Version)
		}
		records[r.Kind] = r
	}
	return recordsSource{directory: directory, db: db, records: records}, nil
}

func (s recordsSource) scan(entry pin) ([]byte, error) {
	r := s.records[entry.Kind]
	name := entry.Kind + " " + entry.Default.Version + " scan record"
	f, err := verifiedfetch.Open(filepath.Join(s.directory, r.Kind+".json.gz"),
		verifiedfetch.Pin{Name: name, Bytes: r.Bytes, SHA256: r.RecordSHA256})
	if err != nil {
		return nil, fmt.Errorf("the reviewed %s is missing or changed; restore it or run make tool-records-refresh", name)
	}
	defer f.Close()
	reader, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	dir, err := os.MkdirTemp("", "bootwright-scan-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "record")
	if err := stage(io.LimitReader(reader, maxRecordBytes+1), path); err != nil {
		return nil, err
	}
	if info, err := os.Stat(path); err != nil || info.Size() > maxRecordBytes {
		return nil, fmt.Errorf("%s exceeds the scan input size bound or is unreadable", name)
	}
	scanning.Lock()
	defer scanning.Unlock()
	var out, problems bytes.Buffer
	command := scan.Command(context.Background(), "-db", s.db, "-mode", "binary", "-format", "json", path)
	command.Stdout, command.Stderr = &out, &problems
	if err := command.Start(); err != nil {
		return nil, err
	}
	if err := command.Wait(); err != nil {
		return nil, fmt.Errorf("govulncheck could not scan %s: %w: %s", name, err, bytes.TrimSpace(problems.Bytes()))
	}
	return out.Bytes(), nil
}

func (s recordsSource) buildModules(entry pin) ([]byte, error) {
	module, err := clientassets.ModulesOf(entry.Kind, entry.Default.Version)
	if err != nil {
		return nil, err
	}
	name := entry.Kind + " " + entry.Default.Version + " go.mod"
	f, err := verifiedfetch.Open(filepath.Join(s.directory, module.Kind+".gomod"),
		verifiedfetch.Pin{Name: name, Bytes: module.Bytes, SHA256: module.SHA256})
	if err != nil {
		return nil, fmt.Errorf("the reviewed %s is missing or changed; restore it or run make tool-records-refresh", name)
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, module.Bytes+1))
}

func refreshRecords(source clientSource, tablePath, directory string, verify bool) error {
	pins, err := readTable(tablePath)
	if err != nil {
		return err
	}
	scanner, err := scannerVersion()
	if err != nil {
		return err
	}
	if !verify {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return err
		}
	}
	lock := recordLock{Format: recordsFormat, Scanner: scanner}
	for _, pin := range pins {
		extracted, err := source.extractRecord(pin)
		if err != nil {
			return err
		}
		var compressed bytes.Buffer
		writer := gzip.NewWriter(&compressed)
		if _, err := writer.Write(extracted); err != nil {
			return err
		}
		if err := writer.Close(); err != nil {
			return err
		}
		hash := sha256.Sum256(compressed.Bytes())
		r := record{Kind: pin.Kind, Version: pin.Default.Version, ReleaseSHA256: pin.Default.SHA256,
			RecordSHA256: hex.EncodeToString(hash[:]), Bytes: int64(compressed.Len())}
		if err := publishRecord(directory, pin.Kind+".json.gz", compressed.Bytes(), verify); err != nil {
			return err
		}
		lock.Records = append(lock.Records, r)
		if _, err := clientassets.ModulesOf(pin.Kind, pin.Default.Version); errors.Is(err, clientassets.ErrUnpinned) {
			continue
		} else if err != nil {
			return err
		}
		data, err := source.buildModules(pin)
		if err != nil {
			return err
		}
		if err := publishRecord(directory, pin.Kind+".gomod", data, verify); err != nil {
			return err
		}
	}
	data, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return err
	}
	return publishRecord(directory, recordsFilename, append(data, '\n'), verify)
}

func publishRecord(directory, name string, data []byte, verify bool) error {
	path := filepath.Join(directory, name)
	if verify {
		have, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(have, data) {
			return fmt.Errorf("%s differs from the pinned release; run make tool-records-refresh", name)
		}
		return nil
	}
	sum := sha256.Sum256(data)
	return verifiedfetch.Write(path, verifiedfetch.Pin{Name: name, SHA256: hex.EncodeToString(sum[:]), Bytes: int64(len(data))},
		0o644, func(w io.Writer) error {
			_, err := w.Write(data)
			return err
		})
}
