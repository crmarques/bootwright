package bundlelocal

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"path"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

const maxToolExpandedBytes int64 = 2 << 30

func approvedToolBytes(source prerequisites.DependencySource, data []byte) bool {
	if source.Bytes <= 0 || source.Bytes > maxToolSourceBytes || int64(len(data)) != source.Bytes {
		return false
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]) == source.SHA256
}

// projectTool is a read-only integrity calculation. The Ansible dependency role
// alone publishes tool payloads. No archive entry obtains filesystem authority.
func projectTool(ctx context.Context, tool prerequisites.ToolDefinition, data []byte) (map[string]projectedFile, error) {
	if !approvedToolBytes(tool.Source, data) {
		return nil, bundleFailure("retained target source differs from its approved size and checksum")
	}
	if err := validateFrozenTool(tool); err != nil {
		return nil, err
	}
	result := map[string]projectedFile{}
	if tool.Archive == "binary" {
		result[tool.Files[0].Path] = projectedFile{data: data, executable: true}
		return result, ctx.Err()
	}
	compressed, err := gzip.NewReader(bytes.NewReader(data))
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
	regular := map[string][]byte{}
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
			content, err := io.ReadAll(io.LimitReader(reader, header.Size+1))
			if err != nil || int64(len(content)) != header.Size || len(content) == 0 || header.Mode&0111 == 0 {
				return nil, bundleFailure("selected target executable is incomplete or lacks executable mode")
			}
			regular[name] = content
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
		result[file.Path] = projectedFile{data: content, executable: true}
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
