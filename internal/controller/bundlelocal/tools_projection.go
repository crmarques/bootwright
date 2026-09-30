package bundlelocal

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"hash"
	"io"
	"path"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

const maxToolExpandedBytes int64 = 2 << 30

// releaseMarker is where a released oc names its release: the version,
// NUL-terminated, overwrites its head. An unstamped oc carries it whole. It is
// the marker the collection's controller_files module scans for.
var releaseMarker = []byte("\x00_RELEASE_VERSION_LOCATION_\x00" + strings.Repeat("X", 64) + "\x00")

// patternCount counts a pattern in the blocks written to it, carrying between
// blocks only the bytes an occurrence split across them needs.
type patternCount struct {
	pattern []byte
	window  []byte
	count   int
}

func (p *patternCount) update(block []byte) {
	p.window = append(p.window, block...)
	for from := 0; ; from++ {
		found := bytes.Index(p.window[from:], p.pattern)
		if found < 0 {
			break
		}
		p.count++
		from += found
	}
	keep := min(len(p.pattern)-1, len(p.window))
	p.window = p.window[:copy(p.window, p.window[len(p.window)-keep:])]
}

// releaseStamp reads the release an oc member names as it streams, without
// executing it: exactly one marker stamped with the frozen version, and no
// unstamped one.
type releaseStamp struct{ stamped, unstamped patternCount }

func newReleaseStamp(version string) (*releaseStamp, error) {
	if len(version) >= len(releaseMarker)-1 {
		return nil, bundleFailure("frozen OpenShift client release is longer than its release marker holds")
	}
	stamp := append([]byte(version+"\x00"), releaseMarker[len(version)+1:]...)
	return &releaseStamp{stamped: patternCount{pattern: stamp}, unstamped: patternCount{pattern: releaseMarker}}, nil
}

func (s *releaseStamp) Write(block []byte) (int, error) {
	s.stamped.update(block)
	s.unstamped.update(block)
	return len(block), nil
}

func (s *releaseStamp) proved() bool {
	return s != nil && s.stamped.count == 1 && s.unstamped.count == 0
}

type streamedFile struct {
	sha256     string
	size       int64
	executable bool
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(data []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(data)
}

type digestReader struct {
	reader io.Reader
	digest hash.Hash
	read   int64
}

func (r *digestReader) Read(data []byte) (int, error) {
	n, err := r.reader.Read(data)
	r.digest.Write(data[:n])
	r.read += int64(n)
	return n, err
}

// projectTool is a read-only integrity calculation over a retained source
// streamed once. Neither the source nor a member is held whole: each selected
// member is reduced to its digest as it streams. The Ansible dependency role
// alone publishes tool payloads. No archive entry obtains filesystem authority.
func projectTool(ctx context.Context, tool prerequisites.ToolDefinition, source io.Reader) (map[string]streamedFile, error) {
	if err := validateFrozenTool(tool); err != nil {
		return nil, err
	}
	if tool.Source.Bytes <= 0 || tool.Source.Bytes > maxToolSourceBytes {
		return nil, bundleFailure("retained target source differs from its approved size and checksum")
	}
	retained := &digestReader{reader: contextReader{ctx: ctx, reader: io.LimitReader(source, tool.Source.Bytes+1)}, digest: sha256.New()}
	result, projected := projectMembers(ctx, tool, retained)
	if errors.Is(projected, context.Canceled) || errors.Is(projected, context.DeadlineExceeded) {
		return nil, projected
	}
	if _, err := io.Copy(io.Discard, retained); err != nil {
		return nil, err
	}
	if retained.read != tool.Source.Bytes || hex.EncodeToString(retained.digest.Sum(nil)) != tool.Source.SHA256 {
		return nil, bundleFailure("retained target source differs from its approved size and checksum")
	}
	return result, projected
}

func projectMembers(ctx context.Context, tool prerequisites.ToolDefinition, source io.Reader) (map[string]streamedFile, error) {
	if tool.Archive == "binary" {
		return map[string]streamedFile{tool.Files[0].Path: {sha256: tool.Source.SHA256, size: tool.Source.Bytes, executable: true}}, nil
	}
	compressed, err := gzip.NewReader(source)
	if err != nil {
		return nil, bundleFailure("retained target archive cannot be decoded")
	}
	defer compressed.Close()
	bounded := &io.LimitedReader{R: compressed, N: maxToolExpandedBytes + 1}
	reader := tar.NewReader(bounded)
	selected := map[string]bool{}
	for _, file := range tool.Files {
		selected[file.Member] = true
	}
	regular := map[string]streamedFile{}
	stamps := map[string]*releaseStamp{}
	links := map[string]string{}
	seen := map[string]bool{}
	var expanded int64
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil || bounded.N <= 0 {
			return nil, bundleFailure("retained target archive exceeds its expanded bound or is malformed")
		}
		name := header.Name
		if header.Typeflag == tar.TypeDir {
			name = strings.TrimSuffix(name, "/")
		}
		if !validPath(name) || seen[name] || len(seen) >= 4096 || header.Mode&07000 != 0 {
			return nil, bundleFailure("target archive contains an unsafe, duplicate, or unbounded member")
		}
		seen[name] = true
		switch header.Typeflag {
		case tar.TypeDir:
			if header.Size != 0 {
				return nil, bundleFailure("target archive directory has unexpected data")
			}
		case tar.TypeReg, tar.TypeRegA:
			if header.Size < 0 || header.Size > maxToolSourceBytes || expanded > maxToolExpandedBytes-header.Size {
				return nil, bundleFailure("target archive member exceeds the expanded bound")
			}
			expanded += header.Size
			if !selected[name] {
				continue
			}
			var stamp *releaseStamp
			if tool.Kind == "openshift-clients" {
				if stamp, err = newReleaseStamp(tool.Version); err != nil {
					return nil, err
				}
				stamps[name] = stamp
			}
			member, err := streamMember(ctx, reader, header, stamp)
			if err != nil {
				return nil, err
			}
			regular[name] = member
		case tar.TypeSymlink, tar.TypeLink:
			target := header.Linkname
			if header.Typeflag == tar.TypeSymlink {
				target = path.Join(path.Dir(name), target)
			}
			if !validPath(header.Linkname) || !validPath(target) || !selected[name] || !selected[target] {
				return nil, bundleFailure("target archive link is outside its fixed executable members")
			}
			links[name] = target
		default:
			return nil, bundleFailure("target archive contains an unsupported member type")
		}
	}
	return resolveMembers(tool, regular, links, stamps)
}

