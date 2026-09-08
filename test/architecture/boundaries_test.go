package architecture_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestSkeletonEffectBoundary(t *testing.T) {
	root := filepath.Join("..", "..", "internal")
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		isCLI := strings.HasPrefix(path, filepath.Join(root, "cli")+string(filepath.Separator))
		for _, imp := range file.Imports {
			name, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				return err
			}
			importsEffectCapability := name == "os" || strings.HasPrefix(name, "os/") || name == "net" ||
				(strings.HasPrefix(name, "net/") && name != "net/url") ||
				name == "crypto/rand" || strings.HasPrefix(name, "math/rand") || name == "syscall" ||
				name == "unsafe" || strings.HasPrefix(name, "golang.org/x/sys")
			if importsEffectCapability {
				t.Errorf("%s imports effect capability %s", path, name)
			}
			if !isCLI && (strings.Contains(name, "/internal/cli") || strings.HasPrefix(name, "github.com/spf13/") || name == "io" || name == "fmt") {
				t.Errorf("%s depends on presentation %s", path, name)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
