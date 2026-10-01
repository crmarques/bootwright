//go:build linux && amd64

package material

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"unicode/utf8"

	"github.com/crmarques/bootwright/internal/secrets"
)

const (
	maxSecretPath = 4096
	pathHandle    = 0x200000
)

type heldDirectory struct {
	file   *os.File
	before syscall.Stat_t
}

type heldPart struct {
	request fileRequest
	path    string
	file    *os.File
	before  syscall.Stat_t
}

type secureFiles struct {
	ctx         context.Context
	root        *os.File
	directories map[string]heldDirectory
	failureCode string
	ownerUID    uint32
}

func (s *Service) readFileParts(ctx context.Context, requests []fileRequest) (map[secrets.Part][]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(requests) == 0 {
		return map[secrets.Part][]byte{}, nil
	}
	code := "input"
	identity := FileIdentity{UID: os.Getuid()}
	if s.operator != nil {
		var err error
		identity, err = s.operator.FileIdentity(ctx)
		if err != nil {
			return nil, err
		}
		if identity.UID < 0 || !canonicalAbsolute(identity.Home) {
			return nil, failure(code, "invoking account identity is invalid", "")
		}
	}
	for _, request := range requests {
		if _, valid := request.Encoding.maximumBytes(); !valid {
			return nil, failure(code, "secret file request uses an invalid transport encoding", request.Path)
		}
		switch request.Part {
		case secrets.CertificatePart, secrets.PasswordPart, secrets.PrivateKeyPart, secrets.PublicKeyPart, secrets.ValuePart:
		default:
			return nil, failure("input", "secret file request has an invalid part", "")
		}
	}
	resolved, err := resolveFileRequests(ctx, requests, code, identity.Home)
	if err != nil {
		return nil, err
	}

	reader, err := newSecureFiles(ctx, code)
	if err != nil {
		return nil, err
	}
	defer reader.close()
	reader.ownerUID = uint32(identity.UID)

	held := make([]heldPart, 0, len(requests))
	defer func() {
		for _, part := range held {
			part.file.Close()
		}
	}()
	for index, request := range requests {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		part, err := reader.openPart(request, resolved[index])
		if err != nil {
			return nil, err
		}
		held = append(held, part)
		maximum, _ := request.Encoding.maximumBytes()
		if part.before.Size > int64(maximum) {
			return nil, failure("store.limit", "secret file exceeds its transport byte limit", request.Path)
		}
	}

	parts, err := readHeldParts(ctx, held, code)
	if err != nil {
		return nil, err
	}
	if err := reader.verify(held); err != nil {
		clearParts(parts)
		return nil, err
	}
	return parts, nil
}

func resolveFileRequests(ctx context.Context, requests []fileRequest, code, home string) ([]string, error) {
	base := ""
	for _, request := range requests {
		if !filepath.IsAbs(request.Path) && request.Path != "~" && !strings.HasPrefix(request.Path, "~/") && !strings.HasPrefix(request.Path, "~") {
			var err error
			base, err = fileBase(ctx)
			if err != nil {
				return nil, err
			}
			break
		}
	}
	resolved := make([]string, len(requests))
	var err error
	for index, request := range requests {
		resolved[index], err = resolveSecretPathFor(ctx, base, request.Path, code, home)
		if err != nil {
			return nil, err
		}
	}
	return resolved, nil
}

func readHeldParts(ctx context.Context, held []heldPart, code string) (map[secrets.Part][]byte, error) {
	parts := make(map[secrets.Part][]byte, len(held))
	fail := func(err error) (map[secrets.Part][]byte, error) {
		clearParts(parts)
		return nil, err
	}
	for _, part := range held {
		data, err := readSecretFileEncoded(ctx, part.file, part.request.Encoding)
		if err != nil {
			if canceled := ctx.Err(); canceled != nil {
				return fail(canceled)
			}
			return fail(failure(code, "secret file bytes could not be read safely", part.request.Path))
		}
		maximum, _ := part.request.Encoding.maximumBytes()
		if len(data) > maximum {
			clear(data)
			return fail(failure("store.limit", "secret file exceeds its transport byte limit", part.request.Path))
		}
		if int64(len(data)) != part.before.Size {
			clear(data)
			return fail(failure(code, "secret file changed while it was read", part.request.Path))
		}
		if _, duplicate := parts[part.request.Part]; duplicate {
			clear(data)
			return fail(failure(code, "secret file request contains a duplicate part", part.request.Path))
		}
		parts[part.request.Part] = data
	}
	return parts, nil
}

func fileBase(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	base, err := os.Getwd()
	if err != nil || !canonicalAbsolute(base) {
		return "", failure("input", "invocation working directory is unavailable", "")
	}
	return base, nil
}

func resolveSecretPath(ctx context.Context, base, authored, code string) (string, error) {
	return resolveSecretPathFor(ctx, base, authored, code, "")
}

