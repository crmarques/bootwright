package architecture_test

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

// docsByteBudgets bounds the files an agent reads at the start of every task or
// every tracked edit, each .claude/rules page among them. A budget more than a
// tenth above its file's size fails, so each one follows its file down; never
// raise one to admit growth that belongs in an on-demand page.
func docsByteBudgets() map[string]int {
	return map[string]int{
		"AGENTS.md":           2516,
		"CLAUDE.md":           12,
		"specs/index.md":      4656,
		"specs/milestones.md": 8192,
		".agents/skills/code-implementation/SKILL.md": 3072,
		".claude/rules/ansible.md":                    491,
		".claude/rules/go.md":                         564,
		".claude/rules/guidance.md":                   484,
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

// trackedFiles lists, relative to root, the files Git tracks there, so no
// untracked or ignored file of a checkout decides a docs verdict that CI
// reaches without it. The caller's GIT_ variables never redirect the listing.
func trackedFiles(root string) ([]string, error) {
	command := exec.Command("git", "ls-files", "-z")
	command.Dir = root
	command.Env = slices.DeleteFunc(os.Environ(), func(variable string) bool { return strings.HasPrefix(variable, "GIT_") })
	output, err := command.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return nil, fmt.Errorf("git ls-files: %w: %s", err, bytes.TrimSpace(exit.Stderr))
		}
		return nil, fmt.Errorf("git ls-files: %w", err)
	}
	var files []string
	for name := range strings.SplitSeq(string(output), "\x00") {
		if name != "" {
			files = append(files, name)
		}
	}
	return files, nil
}

// trackedPaths returns the tracked files together with every directory that
// holds one, each spelled as path.Clean spells it: the names a checkout without
// untracked or ignored files has.
func trackedPaths(files []string) map[string]bool {
	present := map[string]bool{}
	for _, name := range files {
		present[name] = true
		for directory := path.Dir(name); !present[directory]; directory = path.Dir(directory) {
			present[directory] = true
		}
	}
	return present
}

// trackedMarkdown reads each Markdown file Git tracks under root. A tracked
// file the working tree lacks fails until its removal is staged.
func trackedMarkdown(root string) ([]markdownFile, error) {
	names, err := trackedFiles(root)
	if err != nil {
		return nil, err
	}
	var files []markdownFile
	for _, name := range names {
		if !strings.HasSuffix(name, ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			return nil, err
		}
		files = append(files, markdownFile{path: name, lines: strings.Split(string(data), "\n")})
	}
	return files, nil
}

// repositoryFiles lists the files the repository tracks.
func repositoryFiles(t *testing.T) []string {
	t.Helper()
	files, err := trackedFiles(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("list tracked files: %v", err)
	}
	return files
}

// trackedGlob returns the tracked files the slash-separated pattern matches.
func trackedGlob(t *testing.T, pattern string) []string {
	t.Helper()
	var matches []string
	for _, name := range repositoryFiles(t) {
		matched, err := path.Match(pattern, name)
		if err != nil {
			t.Fatalf("match %s: %v", pattern, err)
		}
		if matched {
			matches = append(matches, name)
		}
	}
	return matches
}

// guidanceFiles reads every Markdown file the repository tracks.
func guidanceFiles(t *testing.T) []markdownFile {
	t.Helper()
	files, err := trackedMarkdown(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("read guidance: %v", err)
	}
	return files
}

func TestTrackedMarkdownSkipsUntrackedAndIgnoredFiles(t *testing.T) {
	root := t.TempDir()
	ratchetWrite(t, root, ".gitignore", "/wip/\n", 0o644)
	ratchetWrite(t, root, "README.md", "# committed\n", 0o644)
	ratchetWrite(t, root, "docs/guide.md", "# guide\n", 0o644)
	ratchetGit(t, root, "init", "-q", "-b", "main")
	ratchetGit(t, root, "add", ".")
	ratchetGit(t, root, "commit", "-q", "-m", "base")
	ratchetWrite(t, root, "README.md", "# edited\n", 0o644)
	ratchetWrite(t, root, "staged.md", "# staged\n", 0o644)
	ratchetGit(t, root, "add", "staged.md")
	ratchetWrite(t, root, "notes.md", "# untracked\n", 0o644)
	ratchetWrite(t, root, "wip/draft.md", "# ignored\n", 0o644)
	t.Setenv("GIT_INDEX_FILE", filepath.Join(t.TempDir(), "index"))

	read := func() []string {
		t.Helper()
		files, err := trackedMarkdown(root)
		if err != nil {
			t.Fatalf("read tracked Markdown: %v", err)
		}
		var headings []string
		for _, file := range files {
			headings = append(headings, file.path+": "+file.lines[0])
		}
		return headings
	}
	if got, want := read(), []string{"README.md: # edited", "docs/guide.md: # guide", "staged.md: # staged"}; !slices.Equal(got, want) {
		t.Fatalf("read %q, want %q", got, want)
	}

	if err := os.Remove(filepath.Join(root, "docs", "guide.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := trackedMarkdown(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a tracked file the working tree lacks read as %v, want it to fail", err)
	}
	ratchetGit(t, root, "rm", "-q", "docs/guide.md")
	if got, want := read(), []string{"README.md: # edited", "staged.md: # staged"}; !slices.Equal(got, want) {
		t.Fatalf("after staging the removal, read %q, want %q", got, want)
	}
}

