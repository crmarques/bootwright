package ansible

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"slices"
	"strings"
)

//go:embed ansible.cfg controller collections
var embedded embed.FS

func Assets() map[string][]byte {
	files := map[string][]byte{}
	_ = fs.WalkDir(embedded, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && (strings.HasSuffix(name, "/tests") || strings.HasSuffix(name, "/__pycache__")) {
			return fs.SkipDir
		}
		if !entry.IsDir() {
			data, err := embedded.ReadFile(name)
			if err != nil {
				return err
			}
			files[name] = data
		}
		return nil
	})
	return files
}

// documentationPaths are the collection's root README and CHANGELOG. They run
// nothing, so they stay in every bundle but leave the automation digest.
var documentationPaths = []string{
	"collections/ansible_collections/bootwright/core/CHANGELOG.rst",
	"collections/ansible_collections/bootwright/core/README.md",
}

// Automation is every embedded file the automation digest covers: Assets
// without the collection's documentation.
func Automation() map[string][]byte {
	automation, _ := split(Assets())
	return automation
}

// Documentation is exactly the collection's root README and CHANGELOG.
func Documentation() map[string][]byte {
	_, documentation := split(Assets())
	return documentation
}

func split(files map[string][]byte) (automation, documentation map[string][]byte) {
	automation, documentation = make(map[string][]byte, len(files)), make(map[string][]byte, len(documentationPaths))
	for name, data := range files {
		if slices.Contains(documentationPaths, name) {
			documentation[name] = data
		} else {
			automation[name] = data
		}
	}
	return automation, documentation
}

// Digest identifies the embedded automation: every file Automation returns,
// under a domain version of its own, so no digest that covered documentation
// equals one that does not. Each check of an approved bundle's automation
// compares exactly these files byte for byte, so a documentation-only build
// keeps the digest and the bundle it names. specs/controller.md owns the rule.
func Digest() string { return digestOf(Automation()) }

func digestOf(files map[string][]byte) string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	slices.Sort(names)
	digest := sha256.New()
	digest.Write([]byte("bootwright.controller.automation-v2\x00"))
	for _, name := range names {
		digest.Write([]byte(name))
		digest.Write([]byte{0})
		content := sha256.Sum256(files[name])
		digest.Write(content[:])
	}
	return hex.EncodeToString(digest.Sum(nil))
}
