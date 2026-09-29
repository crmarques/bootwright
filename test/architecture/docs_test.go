package architecture_test

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

// docsByteBudgets bounds the files an agent reads at the start of every task or
// every tracked edit. A budget more than a tenth above its file's size fails,
// so each one follows its file down; never raise one to admit growth that
// belongs in an on-demand page.
func docsByteBudgets() map[string]int {
	return map[string]int{
		"AGENTS.md":           2516,
		"CLAUDE.md":           12,
		"specs/index.md":      4656,
		"specs/milestones.md": 8192,
		".agents/skills/code-implementation/SKILL.md": 3072,
	}
}

// ignoredGuidancePaths are cited on purpose although Git ignores them. An entry
// fails once no guidance outside the ignored paths cites it.
var ignoredGuidancePaths = map[string]bool{
	"examples/wip/": true, // the local work area examples/.gitignore excludes
}

// repositoryPathPrefixes are the top-level directories whose backticked paths
// in guidance must name something that exists.
var repositoryPathPrefixes = []string{
	".agents/", ".claude/", ".github/", "ansible/", "api/", "cmd/", "docs/",
	"examples/", "internal/", "scripts/", "specs/", "test/",
}

type markdownFile struct {
	path  string
	lines []string
}

// guidanceFiles lists every tracked-looking Markdown file outside caches,
// build output and version-control metadata.
func guidanceFiles(t *testing.T) []markdownFile {
	t.Helper()
	root := filepath.Join("..", "..")
	var files []markdownFile
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := entry.Name()
		if entry.IsDir() {
			switch name {
			case ".git", ".cache", "bin", "output", "__pycache__", ".pytest_cache":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".md") {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files = append(files, markdownFile{path: filepath.ToSlash(relative), lines: strings.Split(string(data), "\n")})
		return nil
	})
	if err != nil {
		t.Fatalf("walk guidance: %v", err)
	}
	return files
}

// proseLines yields the lines outside fenced code blocks, numbered from one.
func proseLines(file markdownFile, visit func(number int, line string)) {
	fenced := false
	for index, line := range file.lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fenced = !fenced
			continue
		}
		if !fenced {
			visit(index+1, line)
		}
	}
}

var inlineCode = regexp.MustCompile("`[^`]*`")

// headingSlugs returns every anchor GitHub generates for the file's headings,
// including the numeric suffixes of repeated headings.
func headingSlugs(file markdownFile) map[string]bool {
	slugs := map[string]bool{}
	seen := map[string]int{}
	proseLines(file, func(_ int, line string) {
		if !strings.HasPrefix(line, "#") {
			return
		}
		text := strings.TrimSpace(strings.TrimLeft(line, "#"))
		slug := githubSlug(text)
		if count := seen[slug]; count > 0 {
			slugs[slug+"-"+strconv.Itoa(count)] = true
		} else {
			slugs[slug] = true
		}
		seen[slug]++
	})
	return slugs
}

func githubSlug(heading string) string {
	var slug strings.Builder
	for _, r := range strings.ToLower(heading) {
		switch {
		case unicode.IsLetter(r) || unicode.IsNumber(r) || r == '-' || r == '_':
			slug.WriteRune(r)
		case r == ' ':
			slug.WriteRune('-')
		}
	}
	return slug.String()
}

var markdownLink = regexp.MustCompile(`\]\(([^)\s]+)\)`)