func TestDocsLinksAndCitedPathsNameOnlyTrackedFiles(t *testing.T) {
	root := t.TempDir()
	ratchetWrite(t, root, ".gitignore", "/examples/wip/\n", 0o644)
	ratchetWrite(t, root, "README.md", "# Fixture\n\n"+
		"See [the guide](docs/guide.md#guide), [the docs](docs/), [notes](docs/notes.md) and [a draft](examples/wip/draft.md).\n"+
		"Cite `docs/guide.md:1`, `docs/`, `docs/notes.md` and `examples/wip/draft.md`.\n", 0o644)
	ratchetWrite(t, root, "docs/guide.md", "# Guide\n", 0o644)
	ratchetGit(t, root, "init", "-q", "-b", "main")
	ratchetGit(t, root, "add", ".")
	ratchetGit(t, root, "commit", "-q", "-m", "base")
	ratchetWrite(t, root, "docs/notes.md", "# untracked\n", 0o644)
	ratchetWrite(t, root, "examples/wip/draft.md", "# ignored\n", 0o644)

	files, err := trackedMarkdown(root)
	if err != nil {
		t.Fatalf("read tracked Markdown: %v", err)
	}
	names, err := trackedFiles(root)
	if err != nil {
		t.Fatalf("list tracked files: %v", err)
	}
	present := trackedPaths(names)
	if got, want := linkViolations(files, present), []string{
		"README.md:3 links to missing docs/notes.md",
		"README.md:3 links to missing examples/wip/draft.md",
	}; !slices.Equal(got, want) {
		t.Fatalf("link violations = %q, want %q", got, want)
	}
	if got, want := citedPathViolations(present, files, nil), []string{
		"README.md:4 cites missing path docs/notes.md",
		"README.md:4 cites missing path examples/wip/draft.md",
	}; !slices.Equal(got, want) {
		t.Fatalf("cited path violations = %q, want %q", got, want)
	}
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
	for _, violation := range linkViolations(guidanceFiles(t), trackedPaths(repositoryFiles(t))) {
		t.Error(violation)
	}
}

// linkViolations reports each relative link to a name present lacks and each
// anchor its Markdown target lacks.
func linkViolations(files []markdownFile, present map[string]bool) []string {
	byPath := map[string]markdownFile{}
	for _, file := range files {
		byPath[file.path] = file
	}
	slugs := map[string]map[string]bool{}
	anchorsOf := func(name string) map[string]bool {
		if found, ok := slugs[name]; ok {
			return found
		}
		slugs[name] = headingSlugs(byPath[name])
		return slugs[name]
	}
	var violations []string
	for _, file := range files {
		proseLines(file, func(number int, line string) {
			for _, match := range markdownLink.FindAllStringSubmatch(inlineCode.ReplaceAllString(line, ""), -1) {
				target := match[1]
				if strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
					continue
				}
				linked, anchor, _ := strings.Cut(target, "#")
				resolved := file.path
				if linked != "" {
					resolved = path.Join(path.Dir(file.path), linked)
					if !present[resolved] {
						violations = append(violations, fmt.Sprintf("%s:%d links to missing %s", file.path, number, target))
						continue
					}
				}
				if anchor == "" || !strings.HasSuffix(resolved, ".md") {
					continue
				}
				if !anchorsOf(resolved)[anchor] {
					violations = append(violations, fmt.Sprintf("%s:%d links to missing anchor %s", file.path, number, target))
				}
			}
		})
	}
	return violations
}

var (
	citedPath         = regexp.MustCompile("`([^`\\s]+)`")
	goSymbolReference = regexp.MustCompile(`\.[A-Z][A-Za-z0-9]*$`)
)

func TestDocsRepositoryPathsExist(t *testing.T) {
	for _, violation := range citedPathViolations(trackedPaths(repositoryFiles(t)), guidanceFiles(t), ignoredGuidancePaths) {
		t.Error(violation)
	}
}