// streamMember digests one selected member as it streams, and reads its
// release stamp when stamp is given.
func streamMember(ctx context.Context, reader io.Reader, header *tar.Header, stamp *releaseStamp) (streamedFile, error) {
	member := &digestReader{reader: contextReader{ctx: ctx, reader: io.LimitReader(reader, header.Size+1)}, digest: sha256.New()}
	sink := io.Discard
	if stamp != nil {
		sink = stamp
	}
	if _, err := io.Copy(sink, member); err != nil {
		if ctx.Err() != nil {
			return streamedFile{}, ctx.Err()
		}
		return streamedFile{}, bundleFailure("selected target executable is incomplete or lacks executable mode")
	}
	if member.read != header.Size || member.read == 0 || header.Mode&0111 == 0 {
		return streamedFile{}, bundleFailure("selected target executable is incomplete or lacks executable mode")
	}
	return streamedFile{sha256: hex.EncodeToString(member.digest.Sum(nil)), size: member.read, executable: true}, nil
}

// resolveMembers maps each file to the regular member its links lead to. An
// OpenShift client is proved only after every member resolves, and only when
// the member oc resolves to names the frozen release, as the adapter requires
// before it publishes either client.
func resolveMembers(tool prerequisites.ToolDefinition, regular map[string]streamedFile, links map[string]string, stamps map[string]*releaseStamp) (map[string]streamedFile, error) {
	result := map[string]streamedFile{}
	released := tool.Kind != "openshift-clients"
	for _, file := range tool.Files {
		member := file.Member
		for depth := 0; links[member] != ""; depth++ {
			if depth >= 16 {
				return nil, bundleFailure("target archive executable links are cyclic or excessive")
			}
			member = links[member]
		}
		content, found := regular[member]
		if !found {
			return nil, bundleFailure("target archive omits a required executable member")
		}
		result[file.Path] = content
		if !released && file.Member == "oc" {
			released = stamps[member].proved()
		}
	}
	if !released {
		return nil, prerequisites.UnreleasedClient(tool)
	}
	return result, nil
}

func validateFrozenTool(tool prerequisites.ToolDefinition) error {
	if !toolVersion(tool.Version) || !safeToolURL(tool.Source.URL) || !strings.HasPrefix(tool.Source.ID, "tool-"+tool.Kind+"-") || !strings.HasSuffix(tool.Source.ID, "-"+tool.Version) {
		return bundleFailure("frozen target tool identity is malformed")
	}
	if tool.Compatibility != "okd" && !stableVersion(tool.Version) {
		return bundleFailure("frozen target tool must identify an exact stable release")
	}
	members := []string{tool.Kind}
	archive := "tar.gz"
	switch tool.Kind {
	case "helm":
		members = []string{"linux-amd64/helm"}
	case "govc":
	case "openshift-clients":
		members = []string{"oc", "kubectl"}
	case "openshift-install":
	case "virtctl", "kubectl":
		archive = "binary"
	default:
		return bundleFailure("frozen target tool is outside the supported vocabulary")
	}
	if tool.Kind == "openshift-clients" || tool.Kind == "openshift-install" {
		if tool.Compatibility != "openshift" && tool.Compatibility != "okd" {
			return bundleFailure("frozen cluster tool lacks target compatibility")
		}
	} else if tool.Kind == "virtctl" {
		if tool.Compatibility != "kubevirt" {
			return bundleFailure("frozen virtctl does not identify the upstream KubeVirt publisher")
		}
	} else if tool.Compatibility != "" {
		return bundleFailure("frozen generic tool has unknown compatibility metadata")
	}
	expected := make([]prerequisites.ToolFile, 0, len(members))
	for _, member := range members {
		expected = append(expected, prerequisites.ToolFile{Member: member, Path: path.Join("tools", tool.Kind, tool.Compatibility, tool.Version, path.Base(member))})
	}
	if tool.Archive != archive || !slices.Equal(tool.Files, expected) {
		return bundleFailure("frozen target tool differs from its fixed executable projection")
	}
	return nil
}
