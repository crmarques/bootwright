// Package verifiedfetch owns how the build tools take a pinned file: it is
// read within its pinned size, held to its SHA-256, and given its name only
// once it matches.
package verifiedfetch

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
)

type Pin struct {
	Name   string
	Bytes  int64
	SHA256 string
}

func (p Pin) check(reader io.Reader) error {
	hash := sha256.New()
	written, err := io.Copy(hash, io.LimitReader(reader, p.Bytes+1))
	if err != nil || written != p.Bytes || hex.EncodeToString(hash.Sum(nil)) != p.SHA256 {
		return fmt.Errorf("%s does not match its pinned size and SHA-256", p.Name)
	}
	return nil
}

func Open(file string, pin Pin) (*os.File, error) {
	info, err := os.Lstat(file)
	if err != nil || !info.Mode().IsRegular() || info.Size() != pin.Bytes {
		return nil, errors.New("absent")
	}
	handle, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	if err := pin.check(handle); err != nil {
		handle.Close()
		return nil, err
	}
	if _, err := handle.Seek(0, io.SeekStart); err != nil {
		handle.Close()
		return nil, err
	}
	return handle, nil
}

func Verified(file string, pin Pin) error {
	handle, err := Open(file, pin)
	if err != nil {
		return err
	}
	return handle.Close()
}

func Write(file string, pin Pin, mode os.FileMode, fill func(io.Writer) error) error {
	partial, err := os.CreateTemp(filepath.Dir(file), ".part-*")
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
	if err := pin.check(partial); err != nil {
		return err
	}
	if err := partial.Chmod(mode); err != nil {
		return err
	}
	if err := partial.Close(); err != nil {
		return err
	}
	return os.Rename(partial.Name(), file)
}

func Download(client *http.Client, url string, pin Pin) func(io.Writer) error {
	return func(sink io.Writer) error {
		response, err := client.Get(url)
		if err != nil {
			return err
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return fmt.Errorf("%s answered %s", url, response.Status)
		}
		_, err = io.Copy(sink, io.LimitReader(response.Body, pin.Bytes+1))
		return err
	}
}

func CopyFile(file string) func(io.Writer) error {
	return func(sink io.Writer) error {
		source, err := os.Open(file)
		if err != nil {
			return err
		}
		defer source.Close()
		_, err = io.Copy(sink, source)
		return err
	}
}

func Cached(client *http.Client, url, directory string, pin Pin) (*os.File, error) {
	cached := filepath.Join(directory, path.Base(url))
	if handle, err := Open(cached, pin); err == nil {
		return handle, nil
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return nil, err
	}
	if err := Write(cached, pin, 0o600, Download(client, url, pin)); err != nil {
		return nil, fmt.Errorf("%s: %w", url, err)
	}
	return Open(cached, pin)
}
