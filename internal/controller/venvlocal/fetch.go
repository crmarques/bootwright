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

// Fetch fills the wheel cache with every file of the ABI and the installer.
// A file already cached with its locked size and SHA-256 makes no request.
func (a *Adapter) Fetch(ctx context.Context, abi string, egress prerequisites.SetupEgress) error {
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
	client, err := a.client(ctx, egress)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var group sync.WaitGroup
	var once sync.Once
	var first error
	slots := make(chan struct{}, fetchers)
	for _, file := range missing {
		group.Go(func() {
			select {
			case slots <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-slots }()
			if err := a.download(ctx, client, file); err != nil {
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

// cached proves a cached wheel is a regular file of its locked size and
// SHA-256.
func (a *Adapter) cached(file runtimelock.File) error {
	path := a.cachePath(file)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() != file.Size {
		return failure("the wheel cache does not hold " + file.Filename)
	}
	handle, err := os.Open(path)
	if err != nil {
		return failure("the wheel cache does not hold " + file.Filename)
	}
	defer handle.Close()
	return verifyWheel(file, handle)
}

func verifyWheel(file runtimelock.File, reader io.Reader) error {
	hash := sha256.New()
	written, err := io.Copy(hash, io.LimitReader(reader, file.Size+1))
	if err != nil || written != file.Size || hex.EncodeToString(hash.Sum(nil)) != file.SHA256 {
		return failure(file.Filename + " does not match its locked size and SHA-256")
	}
	return nil
}

// download writes a wheel beside its cache name and renames it into place
// only once its size and SHA-256 match the lock.
func (a *Adapter) download(ctx context.Context, client *http.Client, file runtimelock.File) error {
	address, err := url.Parse(file.URL)
	if err != nil || !approvedURL(address) {
		return failure("the wheel URL of " + file.Filename + " is not a credential-free HTTPS URL")
	}
	directory := filepath.Join(a.wheels, file.SHA256)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return failure("the wheel cache cannot be written")
	}
	partial, err := os.CreateTemp(directory, ".part-*")
	if err != nil {
		return failure("the wheel cache cannot be written")
	}
	defer func() {
		partial.Close()
		os.Remove(partial.Name())
	}()
	if err := receive(ctx, client, file, partial); err != nil {
		return err
	}
	if _, err := partial.Seek(0, io.SeekStart); err != nil {
		return failure("the wheel cache cannot be read back")
	}
	if err := verifyWheel(file, partial); err != nil {
		return err
	}
	if partial.Sync() != nil || partial.Close() != nil || os.Rename(partial.Name(), a.cachePath(file)) != nil {
		return failure("the wheel cache cannot be written")
	}
	return nil
}

func receive(ctx context.Context, client *http.Client, file runtimelock.File, destination io.Writer) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, file.URL, nil)
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
