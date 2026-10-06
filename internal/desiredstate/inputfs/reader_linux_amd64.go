package inputfs

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

const pathHandle = 0x200000

type discoveredFile struct {
	path   string
	before syscall.Stat_t
}

type discovery struct {
	ctx         context.Context
	session     FileSession
	root        *os.File
	rootReads   bool
	identities  map[string]syscall.Stat_t
	directories map[string]syscall.Stat_t
	files       map[string]discoveredFile
	markers     map[string]discoveredFile
	entries     int
	filePaths   int
	markerPaths int
}

func (r Reader) Read(ctx context.Context, paths []string) (desiredstate.Sources, error) {
	return r.read(ctx, paths, false)
}

// ReadDirectory acquires exactly one directory through verified opened handles.
// It rejects other root types before reading candidate bytes.
func (r Reader) ReadDirectory(ctx context.Context, path string) (desiredstate.Sources, error) {
	return r.read(ctx, []string{path}, true)
}

// ReadFile acquires a single bounded regular input through the same verified
// handles as graph discovery, without scanning directories or companion files.
func (r Reader) ReadFile(ctx context.Context, path string, maximum int) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if path == "" || maximum < 1 || maximum > desiredstate.MaxFileBytes {
		return nil, failure("input.read", "single-file input or byte limit is invalid", path)
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, failure("input.read", "input file path cannot be resolved", path)
	}
	session, err := r.begin(ctx, path)
	if err != nil {
		return nil, err
	}
	defer session.Close()
	scan, err := discoverThrough(ctx, session)
	if err != nil {
		return nil, err
	}
	defer scan.root.Close()
	file, stat, err := scan.openPath(absolute, pathHandle)
	if err != nil {
		return nil, err
	}
	file.Close()
	if stat.Mode&syscall.S_IFMT != syscall.S_IFREG {
		return nil, failure("input.read", "input source must be a regular file without symbolic links", path)
	}
	if err := scan.candidate(absolute, stat, false); err != nil {
		return nil, err
	}
	files, err := scan.readFiles(scan.files, maximum, maximum, "input file bytes", "input file bytes")
	if err != nil {
		return nil, err
	}
	if err := scan.verifyDirectories(); err != nil {
		return nil, err
	}
	if len(files) != 1 {
		return nil, failure("input.read", "single-file input was not acquired", path)
	}
	return files[0].Bytes(), nil
}

func (r Reader) read(ctx context.Context, paths []string, directoryOnly bool) (desiredstate.Sources, error) {
	if err := ctx.Err(); err != nil {
		return desiredstate.Sources{}, err
	}
	roots := make([]string, 0, len(paths))
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return desiredstate.Sources{}, err
		}
		if path == "" {
			return desiredstate.Sources{}, failure("input.read", "input path is empty", "")
		}
		absolute, err := filepath.Abs(path)
		if err != nil {
			return desiredstate.Sources{}, failure("input.read", "input path cannot be resolved", path)
		}
		roots = append(roots, absolute)
	}
	slices.Sort(roots)
	roots = slices.Compact(roots)
	if len(roots) == 0 {
		return desiredstate.Sources{Roots: roots, Files: []desiredstate.SourceFile{}, Markers: []desiredstate.SourceFile{}}, nil
	}
	named := ""
	if len(roots) == 1 {
		named = roots[0]
	}
	session, err := r.begin(ctx, named)
	if err != nil {
		return desiredstate.Sources{}, err
	}
	defer session.Close()
	scan, err := discoverThrough(ctx, session)
	if err != nil {
		return desiredstate.Sources{}, err
	}
	defer scan.root.Close()
	for _, path := range roots {
		if err := scan.source(path, directoryOnly); err != nil {
			return desiredstate.Sources{}, err
		}
	}
	files, err := scan.readFiles(scan.files, desiredstate.MaxFileBytes, desiredstate.MaxAllFileBytes, "YAML file bytes", "aggregate YAML bytes")
	if err != nil {
		return desiredstate.Sources{}, err
	}
	markers, err := scan.readFiles(scan.markers, desiredstate.MaxMarkerBytes, desiredstate.MaxAllMarkerBytes, "native add-on marker bytes", "aggregate native add-on marker bytes")
	if err != nil {
		return desiredstate.Sources{}, err
	}
	if err := scan.verifyDirectories(); err != nil {
		return desiredstate.Sources{}, err
	}
	return desiredstate.Sources{Roots: roots, Files: files, Markers: markers}, nil
}

