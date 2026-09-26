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

// Digest identifies the embedded automation. It covers every file Assets
// returns, documentation included, because each check of an approved bundle's
// automation compares every one of those files byte for byte: a file the digest
// skipped could differ under an equal digest. specs/controller.md owns the rule.
func Digest() string { return digestOf(Assets()) }

func digestOf(files map[string][]byte) string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	slices.Sort(names)
	digest := sha256.New()
	digest.Write([]byte("bootwright.controller.automation-v1\x00"))
	for _, name := range names {
		digest.Write([]byte(name))
		digest.Write([]byte{0})
		content := sha256.Sum256(files[name])
		digest.Write(content[:])
	}
	return hex.EncodeToString(digest.Sum(nil))
}
