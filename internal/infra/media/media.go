package media

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	contextstore "github.com/crmarques/bootwright/internal/runtime/context"
	managedroot "github.com/crmarques/bootwright/internal/runtime/root/managedroot"
)

const (
	Scheme  = "local-media"
	DirName = "media"

	kindManaged = "media"

	fileMode = 0o644
	dirMode  = 0o700
)

var keyRE = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

type Resolved struct {
	Original string
	Kind     string
	Key      string
	Path     string
	URL      string
}

type Entry struct {
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	Size      int64     `json:"size"`
	SHA256    string    `json:"sha256"`
	Modified  time.Time `json:"modified"`
	Reference string    `json:"reference"`
}

type AddOptions struct {
	Key      string
	Source   string
	SHA256   string
	Force    bool
	Download bool
}

func StoreDir() string {
	return filepath.Join(contextstore.RootDir(), DirName)
}

func Reference(key string) string {
	return Scheme + ":" + key
}

func ValidateKey(key string) error {
	if strings.TrimSpace(key) == "" {
		return errors.New("media key is required")
	}
	if key != filepath.Base(key) || strings.ContainsAny(key, `/\`) {
		return fmt.Errorf("media key %q must be a filename, not a path", key)
	}
	if strings.Contains(key, "..") {
		return fmt.Errorf("media key %q must not contain path traversal", key)
	}
	if !keyRE.MatchString(key) {
		return fmt.Errorf("media key %q must contain only letters, numbers, dot, underscore, and dash", key)
	}
	if !strings.EqualFold(filepath.Ext(key), ".iso") {
		return fmt.Errorf("media key %q must end with .iso", key)
	}
	return nil
}

func ParseReference(value string) (string, bool, error) {
	if !strings.HasPrefix(value, Scheme+":") {
		return "", false, nil
	}
	key := strings.TrimPrefix(value, Scheme+":")
	if err := ValidateKey(key); err != nil {
		return "", true, err
	}
	return key, true, nil
}

func ValidateISOReference(value string) error {
	_, err := Resolve(value)
	return err
}

func ResolveExisting(value string) (Resolved, error) {
	resolved, err := Resolve(value)
	if err != nil {
		return Resolved{}, err
	}
	switch resolved.Kind {
	case kindManaged, "file":
		info, err := os.Lstat(resolved.Path)
		if errors.Is(err, os.ErrNotExist) {
			return Resolved{}, fmt.Errorf("ISO media %s not found", resolved.Path)
		}
		if err != nil {
			return Resolved{}, fmt.Errorf("stat ISO media %s: %w", resolved.Path, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return Resolved{}, fmt.Errorf("ISO media %s must be a regular file", resolved.Path)
		}
	}
	return resolved, nil
}

func Resolve(value string) (Resolved, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return Resolved{}, errors.New("ISO reference is required")
	}
	if key, ok, err := ParseReference(value); ok || err != nil {
		if err != nil {
			return Resolved{}, err
		}
		path, err := Path(key)
		if err != nil {
			return Resolved{}, err
		}
		return Resolved{Original: value, Kind: kindManaged, Key: key, Path: path}, nil
	}
	u, err := url.Parse(value)
	if err != nil {
		return Resolved{}, fmt.Errorf("parse ISO reference %q: %w", value, err)
	}
	switch strings.ToLower(u.Scheme) {
	case "file":
		if u.User != nil {
			return Resolved{}, errors.New("file ISO references must not embed credentials")
		}
		if u.Host != "" && u.Host != "localhost" {
			return Resolved{}, fmt.Errorf("file ISO reference host %q is not supported", u.Host)
		}
		path := u.Path
		if path == "" || !filepath.IsAbs(path) {
			return Resolved{}, errors.New("file ISO references must use an absolute path")
		}
		if !strings.EqualFold(filepath.Ext(path), ".iso") {
			return Resolved{}, fmt.Errorf("file ISO path %q must end with .iso", path)
		}
		return Resolved{Original: value, Kind: "file", Path: path}, nil
	case "http", "https":
		if u.User != nil {
			return Resolved{}, errors.New("HTTP ISO references must not embed credentials")
		}
		if u.Host == "" {
			return Resolved{}, errors.New("HTTP ISO references must include a host")
		}
		return Resolved{Original: value, Kind: "url", URL: value}, nil
	default:
		return Resolved{}, fmt.Errorf("ISO reference %q must use local-media:, file://, http://, or https://", value)
	}
}

func Path(key string) (string, error) {
	if err := ValidateKey(key); err != nil {
		return "", err
	}
	return filepath.Join(StoreDir(), key), nil
}

func EnsureStoreDir() (string, error) {
	root := contextstore.RootDir()
	if _, err := managedroot.Ensure(root, dirMode); err != nil {
		return "", err
	}
	dir := StoreDir()
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return "", fmt.Errorf("create media store %s: %w", dir, err)
	}
	if err := os.Chmod(dir, dirMode); err != nil {
		return "", fmt.Errorf("chmod media store %s: %w", dir, err)
	}
	return dir, nil
}

func AddFile(key, source, expectedSHA string, force bool) (Entry, error) {
	return add(AddOptions{Key: key, Source: source, SHA256: expectedSHA, Force: force})
}

func AddURL(key, sourceURL, expectedSHA string, force bool) (Entry, error) {
	return add(AddOptions{Key: key, Source: sourceURL, SHA256: expectedSHA, Force: force, Download: true})
}

func add(opts AddOptions) (Entry, error) {
	if err := ValidateKey(opts.Key); err != nil {
		return Entry{}, err
	}
	expectedSHA, err := NormalizeSHA256(opts.SHA256)
	if err != nil {
		return Entry{}, err
	}
	dir, err := EnsureStoreDir()
	if err != nil {
		return Entry{}, err
	}
	target := filepath.Join(dir, opts.Key)
	if err := ensureWritableTarget(target, opts.Force); err != nil {
		return Entry{}, err
	}
	tmp, err := os.CreateTemp(dir, "."+opts.Key+".tmp-*")
	if err != nil {
		return Entry{}, fmt.Errorf("create temporary media file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	sum := sha256.New()
	w := io.MultiWriter(tmp, sum)
	if opts.Download {
		err = copyURL(w, opts.Source)
	} else {
		err = copyFile(w, opts.Source)
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return Entry{}, err
	}
	actualSHA := hex.EncodeToString(sum.Sum(nil))
	if expectedSHA != "" && actualSHA != expectedSHA {
		return Entry{}, fmt.Errorf("sha256 mismatch for %s: got %s, want %s", opts.Key, actualSHA, expectedSHA)
	}
	if err := os.Chmod(tmpPath, fileMode); err != nil {
		return Entry{}, fmt.Errorf("chmod temporary media file: %w", err)
	}
	if err := os.Rename(tmpPath, target); err != nil {
		return Entry{}, fmt.Errorf("install media file %s: %w", target, err)
	}
	return entryForPath(opts.Key, target, actualSHA)
}

func ensureWritableTarget(target string, force bool) error {
	info, err := os.Lstat(target)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat media target %s: %w", target, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("media target %s must not be a symlink", target)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("media target %s must be a regular file", target)
	}
	if !force {
		return fmt.Errorf("media %q already exists; use --force to replace it, then confirm or add --yes", filepath.Base(target))
	}
	return nil
}

func copyFile(w io.Writer, source string) error {
	if strings.TrimSpace(source) == "" {
		return errors.New("--from-file is required")
	}
	f, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open source ISO %s: %w", source, err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat source ISO %s: %w", source, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("source ISO %s must be a regular file", source)
	}
	if _, err := io.Copy(w, f); err != nil {
		return fmt.Errorf("copy source ISO %s: %w", source, err)
	}
	return nil
}

func copyURL(w io.Writer, sourceURL string) error {
	u, err := url.Parse(strings.TrimSpace(sourceURL))
	if err != nil {
		return fmt.Errorf("parse --from-url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return errors.New("--from-url must use http:// or https://")
	}
	if u.User != nil {
		return errors.New("--from-url must not embed credentials")
	}
	resp, err := http.Get(u.String())
	if err != nil {
		return fmt.Errorf("download ISO %s: %w", u.String(), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("download ISO %s: HTTP %d", u.String(), resp.StatusCode)
	}
	if _, err := io.Copy(w, resp.Body); err != nil {
		return fmt.Errorf("download ISO %s: %w", u.String(), err)
	}
	return nil
}

func List() ([]Entry, error) {
	dir := StoreDir()
	items, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read media store %s: %w", dir, err)
	}
	var out []Entry
	for _, item := range items {
		if item.Type()&os.ModeSymlink != 0 || item.IsDir() {
			continue
		}
		name := item.Name()
		if err := ValidateKey(name); err != nil {
			continue
		}
		path := filepath.Join(dir, name)
		entry, err := entryForPath(name, path, "")
		if err != nil {
			return nil, err
		}
		out = append(out, entry)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func Remove(key string) (Entry, error) {
	path, err := Path(key)
	if err != nil {
		return Entry{}, err
	}
	entry, err := entryForPath(key, path, "")
	if err != nil {
		return Entry{}, err
	}
	if err := os.Remove(path); err != nil {
		return Entry{}, fmt.Errorf("remove media %s: %w", path, err)
	}
	return entry, nil
}

func entryForPath(key, path, knownSHA string) (Entry, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return Entry{}, fmt.Errorf("stat media %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return Entry{}, fmt.Errorf("media %s must be a regular file", path)
	}
	sum := knownSHA
	if sum == "" {
		f, err := os.Open(path)
		if err != nil {
			return Entry{}, fmt.Errorf("open media %s: %w", path, err)
		}
		h := sha256.New()
		if _, err := io.Copy(h, f); err != nil {
			_ = f.Close()
			return Entry{}, fmt.Errorf("hash media %s: %w", path, err)
		}
		if err := f.Close(); err != nil {
			return Entry{}, fmt.Errorf("close media %s: %w", path, err)
		}
		sum = hex.EncodeToString(h.Sum(nil))
	}
	return Entry{
		Name:      key,
		Path:      path,
		Size:      info.Size(),
		SHA256:    sum,
		Modified:  info.ModTime().UTC(),
		Reference: Reference(key),
	}, nil
}

func NormalizeSHA256(value string) (string, error) {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "sha256:")
	if value == "" {
		return "", nil
	}
	if len(value) != sha256.Size*2 {
		return "", errors.New("sha256 must be 64 hexadecimal characters")
	}
	if _, err := hex.DecodeString(value); err != nil {
		return "", errors.New("sha256 must be hexadecimal")
	}
	return strings.ToLower(value), nil
}