// begin starts the one session every open of an acquisition goes through;
// the caller closes it only after the final re-walk.
func (r Reader) begin(ctx context.Context, path string) (FileSession, error) {
	if r.Files == nil {
		return processFiles{}, nil
	}
	session, err := r.Files.Begin(ctx)
	if err != nil || session == nil {
		if canceled := ctx.Err(); canceled != nil {
			return nil, canceled
		}
		return nil, failure("input.read", "the input cannot be opened under the invoking account", path)
	}
	return session, nil
}

// newDiscovery opens with this process's own credentials, as a Reader bound to
// no opener does.
func newDiscovery(ctx context.Context) (*discovery, error) {
	return discoverThrough(ctx, processFiles{})
}

// discoverThrough takes "/" from the session and records whether this process
// reads as root: the session opens each descriptor, but every read through one
// runs with this process's credentials.
func discoverThrough(ctx context.Context, session FileSession) (*discovery, error) {
	root, err := session.Root()
	if err != nil {
		if canceled := ctx.Err(); canceled != nil {
			return nil, canceled
		}
		if denied(err) {
			return nil, openDenied(err, "/")
		}
		return nil, failure("input.read", "input root cannot be opened safely", "")
	}
	return &discovery{
		ctx: ctx, session: session, root: root, rootReads: syscall.Geteuid() == 0,
		identities: make(map[string]syscall.Stat_t), directories: make(map[string]syscall.Stat_t),
		files: make(map[string]discoveredFile), markers: make(map[string]discoveredFile),
	}, nil
}

func (s *discovery) source(path string, directoryOnly bool) error {
	file, stat, err := s.openPath(path, pathHandle)
	if err != nil {
		return err
	}
	defer file.Close()
	if directoryOnly && stat.Mode&syscall.S_IFMT != syscall.S_IFDIR {
		code := "input.not-directory"
		if stat.Mode&syscall.S_IFMT == syscall.S_IFLNK {
			code = "input.symlink"
		}
		return failure(code, "input source must be a directory without symbolic links", path)
	}
	switch stat.Mode & syscall.S_IFMT {
	case syscall.S_IFDIR:
		if err := s.rememberIdentity(path, stat); err != nil {
			return err
		}
		return s.directory(path, 0)
	case syscall.S_IFREG:
		if !yamlPath(path) {
			return failure("input.read", "input file must end in lowercase .yaml or .yml", path)
		}
		if err := s.candidate(path, stat, false); err != nil {
			return err
		}
		if filepath.Base(path) == "add-on.yaml" {
			markerPath := filepath.Join(filepath.Dir(path), ".bootwright-addon")
			if markerPosition(markerPath) {
				return s.optionalMarker(markerPath)
			}
		}
		return nil
	case syscall.S_IFLNK:
		return diagnostics.NewFailureWithRemediation("input.symlink", "input source is a symbolic link", path, realPathRemedy)
	default:
		return failure("input.read", "input source is not a regular file or directory", path)
	}
}