func resolveSecretPathFor(ctx context.Context, base, authored, code, home string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if authored == "" || len(authored) > maxSecretPath || !utf8.ValidString(authored) || strings.ContainsRune(authored, 0) {
		return "", failure(code, "secret file path is empty or invalid", authored)
	}
	path := authored
	if authored == "~" || strings.HasPrefix(authored, "~/") {
		if home == "" {
			var err error
			home, err = accountHome(ctx, code)
			if err != nil {
				return "", err
			}
		}
		if authored == "~" {
			path = home
		} else {
			path = filepath.Join(home, strings.TrimPrefix(authored, "~/"))
		}
	} else if strings.HasPrefix(authored, "~") {
		return "", failure(code, "secret file path supports only the current account tilde", authored)
	} else if !filepath.IsAbs(authored) {
		path = filepath.Join(base, authored)
	}
	path = filepath.Clean(path)
	if !canonicalAbsolute(path) {
		return "", failure(code, "secret file path cannot be resolved safely", authored)
	}
	return path, nil
}

func canonicalAbsolute(path string) bool {
	return len(path) > 0 && len(path) <= maxSecretPath && utf8.ValidString(path) &&
		!strings.ContainsRune(path, 0) && filepath.IsAbs(path) && filepath.Clean(path) == path
}

func accountHome(ctx context.Context, code string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	fd, err := syscall.Open("/etc/passwd", syscall.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return "", failure(code, "local account home is unavailable", "")
	}
	file := os.NewFile(uintptr(fd), "/etc/passwd")
	defer file.Close()
	before, err := statSecretFile(file)
	if err != nil || before.Mode&syscall.S_IFMT != syscall.S_IFREG || before.Size < 0 || before.Size > 1<<20 {
		return "", failure(code, "local account database cannot be verified", "")
	}
	data, err := readWithLimit(ctx, file, (1<<20)+1)
	if err != nil {
		if canceled := ctx.Err(); canceled != nil {
			return "", canceled
		}
		return "", failure(code, "local account database cannot be read safely", "")
	}
	defer clear(data)
	after, err := statSecretFile(file)
	if err != nil || len(data) > 1<<20 || !sameStableFile(before, after) {
		return "", failure(code, "local account database cannot be read safely", "")
	}
	uid := strconv.Itoa(os.Getuid())
	home := ""
	for _, entry := range strings.Split(string(data), "\n") {
		fields := strings.Split(entry, ":")
		if len(fields) != 7 || fields[2] != uid {
			continue
		}
		if home != "" || !canonicalAbsolute(fields[5]) {
			return "", failure(code, "local account home is ambiguous", "")
		}
		home = fields[5]
	}
	if home == "" {
		return "", failure(code, "local account home is unavailable", "")
	}
	return home, nil
}

func newSecureFiles(ctx context.Context, code string) (*secureFiles, error) {
	fd, err := syscall.Open("/", syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		if canceled := ctx.Err(); canceled != nil {
			return nil, canceled
		}
		return nil, failure(code, "secret file root could not be opened safely", "")
	}
	return &secureFiles{
		ctx: ctx, root: os.NewFile(uintptr(fd), "/"),
		directories: make(map[string]heldDirectory), failureCode: code, ownerUID: uint32(os.Getuid()),
	}, nil
}

func (s *secureFiles) close() {
	for _, directory := range s.directories {
		directory.file.Close()
	}
	if s.root != nil {
		s.root.Close()
	}
}

func (s *secureFiles) openPart(request fileRequest, path string) (heldPart, error) {
	parent := s.root
	parts := pathParts(path)
	current := ""
	for index, name := range parts {
		if err := s.ctx.Err(); err != nil {
			return heldPart{}, err
		}
		current = filepath.Join(current, "/", name)
		if index == len(parts)-1 {
			file, stat, err := openSecretChild(parent, name, syscall.O_RDONLY|syscall.O_NONBLOCK)
			if err != nil {
				if canceled := s.ctx.Err(); canceled != nil {
					return heldPart{}, canceled
				}
				return heldPart{}, failure(s.failureCode, "secret file could not be opened safely", request.Path)
			}
			if !safeOperatorFileFor(stat, s.ownerUID) {
				file.Close()
				return heldPart{}, failure(s.failureCode, "secret file type, owner, links, or permissions are unsafe", request.Path)
			}
			return heldPart{request: request, path: path, file: file, before: stat}, nil
		}
		if directory, exists := s.directories[current]; exists {
			parent = directory.file
			continue
		}
		file, stat, err := openSecretChild(parent, name, pathHandle|syscall.O_DIRECTORY)
		if err != nil || stat.Mode&syscall.S_IFMT != syscall.S_IFDIR {
			if file != nil {
				file.Close()
			}
			if canceled := s.ctx.Err(); canceled != nil {
				return heldPart{}, canceled
			}
			return heldPart{}, failure(s.failureCode, "secret file ancestor is not a safe directory", request.Path)
		}
		s.directories[current] = heldDirectory{file: file, before: stat}
		parent = file
	}
	return heldPart{}, failure(s.failureCode, "secret file path is invalid", request.Path)
}

