package venvlocal

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/controller/runtimelock"
)

const fetchTimeout = 5 * time.Minute

func (a *Adapter) Wheels(ctx context.Context, abi string) ([]prerequisites.RuntimeWheel, error) {
	files, err := a.lockedFiles(abi)
	if err != nil {
		return nil, err
	}
	var wheels []prerequisites.RuntimeWheel
	for _, file := range append(files, a.lock.Installer()) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		wheels = append(wheels, prerequisites.RuntimeWheel{Filename: file.Filename, Size: file.Size, Cached: a.cached(file) == nil})
	}
	return wheels, nil
}

// Fetch fills the wheel cache with the ABI's files and the installer; a verified cached file makes no request.
func (a *Adapter) Fetch(ctx context.Context, abi string, egress prerequisites.SetupEgress, mirror prerequisites.WheelMirror) error {
	files, err := a.lockedFiles(abi)
	if err != nil {
		return err
	}
	var missing []runtimelock.File
	for _, file := range append(files, a.lock.Installer()) {
		if a.cached(file) != nil {
			missing = append(missing, file)
		}
	}
	if len(missing) == 0 {
		return ctx.Err()
	}
	source, err := a.source(ctx, egress, mirror)
	if err != nil {
		return err
	}
	return a.fetchAll(ctx, missing, func(ctx context.Context, file runtimelock.File) error {
		return a.download(ctx, source, file)
	})
}

func (a *Adapter) fetchAll(ctx context.Context, files []runtimelock.File, task func(context.Context, runtimelock.File) error) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var group sync.WaitGroup
	var once sync.Once
	var first error
	slots := make(chan struct{}, fetchers)
	for _, file := range files {
		group.Go(func() {
			select {
			case slots <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-slots }()
			if err := task(ctx, file); err != nil {
				once.Do(func() { first = err; cancel() })
			}
		})
	}
	group.Wait()
	if first != nil {
		return first
	}
	return ctx.Err()
}

func (a *Adapter) cachePath(file runtimelock.File) string {
	return filepath.Join(a.wheels, file.SHA256, file.Filename)
}

// cached proves a cached wheel is a regular file of its locked size and SHA-256.
func (a *Adapter) cached(file runtimelock.File) error {
	if verifiedFile(a.cachePath(file), file) != nil {
		return failure("the wheel cache does not hold " + file.Filename)
	}
	return nil
}

func verifiedFile(path string, file runtimelock.File) error {
	handle, err := openRegular(path, file.Size)
	if err != nil {
		return failure(file.Filename + " is not a regular file of its locked size")
	}
	defer handle.Close()
	return verifyWheel(file, handle)
}

func openRegular(path string, size int64) (*os.File, error) {
	handle, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	info, err := handle.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != size {
		handle.Close()
		return nil, errors.New("not a regular file of its locked size")
	}
	return handle, nil
}

func verifyWheel(file runtimelock.File, reader io.Reader) error {
	hash := sha256.New()
	written, err := io.Copy(hash, io.LimitReader(reader, file.Size+1))
	if err != nil || written != file.Size || hex.EncodeToString(hash.Sum(nil)) != file.SHA256 {
		return failure(file.Filename + " does not match its locked size and SHA-256")
	}
	return nil
}

type wheelSource func(ctx context.Context, file runtimelock.File, destination io.Writer) error

func (a *Adapter) source(ctx context.Context, egress prerequisites.SetupEgress, mirror prerequisites.WheelMirror) (wheelSource, error) {
	if directory, local := mirror.Directory(); local {
		return mirrored(directorySource(directory), mirror), nil
	}
	client, err := a.client(ctx, egress)
	if err != nil {
		return nil, err
	}
	fetch := func(ctx context.Context, file runtimelock.File, destination io.Writer) error {
		address := file.URL
		if mirror.Set() {
			address = mirror.Location(file.Filename)
		}
		return receive(ctx, client, address, file, destination)
	}
	if mirror.Set() {
		return mirrored(fetch, mirror), nil
	}
	return fetch, nil
}

// mirrored blames the mirror for any wheel it lacks or holds with other bytes.
func mirrored(inner wheelSource, mirror prerequisites.WheelMirror) wheelSource {
	return func(ctx context.Context, file runtimelock.File, destination io.Writer) error {
		hash := sha256.New()
		err := inner(ctx, file, io.MultiWriter(destination, hash))
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var scoped *prerequisites.ScopedFailure
		switch {
		case errors.As(err, &scoped) && scoped.Routable:
			return err
		case err != nil:
			return mirrorFailure(file.Filename+" is not a readable file of its locked size in the mirror "+mirror.String(), mirror)
		case hex.EncodeToString(hash.Sum(nil)) != file.SHA256:
			return mirrorFailure(file.Filename+" in the mirror "+mirror.String()+" does not match its locked size and SHA-256", mirror)
		}
		return nil
	}
}