// citedPathViolations reports each backticked repository path that names
// nothing present holds, and each ignored path no file outside the ignored
// paths cites. Whether an ignored path exists never decides, since a fresh
// clone has none.
func citedPathViolations(present map[string]bool, files []markdownFile, ignored map[string]bool) []string {
	var violations []string
	cited := map[string]bool{}
	for _, file := range files {
		inside := slices.ContainsFunc(sortedKeys(ignored), func(prefix string) bool { return strings.HasPrefix(file.path, prefix) })
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
				name := token
				if index := strings.Index(name, ":"); index >= 0 {
					name = name[:index]
				}
				// A Go reference such as internal/cli.Confirmation names a
				// symbol of the package directory before the dot.
				if symbol := goSymbolReference.FindStringIndex(name); symbol != nil {
					name = name[:symbol[0]]
				}
				if !present[path.Clean(name)] {
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
	for _, name := range repositoryFiles(t) {
		if !strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			t.Fatalf("read tests: %v", err)
		}
		for _, match := range declaration.FindAllStringSubmatch(string(data), -1) {
			declared[match[1]] = true
		}
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
	canonical := trackedGlob(t, ".agents/skills/*/SKILL.md")
	if len(canonical) == 0 {
		t.Fatal("find skills: the repository tracks none")
	}
	wrappers := trackedGlob(t, ".claude/skills/*/SKILL.md")
	for _, path := range canonical {
		directory := filepath.Base(filepath.Dir(path))
		name, description := frontmatter(t, filepath.Join(root, path))
		if name != directory || len(name) > 64 {
			t.Errorf("%s: name %q must equal its directory and stay within 64 characters", path, name)
		}
		if description == "" || len(description) > 1536 {
			t.Errorf("%s: description must be present and within 1,536 characters", path)
		}
		wrapper := ".claude/skills/" + directory + "/SKILL.md"
		if !slices.Contains(wrappers, wrapper) {
			continue
		}
		wrapperName, wrapperDescription := frontmatter(t, filepath.Join(root, wrapper))
		if wrapperName != name || wrapperDescription != description {
			t.Errorf("%s: frontmatter differs from %s", wrapper, path)
		}
	}
}

func TestDocsExamplesAreIndexed(t *testing.T) {
	root := filepath.Join("..", "..")
	tracked := repositoryFiles(t)
	if !slices.Contains(tracked, "examples/README.md") {
		t.Fatal("read examples index: the repository does not track examples/README.md")
	}
	index, err := os.ReadFile(filepath.Join(root, "examples", "README.md"))
	if err != nil {
		t.Fatalf("read examples index: %v", err)
	}
	examples := map[string]bool{}
	for _, name := range tracked {
		if rest, ok := strings.CutPrefix(name, "examples/"); ok {
			if example, _, nested := strings.Cut(rest, "/"); nested {
				examples[example] = true
			}
		}
	}
	for _, example := range sortedKeys(examples) {
		if !strings.Contains(string(index), example+"/") {
			t.Errorf("examples/README.md does not index examples/%s", example)
		}
	}
}

func TestDocsStayWithinByteBudgets(t *testing.T) {
	root := filepath.Join("..", "..")
	for _, violation := range byteBudgetViolations(root, repositoryFiles(t), docsByteBudgets(), trackedGlob(t, ".claude/rules/*.md")) {
		t.Error(violation)
	}
}

// byteBudgetViolations reports each budgeted file outside tracked, each file
// above its budget, each budget more than a tenth above its file's size, and
// each required file without a budget. The largest budget a file of size bytes
// admits is size+size/9.
func byteBudgetViolations(root string, tracked []string, budgets map[string]int, required []string) []string {
	var violations []string
	check := func(path string, limit int) {
		if !slices.Contains(tracked, path) {
			violations = append(violations, fmt.Sprintf("budgeted file %s is not tracked", path))
			return
		}
		info, err := os.Stat(filepath.Join(root, path))
		if err != nil {
			violations = append(violations, fmt.Sprintf("budgeted file %s: %v", path, err))
			return
		}
		size := int(info.Size())
		if size > limit {
			violations = append(violations, fmt.Sprintf("%s is %d bytes, above its %d-byte budget; move detail to an on-demand page", path, size, limit))
		} else if limit-size > limit/10 {
			violations = append(violations, fmt.Sprintf("%s is %d bytes, more than a tenth below its %d-byte budget; lower the budget to %d", path, size, limit, size+size/9))
		}
	}
	for _, path := range sortedKeys(budgets) {
		check(path, budgets[path])
	}
	for _, path := range required {
		if _, budgeted := budgets[path]; !budgeted {
			violations = append(violations, fmt.Sprintf("%s has no byte budget; add one to docsByteBudgets", path))
		}
	}
	return violations
}