func TestDocsLinksAndAnchorsResolve(t *testing.T) {
	files := guidanceFiles(t)
	byPath := map[string]markdownFile{}
	for _, file := range files {
		byPath[file.path] = file
	}
	slugs := map[string]map[string]bool{}
	anchorsOf := func(path string) map[string]bool {
		if found, ok := slugs[path]; ok {
			return found
		}
		slugs[path] = headingSlugs(byPath[path])
		return slugs[path]
	}
	root := filepath.Join("..", "..")
	for _, file := range files {
		proseLines(file, func(number int, line string) {
			for _, match := range markdownLink.FindAllStringSubmatch(inlineCode.ReplaceAllString(line, ""), -1) {
				target := match[1]
				if strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
					continue
				}
				path, anchor, _ := strings.Cut(target, "#")
				resolved := file.path
				if path != "" {
					resolved = filepath.ToSlash(filepath.Clean(filepath.Join(filepath.Dir(file.path), path)))
					if _, err := os.Stat(filepath.Join(root, resolved)); err != nil {
						t.Errorf("%s:%d links to missing %s", file.path, number, target)
						continue
					}
				}
				if anchor == "" || !strings.HasSuffix(resolved, ".md") {
					continue
				}
				if !anchorsOf(resolved)[anchor] {
					t.Errorf("%s:%d links to missing anchor %s", file.path, number, target)
				}
			}
		})
	}
}

var (
	citedPath         = regexp.MustCompile("`([^`\\s]+)`")
	goSymbolReference = regexp.MustCompile(`\.[A-Z][A-Za-z0-9]*$`)
)

func TestDocsRepositoryPathsExist(t *testing.T) {
	for _, violation := range citedPathViolations(filepath.Join("..", ".."), guidanceFiles(t), ignoredGuidancePaths) {
		t.Error(violation)
	}
}

// citedPathViolations reports each backticked repository path that names
// nothing under root, and each ignored path no file outside the ignored paths
// cites. Whether an ignored path exists never decides, since a fresh clone has
// none.
func citedPathViolations(root string, files []markdownFile, ignored map[string]bool) []string {
	var violations []string
	cited := map[string]bool{}
	for _, file := range files {
		inside := slices.ContainsFunc(sortedKeys(ignored), func(path string) bool { return strings.HasPrefix(file.path, path) })
		proseLines(file, func(number int, line string) {
			for _, match := range citedPath.FindAllStringSubmatch(line, -1) {
				token := strings.TrimRight(match[1], ".,;")
				if !hasRepositoryPrefix(token) || strings.ContainsAny(token, "<>*{}$[]|") {
					continue
				}
				if ignored[token] {
					cited[token] = cited[token] || !inside
					continue
				}
				path := token
				if index := strings.Index(path, ":"); index >= 0 {
					path = path[:index]
				}
				// A Go reference such as internal/cli.Confirmation names a
				// symbol of the package directory before the dot.
				if symbol := goSymbolReference.FindStringIndex(path); symbol != nil {
					path = path[:symbol[0]]
				}
				if _, err := os.Stat(filepath.Join(root, path)); err != nil {
					violations = append(violations, fmt.Sprintf("%s:%d cites missing path %s", file.path, number, token))
				}
			}
		})
	}
	for _, path := range sortedKeys(ignored) {
		if !cited[path] {
			violations = append(violations, fmt.Sprintf("ignoredGuidancePaths excuses %s, which no guidance outside it cites; remove it", path))
		}
	}
	return violations
}

func hasRepositoryPrefix(token string) bool {
	for _, prefix := range repositoryPathPrefixes {
		if strings.HasPrefix(token, prefix) {
			return true
		}
	}
	return false
}

var citedGoTest = regexp.MustCompile("`(Test[A-Z][A-Za-z0-9_]*)`")

func TestDocsCitedTestsExist(t *testing.T) {
	root := filepath.Join("..", "..")
	declared := map[string]bool{}
	declaration := regexp.MustCompile(`(?m)^func (Test[A-Za-z0-9_]+)\(`)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && (entry.Name() == ".git" || entry.Name() == ".cache") {
			return filepath.SkipDir
		}
		if entry.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, match := range declaration.FindAllStringSubmatch(string(data), -1) {
			declared[match[1]] = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk tests: %v", err)
	}
	for _, file := range guidanceFiles(t) {
		proseLines(file, func(number int, line string) {
			for _, match := range citedGoTest.FindAllStringSubmatch(line, -1) {
				if !declared[match[1]] {
					t.Errorf("%s:%d cites missing test %s", file.path, number, match[1])
				}
			}
		})
	}
}

