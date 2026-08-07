package repocheck

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type ansibleArgvBlock struct {
	file string
	line int
	argv []string
}

func TestAnsibleBoundsCommandsThatCanHangForever(t *testing.T) {
	root := repoRoot(t)
	base := filepath.Join(root, "ansible/collections/ansible_collections/bootwright/core")
	var candidates int
	var offenders []string
	err := filepath.WalkDir(base, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || (filepath.Ext(path) != ".yml" && filepath.Ext(path) != ".yaml") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		for _, block := range ansibleArgvBlocks(rel, string(data)) {
			if !ansibleCommandCanHangForever(block.argv) {
				continue
			}
			candidates++
			if len(block.argv) == 0 || block.argv[0] != "timeout" {
				offenders = append(offenders, fmt.Sprintf("%s:%d runs `%s` unbounded", block.file, block.line, strings.Join(block.argv, " ")))
				continue
			}
			if !ansibleArgvBoundsItsChildren(block.argv) {
				offenders = append(offenders, fmt.Sprintf("%s:%d bounds `%s` without --kill-after", block.file, block.line, strings.Join(block.argv, " ")))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", base, err)
	}
	if candidates == 0 {
		t.Fatalf("no podman run or cephadm rm-cluster argv found under %s: the scanner no longer matches the tasks it guards", base)
	}
	if len(offenders) > 0 {
		t.Fatalf("every podman run and cephadm rm-cluster must run under timeout with a --kill-after escalation, or a wedged child keeps the command module's stdout pipe open and the play stalls forever with no banner and no result:\n%s", strings.Join(offenders, "\n"))
	}
}

func ansibleArgvBlocks(file, body string) []ansibleArgvBlock {
	lines := strings.Split(body, "\n")
	var blocks []ansibleArgvBlock
	for i, line := range lines {
		if strings.TrimSpace(line) != "argv:" {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		block := ansibleArgvBlock{file: file, line: i + 1}
		for _, item := range lines[i+1:] {
			trimmed := strings.TrimSpace(item)
			if !strings.HasPrefix(trimmed, "- ") {
				break
			}
			if len(item)-len(strings.TrimLeft(item, " ")) <= indent {
				break
			}
			block.argv = append(block.argv, strings.Trim(strings.TrimPrefix(trimmed, "- "), `"`))
		}
		blocks = append(blocks, block)
	}
	return blocks
}

func ansibleCommandCanHangForever(argv []string) bool {
	for i, arg := range argv {
		if arg == "rm-cluster" {
			return true
		}
		if arg == "podman" && i+1 < len(argv) && argv[i+1] == "run" {
			return true
		}
	}
	return false
}

func ansibleBoundedCommand(argv []string) []string {
	if len(argv) == 0 || argv[0] != "timeout" {
		return argv
	}
	rest := argv[1:]
	for len(rest) > 0 && strings.HasPrefix(rest[0], "-") {
		rest = rest[1:]
	}
	if len(rest) == 0 {
		return nil
	}
	return rest[1:]
}

func ansibleArgvBoundsItsChildren(argv []string) bool {
	for _, arg := range argv {
		if strings.HasPrefix(arg, "--kill-after") {
			return true
		}
	}
	return false
}