func (s *secureFiles) verify(parts []heldPart) error {
	paths := make([]string, 0, len(s.directories))
	for path := range s.directories {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		directory := s.directories[path]
		if err := s.ctx.Err(); err != nil {
			return err
		}
		after, err := statSecretFile(directory.file)
		if err != nil || !sameFileIdentity(directory.before, after) {
			return failure(s.failureCode, "secret file directory changed during acquisition", path)
		}
	}
	for _, part := range parts {
		after, err := statSecretFile(part.file)
		if err != nil || !sameStableFile(part.before, after) {
			return failure(s.failureCode, "secret file changed during acquisition", part.request.Path)
		}
		current, err := s.freshStat(part.path)
		if err != nil || !sameStableFile(part.before, current) {
			if canceled := s.ctx.Err(); canceled != nil {
				return canceled
			}
			return failure(s.failureCode, "secret file was replaced during acquisition", part.request.Path)
		}
	}
	return s.ctx.Err()
}

func (s *secureFiles) freshStat(path string) (syscall.Stat_t, error) {
	fd, err := syscall.Open("/", syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return syscall.Stat_t{}, err
	}
	parent := os.NewFile(uintptr(fd), "/")
	defer func() { parent.Close() }()
	current := ""
	parts := pathParts(path)
	for index, name := range parts {
		current = filepath.Join(current, "/", name)
		flags := pathHandle | syscall.O_DIRECTORY
		if index == len(parts)-1 {
			flags = pathHandle
		}
		file, stat, err := openSecretChild(parent, name, flags)
		if err != nil {
			return syscall.Stat_t{}, err
		}
		if index != len(parts)-1 {
			original, exists := s.directories[current]
			if !exists || stat.Mode&syscall.S_IFMT != syscall.S_IFDIR || !sameFileIdentity(original.before, stat) {
				file.Close()
				return syscall.Stat_t{}, errors.New("directory changed")
			}
		}
		parent.Close()
		parent = file
	}
	stat, err := statSecretFile(parent)
	return stat, err
}

func pathParts(path string) []string {
	return strings.Split(strings.TrimPrefix(path, "/"), "/")
}

func openSecretChild(parent *os.File, name string, flags int) (*os.File, syscall.Stat_t, error) {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\x00") {
		return nil, syscall.Stat_t{}, syscall.EINVAL
	}
	fd, err := syscall.Openat(int(parent.Fd()), name, flags|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, syscall.Stat_t{}, err
	}
	file := os.NewFile(uintptr(fd), name)
	stat, err := statSecretFile(file)
	if err != nil {
		file.Close()
		return nil, syscall.Stat_t{}, err
	}
	return file, stat, nil
}

func safeOperatorFile(stat syscall.Stat_t) bool {
	return safeOperatorFileFor(stat, uint32(os.Getuid()))
}

func safeOperatorFileFor(stat syscall.Stat_t, uid uint32) bool {
	permissions := stat.Mode & 07777
	return stat.Mode&syscall.S_IFMT == syscall.S_IFREG && stat.Uid == uid &&
		stat.Nlink == 1 && (permissions == 0400 || permissions == 0600) && stat.Size >= 0
}

func statSecretFile(file *os.File) (syscall.Stat_t, error) {
	var stat syscall.Stat_t
	err := syscall.Fstat(int(file.Fd()), &stat)
	return stat, err
}

func sameStableFile(left, right syscall.Stat_t) bool {
	return sameFileIdentity(left, right) && left.Nlink == right.Nlink &&
		left.Size == right.Size && left.Mtim == right.Mtim && left.Ctim == right.Ctim
}

func sameFileIdentity(left, right syscall.Stat_t) bool {
	return left.Dev == right.Dev && left.Ino == right.Ino && left.Mode == right.Mode && left.Uid == right.Uid && left.Gid == right.Gid
}

func readSecretFile(ctx context.Context, file *os.File) ([]byte, error) {
	return readSecretFileEncoded(ctx, file, transportExact)
}

func readSecretFileEncoded(ctx context.Context, file *os.File, encoding transportEncoding) ([]byte, error) {
	maximum, valid := encoding.maximumBytes()
	if !valid {
		return nil, errors.New("invalid transport encoding")
	}
	return readWithLimit(ctx, file, maximum+1)
}

func readWithLimit(ctx context.Context, file *os.File, maximum int) ([]byte, error) {
	data := make([]byte, 0, min(maximum, 32768))
	buffer := make([]byte, min(maximum, 32768))
	defer clear(buffer)
	for len(data) < maximum {
		if err := ctx.Err(); err != nil {
			clear(data)
			return nil, err
		}
		n, err := file.Read(buffer[:min(len(buffer), maximum-len(data))])
		if n < 0 || n > min(len(buffer), maximum-len(data)) {
			clear(data)
			return nil, errors.New("invalid read count")
		}
		data = append(data, buffer[:n]...)
		if errors.Is(err, io.EOF) {
			return data, nil
		}
		if err != nil || n == 0 {
			clear(data)
			return nil, errors.New("file read failed")
		}
	}
	return data, nil
}