func (s *discovery) directory(path string, depth int) error {
	if _, visited := s.directories[path]; visited {
		return nil
	}
	file, before, err := s.openPath(path, syscall.O_RDONLY|syscall.O_DIRECTORY)
	if err != nil {
		return err
	}
	defer file.Close()
	entries, err := file.ReadDir(desiredstate.MaxEntries - s.entries + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return failure("input.read", "input directory cannot be enumerated", path)
	}
	s.entries += len(entries)
	if s.entries > desiredstate.MaxEntries {
		return limit("filesystem entries enumerated", desiredstate.MaxEntries, path)
	}
	slices.SortFunc(entries, func(a, b os.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
	for _, entry := range entries {
		if err := s.ctx.Err(); err != nil {
			return err
		}
		child := filepath.Join(path, entry.Name())
		if depth+1 > desiredstate.MaxPathDepth {
			return limit("descendant path depth", desiredstate.MaxPathDepth, child)
		}
		marker := markerPosition(child)
		if entry.IsDir() && desiredstate.SkippedDirectory(entry.Name()) && !marker {
			continue
		}
		if !entry.IsDir() && !yamlPath(child) && !marker {
			continue
		}
		handle, stat, err := s.openChild(file, entry.Name(), pathHandle, child)
		if err != nil {
			return err
		}
		handle.Close()
		if marker {
			if err := s.candidate(child, stat, true); err != nil {
				return err
			}
		} else if stat.Mode&syscall.S_IFMT == syscall.S_IFDIR {
			if desiredstate.SkippedDirectory(entry.Name()) {
				continue
			}
			if err := s.rememberIdentity(child, stat); err != nil {
				return err
			}
			if err := s.directory(child, depth+1); err != nil {
				return err
			}
		} else if yamlPath(child) {
			if err := s.candidate(child, stat, false); err != nil {
				return err
			}
		}
	}
	after, err := statFile(file)
	if err != nil || !sameStable(before, after) {
		return failure("input.read", "input directory changed during discovery", path)
	}
	s.directories[path] = before
	return nil
}

func (s *discovery) candidate(path string, stat syscall.Stat_t, marker bool) error {
	collection := s.files
	if marker {
		collection = s.markers
	}
	if prior, exists := collection[path]; exists {
		if !sameStable(prior.before, stat) {
			return failure("input.read", "input changed during discovery", path)
		}
		return nil
	}
	if marker {
		s.markerPaths++
		if s.markerPaths > desiredstate.MaxMarkers {
			return limit("native add-on marker candidate paths", desiredstate.MaxMarkers, path)
		}
	} else {
		s.filePaths++
		if s.filePaths > desiredstate.MaxFiles {
			return limit("YAML-suffix candidate paths", desiredstate.MaxFiles, path)
		}
	}
	if stat.Mode&syscall.S_IFMT == syscall.S_IFLNK {
		return diagnostics.NewFailureWithRemediation("input.symlink", "input candidate is a symbolic link", path, "replace the symbolic link with a copy of the file it names")
	}
	if stat.Mode&syscall.S_IFMT != syscall.S_IFREG {
		return failure("input.read", "input candidate must be a regular file", path)
	}
	if stat.Nlink != 1 {
		return diagnostics.NewFailureWithRemediation("input.symlink", "input candidate has more than one hard link", path, "replace the hard link with a copy of the file")
	}
	collection[path] = discoveredFile{path: path, before: stat}
	return nil
}

func (s *discovery) optionalMarker(path string) error {
	file, stat, err := s.openPath(path, pathHandle)
	if err != nil {
		var problem *diagnostics.Failure
		if errors.As(err, &problem) && len(problem.Diagnostics) == 1 && problem.Diagnostics[0].Code == "input.not-found" {
			return nil
		}
		return err
	}
	defer file.Close()
	return s.candidate(path, stat, true)
}

func (s *discovery) readFiles(candidates map[string]discoveredFile, maximum, aggregate int, singleName, aggregateName string) ([]desiredstate.SourceFile, error) {
	paths := make([]string, 0, len(candidates))
	for path := range candidates {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	result := make([]desiredstate.SourceFile, 0, len(paths))
	total := 0
	for _, path := range paths {
		data, err := s.readFile(candidates[path], maximum, aggregate-total, singleName, aggregateName, aggregate)
		if err != nil {
			return nil, err
		}
		total += len(data)
		result = append(result, desiredstate.NewSourceFile(path, data))
	}
	return result, nil
}

func (s *discovery) readFile(candidate discoveredFile, maximum, remaining int, singleName, aggregateName string, aggregate int) ([]byte, error) {
	file, before, err := s.openPath(candidate.path, syscall.O_RDONLY|syscall.O_NONBLOCK)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if before.Mode&syscall.S_IFMT != syscall.S_IFREG || before.Nlink != 1 || !sameStable(candidate.before, before) {
		return nil, failure("input.read", "input changed before reading", candidate.path)
	}
	if before.Size > int64(maximum) {
		return nil, limit(singleName, maximum, candidate.path)
	}
	data, err := readBounded(s.ctx, file, min(maximum, remaining)+1)
	if err != nil {
		if s.ctx.Err() != nil {
			return nil, s.ctx.Err()
		}
		if denied(err) {
			return nil, readDenied(s.rootReads, candidate.path)
		}
		return nil, failure("input.read", "input bytes cannot be read safely", candidate.path)
	}
	if len(data) > maximum {
		return nil, limit(singleName, maximum, candidate.path)
	}
	if len(data) > remaining {
		return nil, limit(aggregateName, aggregate, candidate.path)
	}
	after, err := statFile(file)
	if err != nil || !sameStable(before, after) || int64(len(data)) != after.Size {
		return nil, failure("input.read", "input changed while reading", candidate.path)
	}
	current, identity, err := s.openPath(candidate.path, pathHandle)
	if err != nil {
		return nil, err
	}
	defer current.Close()
	if !sameStable(after, identity) {
		return nil, failure("input.read", "input was replaced while reading", candidate.path)
	}
	return data, nil
}

func readBounded(ctx context.Context, file *os.File, maximum int) ([]byte, error) {
	data := make([]byte, 0, min(maximum, 32768))
	buffer := make([]byte, min(maximum, 32768))
	for len(data) < maximum {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		n, err := file.Read(buffer[:min(len(buffer), maximum-len(data))])
		data = append(data, buffer[:n]...)
		if errors.Is(err, io.EOF) {
			return data, nil
		}
		if err != nil {
			return nil, err
		}
	}
	return data, nil
}

func (s *discovery) openPath(path string, flags int) (*os.File, syscall.Stat_t, error) {
	if err := s.ctx.Err(); err != nil {
		return nil, syscall.Stat_t{}, err
	}
	parent := s.root
	owned := false
	defer func() {
		if owned {
			parent.Close()
		}
	}()
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if path == "/" {
		return s.openChild(s.root, ".", flags, path)
	}
	current := ""
	for index, name := range parts {
		if err := s.ctx.Err(); err != nil {
			return nil, syscall.Stat_t{}, err
		}
		current += "/" + name
		openFlags := flags
		if index != len(parts)-1 {
			openFlags = pathHandle
		}
		file, stat, err := s.openChild(parent, name, openFlags, current)
		if err != nil {
			return nil, syscall.Stat_t{}, err
		}
		if index == len(parts)-1 {
			if err := s.checkDirectory(current, stat); err != nil {
				file.Close()
				return nil, syscall.Stat_t{}, err
			}
			return file, stat, nil
		}
		if stat.Mode&syscall.S_IFMT != syscall.S_IFDIR {
			file.Close()
			if stat.Mode&syscall.S_IFMT == syscall.S_IFLNK {
				return nil, syscall.Stat_t{}, diagnostics.NewFailureWithRemediation("input.symlink", "input ancestor must be a directory without symbolic links", current, realPathRemedy)
			}
			return nil, syscall.Stat_t{}, diagnostics.NewFailureWithRemediation("input.not-directory", "input ancestor must be a directory without symbolic links", current, "check the path")
		}
		if err := s.rememberIdentity(current, stat); err != nil {
			file.Close()
			return nil, syscall.Stat_t{}, err
		}
		if owned {
			parent.Close()
		}
		parent = file
		owned = true
	}
	return nil, syscall.Stat_t{}, failure("input.read", "input path cannot be opened safely", path)
}

// openChild asks the session for one name; the session never follows a link
// at it, and the reader proves the descriptor it receives.
func (s *discovery) openChild(parent *os.File, name string, flags int, path string) (*os.File, syscall.Stat_t, error) {
	file, err := s.session.OpenAt(parent, name, flags)
	if err != nil {
		if canceled := s.ctx.Err(); canceled != nil {
			return nil, syscall.Stat_t{}, canceled
		}
		return nil, syscall.Stat_t{}, openFailure(err, path)
	}
	stat, err := statFile(file)
	if err != nil {
		file.Close()
		return nil, syscall.Stat_t{}, failure("input.read", "input handle cannot be verified", path)
	}
	return file, stat, nil
}

const realPathRemedy = "pass a path with no symbolic link in it, such as the output of realpath"

func openFailure(err error, path string) error {
	switch {
	case errors.Is(err, syscall.ENOENT):
		return diagnostics.NewFailureWithRemediation("input.not-found", "input path does not exist", path, "check the path")
	case errors.Is(err, syscall.ELOOP):
		return diagnostics.NewFailureWithRemediation("input.symlink", "input path is a symbolic link", path, realPathRemedy)
	case errors.Is(err, syscall.ENOTDIR):
		return diagnostics.NewFailureWithRemediation("input.not-directory", "a component of the input path is not a directory", path, "check the path")
	case denied(err):
		return openDenied(err, path)
	}
	return failure("input.read", "input path cannot be opened safely", path)
}

func (s *discovery) rememberIdentity(path string, stat syscall.Stat_t) error {
	if prior, exists := s.identities[path]; exists && !sameIdentity(prior, stat) {
		return failure("input.read", "input directory was replaced", path)
	}
	if err := s.checkDirectory(path, stat); err != nil {
		return err
	}
	s.identities[path] = stat
	return nil
}

func (s *discovery) checkDirectory(path string, stat syscall.Stat_t) error {
	if prior, exists := s.directories[path]; exists && !sameStable(prior, stat) {
		return failure("input.read", "input directory changed after discovery", path)
	}
	if prior, exists := s.identities[path]; exists && !sameIdentity(prior, stat) {
		return failure("input.read", "input directory was replaced", path)
	}
	return nil
}

func (s *discovery) verifyDirectories() error {
	paths := make([]string, 0, len(s.directories))
	for path := range s.directories {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	for _, path := range paths {
		file, _, err := s.openPath(path, pathHandle|syscall.O_DIRECTORY)
		if err != nil {
			return err
		}
		file.Close()
	}
	return s.ctx.Err()
}

// deniedToRoot is what a session's failure reports when the denied open ran
// with root's credentials.
type deniedToRoot interface{ DeniedToRoot() bool }

func denied(err error) bool {
	return errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM)
}

// openDenied names who the session's open was denied to: root only when the
// open ran as root, which a root invoking account alone does.
func openDenied(err error, path string) error {
	var root deniedToRoot
	return readDenied(errors.As(err, &root) && root.DeniedToRoot(), path)
}

func readDenied(root bool, path string) error {
	if root {
		return diagnostics.NewFailureWithRemediation("input.read", "root cannot read this input path (a network home with root squash?)", path,
			"copy it to a local directory and name the copy")
	}
	return diagnostics.NewFailureWithRemediation("input.read", "the invoking account cannot read this input path (permission denied)", path,
		"give the invoking account read access to it, or copy it to a directory that account can read")
}

// processFiles opens with this process's own credentials, for a Reader bound
// to no opener.
type processFiles struct{}

func (processFiles) Root() (*os.File, error) {
	fd, err := syscall.Open("/", syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), "/"), nil
}

func (processFiles) OpenAt(parent *os.File, name string, flags int) (*os.File, error) {
	fd, err := syscall.Openat(int(parent.Fd()), name, flags|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), name), nil
}

func (processFiles) Close() error { return nil }

func statFile(file *os.File) (syscall.Stat_t, error) {
	var stat syscall.Stat_t
	err := syscall.Fstat(int(file.Fd()), &stat)
	return stat, err
}

func sameIdentity(a, b syscall.Stat_t) bool {
	return a.Dev == b.Dev && a.Ino == b.Ino && a.Mode == b.Mode
}

func sameStable(a, b syscall.Stat_t) bool {
	return sameIdentity(a, b) && a.Nlink == b.Nlink && a.Size == b.Size && a.Mtim == b.Mtim && a.Ctim == b.Ctim
}

func yamlPath(path string) bool {
	return strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml")
}

func markerPosition(path string) bool {
	parts := strings.Split(filepath.ToSlash(path), "/")
	return len(parts) >= 4 && parts[len(parts)-4] == "add-ons" && parts[len(parts)-3] == "_store" && parts[len(parts)-1] == ".bootwright-addon"
}

func failure(code, message, path string) error {
	return diagnostics.NewFailure(code, message, path)
}

func limit(resource string, ceiling int, path string) error {
	return failure("input.limit", desiredstate.LimitMessage(resource, ceiling), path)
}