func mirrorFailure(message string, mirror prerequisites.WheelMirror) error {
	return &prerequisites.ScopedFailure{
		Message:    message,
		Correction: "Place the locked wheels in " + mirror.String() + " (setup --download-only --dest-dir writes them), or drop --wheel-mirror",
		Command:    "repeat this command",
	}
}

func directorySource(directory string) wheelSource {
	return func(ctx context.Context, file runtimelock.File, destination io.Writer) error {
		unreadable := errors.New("unreadable")
		if file.Filename != filepath.Base(file.Filename) || ctx.Err() != nil {
			return errors.Join(ctx.Err(), unreadable)
		}
		handle, err := openRegular(filepath.Join(directory, file.Filename), file.Size)
		if err != nil {
			return unreadable
		}
		defer handle.Close()
		if written, err := io.Copy(destination, io.LimitReader(handle, file.Size+1)); err != nil || written != file.Size {
			return unreadable
		}
		return nil
	}
}

// download writes a wheel beside its cache name and renames it into place
// only once its size and SHA-256 match the lock.
func (a *Adapter) download(ctx context.Context, source wheelSource, file runtimelock.File) error {
	directory := filepath.Join(a.wheels, file.SHA256)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return failure("the wheel cache cannot be written")
	}
	return place(ctx, source, file, directory, a.cachePath(file), 0o600)
}

func place(ctx context.Context, source wheelSource, file runtimelock.File, directory, final string, mode os.FileMode) error {
	partial, err := os.CreateTemp(directory, ".part-*")
	if err != nil {
		return failure("the wheel cache cannot be written")
	}
	defer func() {
		partial.Close()
		os.Remove(partial.Name())
	}()
	if err := source(ctx, file, partial); err != nil {
		return err
	}
	if _, err := partial.Seek(0, io.SeekStart); err != nil {
		return failure("the wheel cache cannot be read back")
	}
	if err := verifyWheel(file, partial); err != nil {
		return err
	}
	if partial.Chmod(mode) != nil || partial.Sync() != nil || partial.Close() != nil || os.Rename(partial.Name(), final) != nil {
		return failure("the wheel cache cannot be written")
	}
	return nil
}

func receive(ctx context.Context, client *http.Client, address string, file runtimelock.File, destination io.Writer) error {
	approved, err := url.Parse(address)
	if err != nil || !approvedURL(approved) {
		return failure("the wheel URL of " + file.Filename + " is not a credential-free HTTPS URL")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return failure("the wheel request for " + file.Filename + " is invalid")
	}
	request.Header.Set("Accept-Encoding", "identity")
	request.Header.Set("User-Agent", "Bootwright-Controller/1")
	response, err := client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return &prerequisites.ScopedFailure{
			Message:    file.Filename + " could not be fetched from " + request.URL.Hostname(),
			Correction: "Restore this host's access to " + request.URL.Hostname(), Routable: true,
		}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.ContentLength > file.Size || response.Header.Get("Content-Encoding") != "" {
		return failure(file.Filename + " was answered with an unexpected status or representation")
	}
	written, err := io.Copy(destination, io.LimitReader(response.Body, file.Size+1))
	if err != nil || written != file.Size {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return failure(file.Filename + " does not match its locked size and SHA-256")
	}
	return nil
}

func approvedURL(address *url.URL) bool {
	return address != nil && address.Scheme == "https" && address.User == nil && address.Host != "" &&
		address.Opaque == "" && address.Fragment == "" && (address.Port() == "" || address.Port() == "443")
}

// wheelClient is the one client every fetch of a run shares: system trust,
// the explicit route, HTTPS only and a bounded redirect chain.
func wheelClient(ctx context.Context, egress prerequisites.SetupEgress) (*http.Client, error) {
	selector, err := controller.NewProxySelector(egress.HTTPProxy, egress.HTTPSProxy, egress.NoProxy)
	if err != nil {
		return nil, failure("the explicit HTTPS proxy route is invalid")
	}
	roots, err := systemRoots(ctx)
	if err != nil {
		return nil, err
	}
	transport := &http.Transport{
		Proxy:                  func(request *http.Request) (*url.URL, error) { return selector(request.URL), nil },
		TLSClientConfig:        &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots},
		DialContext:            (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:    15 * time.Second,
		ResponseHeaderTimeout:  30 * time.Second,
		MaxResponseHeaderBytes: 64 << 10, DisableCompression: true, MaxIdleConnsPerHost: fetchers,
	}
	return &http.Client{
		Transport: transport, Timeout: fetchTimeout,
		CheckRedirect: func(request *http.Request, previous []*http.Request) error {
			if len(previous) >= maxRedirect || !approvedURL(request.URL) {
				return errors.New("the wheel request redirected outside HTTPS or too many times")
			}
			return nil
		},
	}, nil
}