// frontmatter returns the name and description of a skill file.
func frontmatter(t *testing.T, path string) (string, string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	if !scanner.Scan() || scanner.Text() != "---" {
		t.Fatalf("%s has no frontmatter", path)
	}
	var name, description string
	for scanner.Scan() {
		line := scanner.Text()
		if line == "---" {
			return name, description
		}
		if value, ok := strings.CutPrefix(line, "name: "); ok {
			name = strings.TrimSpace(value)
		}
		if value, ok := strings.CutPrefix(line, "description: "); ok {
			description = strings.TrimSpace(value)
		}
	}
	t.Fatalf("%s frontmatter is not closed", path)
	return "", ""
}

func TestDocsSkillsAreDiscoverable(t *testing.T) {
	root := filepath.Join("..", "..")
	canonical, err := filepath.Glob(filepath.Join(root, ".agents", "skills", "*", "SKILL.md"))
	if err != nil || len(canonical) == 0 {
		t.Fatalf("find skills: %v", err)
	}
	for _, path := range canonical {
		directory := filepath.Base(filepath.Dir(path))
		name, description := frontmatter(t, path)
		if name != directory || len(name) > 64 {
			t.Errorf("%s: name %q must equal its directory and stay within 64 characters", path, name)
		}
		if description == "" || len(description) > 1536 {
			t.Errorf("%s: description must be present and within 1,536 characters", path)
		}
		wrapper := filepath.Join(root, ".claude", "skills", directory, "SKILL.md")
		if _, err := os.Stat(wrapper); err != nil {
			continue
		}
		wrapperName, wrapperDescription := frontmatter(t, wrapper)
		if wrapperName != name || wrapperDescription != description {
			t.Errorf("%s: frontmatter differs from %s", wrapper, path)
		}
	}
}

func TestDocsExamplesAreIndexed(t *testing.T) {
	root := filepath.Join("..", "..")
	index, err := os.ReadFile(filepath.Join(root, "examples", "README.md"))
	if err != nil {
		t.Fatalf("read examples index: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "examples"))
	if err != nil {
		t.Fatalf("list examples: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() && !strings.Contains(string(index), entry.Name()+"/") {
			t.Errorf("examples/README.md does not index examples/%s", entry.Name())
		}
	}
}

func TestDocsStayWithinByteBudgets(t *testing.T) {
	root := filepath.Join("..", "..")
	rules, err := filepath.Glob(filepath.Join(root, ".claude", "rules", "*.md"))
	if err != nil {
		t.Fatalf("find rules: %v", err)
	}
	caps := map[string]int{}
	for _, rule := range rules {
		relative, _ := filepath.Rel(root, rule)
		caps[filepath.ToSlash(relative)] = 1024
	}
	for _, violation := range byteBudgetViolations(root, docsByteBudgets(), caps) {
		t.Error(violation)
	}
}

// byteBudgetViolations reports each file above its budget or cap, and each
// budget, never a cap, more than a tenth above its file's size. The largest
// budget a file of size bytes admits is size+size/9.
func byteBudgetViolations(root string, budgets, caps map[string]int) []string {
	var violations []string
	check := func(path string, limit int, ratcheted bool) {
		info, err := os.Stat(filepath.Join(root, path))
		if err != nil {
			violations = append(violations, fmt.Sprintf("budgeted file %s: %v", path, err))
			return
		}
		size := int(info.Size())
		if size > limit {
			violations = append(violations, fmt.Sprintf("%s is %d bytes, above its %d-byte budget; move detail to an on-demand page", path, size, limit))
		} else if ratcheted && limit-size > limit/10 {
			violations = append(violations, fmt.Sprintf("%s is %d bytes, more than a tenth below its %d-byte budget; lower the budget to %d", path, size, limit, size+size/9))
		}
	}
	for _, path := range sortedKeys(budgets) {
		check(path, budgets[path], true)
	}
	for _, path := range sortedKeys(caps) {
		check(path, caps[path], false)
	}
	return violations
}
