package bundlelocal

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io"
	"io/fs"
	"path"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/crmarques/bootwright/ansible"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

const (
	maxArchiveEntries = 16000
	maxExpandedBytes  = 256 << 20
	maxMemberBytes    = 64 << 20
	sitePackages      = "python/lib/python3.13/site-packages/"
)

type projectedFile struct {
	data       []byte
	executable bool
}

type projection struct {
	files map[string]projectedFile
	// parents holds every ancestor directory of every added file, so a
	// file-directory collision is a lookup rather than a scan of the whole
	// projection. A closure reaches thousands of files and is rebuilt for every
	// inspection, so that scan was the projection's dominant cost.
	parents map[string]bool
	bytes   int64
	site    string
}

func newProjection() *projection {
	return &projection{files: make(map[string]projectedFile), parents: make(map[string]bool), site: sitePackages}
}

func projectionFor(record catalogRecord) *projection {
	p := newProjection()
	if record.Bootstrap != nil {
		p.site = record.Bootstrap.SitePackages
	}
	return p
}

func (p *projection) identity() string {
	names := make([]string, 0, len(p.files))
	for name := range p.files {
		names = append(names, name)
	}
	sort.Strings(names)
	hash := sha256.New()
	hash.Write([]byte("bootwright.controller.projection-v1\x00"))
	for _, name := range names {
		file := p.files[name]
		binary.Write(hash, binary.BigEndian, uint32(len(name)))
		hash.Write([]byte(name))
		binary.Write(hash, binary.BigEndian, uint64(len(file.data)))
		flag := byte(0)
		if file.executable {
			flag = 1
		}
		hash.Write([]byte{flag})
		digest := sha256.Sum256(file.data)
		hash.Write(digest[:])
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func (p *projection) matches(value *prerequisites.BootstrapDefinition) bool {
	return value == nil || value.FileCount == len(p.files) && value.ExpandedBytes == p.bytes && value.ProjectionSHA256 == p.identity()
}

// The baseline bootstrap carries the compiled Ansible roles beside the private
// Python runtime. Target dependency installation remains owned by those roles.
func (p *projection) automation(ctx context.Context) error {
	assets := ansible.Assets()
	names := make([]string, 0, len(assets))
	for name := range assets {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !validPath(name) {
			return bundleFailure("compiled automation asset has an invalid path")
		}
		if err := p.add(path.Join("automation", name), assets[name], false); err != nil {
			return err
		}
	}
	return nil
}

func validPath(name string) bool {
	return name != "" && len(name) <= 4096 && utf8.ValidString(name) &&
		!strings.ContainsAny(name, "\\\x00\r\n") && name != "." &&
		!strings.HasPrefix(name, "/") && !strings.HasPrefix(name, "../") &&
		path.Clean(name) == name
}

func (p *projection) add(name string, data []byte, executable bool) error {
	if !validPath(name) || len(p.files) >= maxArchiveEntries || len(data) > maxMemberBytes || p.bytes+int64(len(data)) > maxExpandedBytes {
		return bundleFailure("dependency archive exceeds its qualified path or size limits")
	}
	if _, found := p.files[name]; found {
		return bundleFailure("dependency archive contains conflicting file entries")
	}
	// A path something was already added beneath is a directory, and an ancestor
	// that already holds content is a file. Either way this entry collides.
	if p.parents[name] {
		return bundleFailure("dependency archive contains a file-directory collision")
	}
	for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
		if _, found := p.files[parent]; found {
			return bundleFailure("dependency archive contains a file-directory collision")
		}
	}
	p.files[name] = projectedFile{data: data, executable: executable}
	p.bytes += int64(len(data))
	// Ancestors above the first one already recorded were recorded with it.
	for parent := path.Dir(name); parent != "." && !p.parents[parent]; parent = path.Dir(parent) {
		p.parents[parent] = true
	}
	return nil
}

func approvedBytes(source prerequisites.DependencySource, data []byte) bool {
	if source.Bytes <= 0 || source.Bytes > maxMemberBytes || int64(len(data)) != source.Bytes {
		return false
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]) == source.SHA256
}

