package cli

import (
	"io"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge/bundle"
)

var (
	versionString = "dev"
	gitCommit     = "unknown"
)

const unavailableArchiveIdentity = "unavailable"

type versionMetadata struct {
	version     string
	gitCommit   string
	vcsModified string
}

func bundleVersionMarker() string {
	size, digest, err := bundle.EmbeddedArchiveIdentity()
	if err != nil {
		size, digest = 0, unavailableArchiveIdentity
	}
	return bundleVersionMarkerFrom(currentVersionMetadata(), size, digest)
}

func bundleVersionMarkerFrom(metadata versionMetadata, archiveBytes int64, archiveDigest string) string {
	return "version=" + metadata.version +
		"\ngitCommit=" + metadata.gitCommit +
		"\nvcsModified=" + metadata.vcsModified +
		"\nbundleBytes=" + strconv.FormatInt(archiveBytes, 10) +
		"\nbundleArchive=" + archiveDigest
}

func currentVersionMetadata() versionMetadata {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return versionMetadataFrom(versionString, gitCommit, nil)
	}
	return versionMetadataFrom(versionString, gitCommit, info)
}

func versionMetadataFrom(version, commit string, info *debug.BuildInfo) versionMetadata {
	metadata := versionMetadata{
		version:     strings.TrimSpace(version),
		gitCommit:   strings.TrimSpace(commit),
		vcsModified: buildSetting(info, "vcs.modified"),
	}
	if metadata.version == "" {
		metadata.version = "dev"
	}
	if metadata.gitCommit == "" || metadata.gitCommit == "unknown" {
		metadata.gitCommit = vcsRevision(info)
	}
	if metadata.gitCommit == "" {
		metadata.gitCommit = "unknown"
	}
	if metadata.vcsModified == "" {
		metadata.vcsModified = "unknown"
	}
	return metadata
}

func vcsRevision(info *debug.BuildInfo) string {
	return shortCommit(buildSetting(info, "vcs.revision"))
}

func buildSetting(info *debug.BuildInfo, key string) string {
	if info == nil {
		return ""
	}
	for _, setting := range info.Settings {
		if setting.Key == key {
			return strings.TrimSpace(setting.Value)
		}
	}
	return ""
}

func shortCommit(commit string) string {
	commit = strings.TrimSpace(commit)
	if len(commit) > 7 {
		return commit[:7]
	}
	return commit
}

func embeddedBundleDigestDisplay() string {
	digest, err := bundle.EmbeddedBundleDigest()
	if err != nil {
		if bundle.IsEmptyAnsibleBundle(err) {
			return "not built (run make build)"
		}
		return "unknown"
	}
	if len(digest) > 12 {
		return digest[:12]
	}
	return digest
}

func newVersionCmd(stdout io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print build version, git commit, and Go runtime",
		Args:  cobra.NoArgs,
		Run: func(_ *cobra.Command, _ []string) {
			metadata := currentVersionMetadata()
			p := output.New(stdout)
			p.Command("version")
			p.Section("Build")
			p.Fields([]output.Field{
				{Key: "version", Value: metadata.version},
				{Key: "git commit", Value: metadata.gitCommit},
				{Key: "go", Value: runtime.Version() + " " + runtime.GOOS + "/" + runtime.GOARCH},
				{Key: "ansible bundle", Value: embeddedBundleDigestDisplay()},
			})
			p.Summary(output.StatusOK, "bootwright", metadata.version)
		},
	}
}