func (p *projection) archive(ctx context.Context, data []byte) error {
	compressed, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return bundleFailure("approved Python archive cannot be decoded")
	}
	defer compressed.Close()
	reader := tar.NewReader(io.LimitReader(compressed, maxExpandedBytes+1))
	links := map[string]string{}
	entries := map[string]bool{}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil || !validPath(header.Name) || !strings.HasPrefix(header.Name, "python/") || entries[header.Name] || len(entries) >= maxArchiveEntries {
			return bundleFailure("approved Python archive has an invalid or duplicate entry")
		}
		entries[header.Name] = true
		if header.Mode&07000 != 0 {
			return bundleFailure("dependency archive contains an unapproved special mode")
		}
		switch header.Typeflag {
		case tar.TypeReg, tar.TypeRegA:
			if header.Size < 0 || header.Size > maxMemberBytes {
				return bundleFailure("dependency archive member exceeds its qualified size limit")
			}
			content, err := io.ReadAll(io.LimitReader(reader, header.Size+1))
			if err != nil || int64(len(content)) != header.Size {
				return bundleFailure("dependency archive member is incomplete")
			}
			if err := p.add(header.Name, content, header.Mode&0111 != 0); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if header.Linkname == "" || path.IsAbs(header.Linkname) || strings.ContainsAny(header.Linkname, "\\\x00\r\n") {
				return bundleFailure("dependency archive contains an unsafe link")
			}
			target := path.Join(path.Dir(header.Name), header.Linkname)
			if !validPath(target) || !strings.HasPrefix(target, "python/") {
				return bundleFailure("dependency archive link escapes the Python tree")
			}
			links[header.Name] = target
		default:
			return bundleFailure("dependency archive contains an unqualified entry type")
		}
	}
	// Materialize only links resolving to an archive regular file. No live link
	// is ever handed to storage, and cycles, directory links and absent targets
	// cannot acquire filesystem authority.
	names := make([]string, 0, len(links))
	for name := range links {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		target := links[name]
		for depth := 0; ; depth++ {
			if depth >= 64 || target == name {
				return bundleFailure("dependency archive contains a cyclic or excessive link chain")
			}
			next, linked := links[target]
			if !linked {
				break
			}
			target = next
		}
		file, exists := p.files[target]
		if !exists {
			return bundleFailure("dependency archive link does not resolve to an approved regular file")
		}
		if err := p.add(name, file.data, file.executable); err != nil {
			return err
		}
	}
	return nil
}

func (p *projection) wheel(ctx context.Context, data []byte) error {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil || len(reader.File) > maxArchiveEntries {
		return bundleFailure("approved wheel cannot be decoded within its entry limit")
	}
	seen := map[string]bool{}
	for _, member := range reader.File {
		if err := ctx.Err(); err != nil {
			return err
		}
		name := member.Name
		if member.FileInfo().IsDir() {
			name = strings.TrimSuffix(name, "/")
		}
		if !validPath(name) || seen[name] || member.Mode()&(fs.ModeSetuid|fs.ModeSetgid|fs.ModeSticky) != 0 ||
			member.UncompressedSize64 > maxMemberBytes || strings.Contains(member.Name, ".data/") ||
			strings.HasSuffix(member.Name, ".pth") || path.Base(member.Name) == "sitecustomize.py" || path.Base(member.Name) == "usercustomize.py" {
			return bundleFailure("wheel entry is outside the qualified site-packages projection")
		}
		seen[name] = true
		if member.FileInfo().IsDir() && member.UncompressedSize64 == 0 {
			continue
		}
		if !member.Mode().IsRegular() {
			return bundleFailure("wheel entry is not a qualified regular file")
		}
		stream, err := member.Open()
		if err != nil {
			return bundleFailure("approved wheel member cannot be read")
		}
		content, readErr := io.ReadAll(io.LimitReader(stream, int64(member.UncompressedSize64)+1))
		closeErr := stream.Close()
		if readErr != nil || closeErr != nil || uint64(len(content)) != member.UncompressedSize64 {
			return bundleFailure("approved wheel member is incomplete or corrupt")
		}
		if err := p.add(p.site+member.Name, content, member.Mode()&0111 != 0); err != nil {
			return err
		}
	}
	return nil
}

func (p *projection) directories() []string {
	result := make([]string, 0, len(p.parents))
	for name := range p.parents {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}
