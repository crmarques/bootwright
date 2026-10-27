package architecture_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
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
		"specs/index.md":      4008,
		"specs/milestones.md": 8192,
		".agents/skills/code-implementation/SKILL.md": 3072,
		".claude/rules/ansible.md":                    440,
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

// datedRecords holds reports that cite the repository at the commit they
// audited, so links and paths they name may no longer resolve.
const datedRecords = "audits/"

// guidanceFiles reads every Markdown file the repository tracks outside the
// dated records.
func guidanceFiles(t *testing.T) []markdownFile {
	t.Helper()
	files, err := trackedMarkdown(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("read guidance: %v", err)
	}
	return slices.DeleteFunc(files, func(file markdownFile) bool { return strings.HasPrefix(file.path, datedRecords) })
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
	for _, violation := range exampleIndexViolations(tracked, string(index)) {
		t.Error(violation)
	}
}

func exampleIndexViolations(tracked []string, index string) []string {
	var violations []string
	scenarios := map[string]bool{}
	for _, name := range tracked {
		rest, ok := strings.CutPrefix(name, "examples/")
		if !ok {
			continue
		}
		if rest == ".gitignore" || rest == ".yamllint" || rest == "README.md" {
			continue
		}
		parts := strings.Split(rest, "/")
		if len(parts) < 3 {
			violations = append(violations, fmt.Sprintf("examples/%s must be under a category and scenario directory", rest))
			continue
		}
		scenarios[parts[0]+"/"+parts[1]] = true
	}
	for _, scenario := range sortedKeys(scenarios) {
		for _, required := range []string{"environment.yaml", "README.md"} {
			if !slices.Contains(tracked, "examples/"+scenario+"/"+required) {
				violations = append(violations, fmt.Sprintf("examples/%s does not track %s", scenario, required))
			}
		}
		if !strings.Contains(index, scenario+"/") {
			violations = append(violations, fmt.Sprintf("examples/README.md does not index examples/%s", scenario))
		}
	}
	return violations
}

func TestExampleIndexViolations(t *testing.T) {
	complete := []string{"examples/.gitignore", "examples/.yamllint", "examples/README.md",
		"examples/cat/one/environment.yaml", "examples/cat/one/README.md", "examples/cat/one/inputs/a.yaml"}
	cases := []struct {
		name    string
		tracked []string
		index   string
		want    []string
	}{
		{"complete and indexed", complete, "cat/one/", nil},
		{"unindexed", complete, "nothing", []string{"examples/README.md does not index examples/cat/one"}},
		{"environment.yml only", []string{"examples/cat/one/environment.yml", "examples/cat/one/README.md"}, "cat/one/",
			[]string{"examples/cat/one does not track environment.yaml"}},
		{"README only", []string{"examples/cat/one/README.md"}, "cat/one/",
			[]string{"examples/cat/one does not track environment.yaml"}},
		{"no README", []string{"examples/cat/one/environment.yaml"}, "cat/one/",
			[]string{"examples/cat/one does not track README.md"}},
		{"too shallow", append([]string{"examples/cat/x.yaml"}, complete...), "cat/one/",
			[]string{"examples/cat/x.yaml must be under a category and scenario directory"}},
	}
	for _, tc := range cases {
		got := exampleIndexViolations(tc.tracked, tc.index)
		if !slices.Equal(got, tc.want) {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
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

var (
	notYetMetLink    = regexp.MustCompile(`\[(B\d+)\]\(([^)\s]+)\)`)
	notYetMetPage    = regexp.MustCompile(`^specs/milestones/(m[1-9][0-9]*|backlog)\.md$`)
	notYetMetRowHead = regexp.MustCompile(`^\| \[(B\d+)\]\(#b\d+\) \|`)
)

func TestDocsNotYetMetNamesAnUndeliveredItem(t *testing.T) {
	for _, violation := range notYetMetViolations(guidanceFiles(t)) {
		t.Error(violation)
	}
}

func TestDocsNotYetMetMatcherRejectsBadParagraphs(t *testing.T) {
	pages := markdownFile{path: "specs/milestones/m2.md", lines: strings.Split(
		"# M2\n\n| ID | Alias | Kind | Owner | Outcome | Requires | Definition | Delivery |\n| --- | --- | --- | --- | --- | --- | --- | --- |\n"+
			"| [B1](#b1) | | product | x | y | | Specified | not started |\n"+
			"| [B2](#b2) | | product | x | y | | Specified | completed |\n"+
			"| [B4](#b4) | | product | x | y | | Specified | not started |\n\n### B1\n\n### B2\n", "\n")}
	backlog := markdownFile{path: "specs/milestones/backlog.md", lines: strings.Split(
		"# Backlog\n\n| ID | Alias |\n| --- | --- |\n| [B3](#b3) | x |\n\n### B3\n", "\n")}
	spec := func(body string) markdownFile {
		return markdownFile{path: "specs/api/x.md", lines: strings.Split(body, "\n")}
	}
	cases := []struct {
		name string
		body string
		want []string
	}{
		{"good m2 link", "Not yet met: a; tracked as [B1](../milestones/m2.md#b1).", nil},
		{"good backlog link", "Not yet met: a; tracked as\n[B3](../milestones/backlog.md#b3).", nil},
		{"two paragraphs split at the next marker", "Not yet met: a; [B1](../milestones/m2.md#b1).\nNot yet met: b; [B3](../milestones/backlog.md#b3).", nil},
		{"no link", "Not yet met: a; tracked as B1.", []string{"specs/api/x.md:1 Not yet met paragraph links no B<n> item"}},
		{"second marker without link", "Not yet met: a; [B1](../milestones/m2.md#b1).\nNot yet met: b.", []string{"specs/api/x.md:2 Not yet met paragraph links no B<n> item"}},
		{"delivered record", "Not yet met: a; [B1](../milestones/delivered.md#b1).", []string{"specs/api/x.md:1 B1 links ../milestones/delivered.md#b1, not a milestone page or the backlog at #b1"}},
		{"wrong anchor", "Not yet met: a; [B1](../milestones/m2.md#b2).", []string{"specs/api/x.md:1 B1 links ../milestones/m2.md#b2, not a milestone page or the backlog at #b1"}},
		{"target does not resolve", "Not yet met: a; [B1](milestones/m2.md#b1).", []string{"specs/api/x.md:1 B1 links milestones/m2.md#b1, not a milestone page or the backlog at #b1"}},
		{"item on another page", "Not yet met: a; [B3](../milestones/m2.md#b3).", []string{"specs/api/x.md:1 B3 has no heading on specs/milestones/m2.md", "specs/api/x.md:1 B3 has no row on specs/milestones/m2.md"}},
		{"delivered row", "Not yet met: a; [B2](../milestones/m2.md#b2).", []string{"specs/api/x.md:1 B2 is delivered on specs/milestones/m2.md"}},
		{"row without heading", "Not yet met: a; [B4](../milestones/m2.md#b4).", []string{"specs/api/x.md:1 B4 has no heading on specs/milestones/m2.md"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got := notYetMetViolations([]markdownFile{pages, backlog, spec(test.body)})
			if !slices.Equal(got, test.want) {
				t.Fatalf("violations = %q, want %q", got, test.want)
			}
		})
	}
	for _, name := range []string{"specs/milestones/m2.md", "specs/deferred/x.md"} {
		skipped := markdownFile{path: name, lines: []string{"Not yet met: a."}}
		if got := notYetMetViolations([]markdownFile{skipped}); got != nil {
			t.Fatalf("%s scanned: %q", name, got)
		}
	}
}

// notYetMetViolations reports each "Not yet met:" paragraph of a spec outside
// specs/milestones and specs/deferred that links no item, and each linked item
// that is not an undelivered one on its milestone page or the backlog.
func notYetMetViolations(files []markdownFile) []string {
	byPath := map[string]markdownFile{}
	for _, file := range files {
		byPath[file.path] = file
	}
	var violations []string
	for _, file := range files {
		if !strings.HasPrefix(file.path, "specs/") ||
			strings.HasPrefix(file.path, "specs/milestones/") || strings.HasPrefix(file.path, "specs/deferred/") {
			continue
		}
		start, paragraph := 0, []string(nil)
		flush := func() {
			if start != 0 {
				violations = append(violations, notYetMetParagraph(byPath, file.path, start, strings.Join(paragraph, " "))...)
			}
			start, paragraph = 0, nil
		}
		proseLines(file, func(number int, line string) {
			switch {
			case strings.HasPrefix(strings.TrimSpace(line), "Not yet met:"):
				flush()
				start, paragraph = number, []string{line}
			case strings.TrimSpace(line) == "":
				flush()
			case start != 0:
				paragraph = append(paragraph, line)
			}
		})
		flush()
	}
	return violations
}

func notYetMetParagraph(pages map[string]markdownFile, from string, line int, text string) []string {
	links := notYetMetLink.FindAllStringSubmatch(inlineCode.ReplaceAllString(text, ""), -1)
	if len(links) == 0 {
		return []string{fmt.Sprintf("%s:%d Not yet met paragraph links no B<n> item", from, line)}
	}
	var violations []string
	for _, link := range links {
		item, target := link[1], link[2]
		linked, anchor, _ := strings.Cut(target, "#")
		resolved := path.Join(path.Dir(from), linked)
		if !notYetMetPage.MatchString(resolved) || anchor != strings.ToLower(item) {
			violations = append(violations, fmt.Sprintf("%s:%d %s links %s, not a milestone page or the backlog at #%s", from, line, item, target, strings.ToLower(item)))
			continue
		}
		heading, row, delivered := false, false, false
		proseLines(pages[resolved], func(_ int, text string) {
			if strings.TrimRight(text, " ") == "### "+item {
				heading = true
			}
			if match := notYetMetRowHead.FindStringSubmatch(text); match != nil && match[1] == item {
				row = true
				cells := strings.Split(strings.TrimSuffix(strings.TrimSpace(text), "|"), "|")
				delivered = strings.HasPrefix(strings.TrimSpace(cells[len(cells)-1]), "completed")
			}
		})
		if !heading {
			violations = append(violations, fmt.Sprintf("%s:%d %s has no heading on %s", from, line, item, resolved))
		}
		if !row {
			violations = append(violations, fmt.Sprintf("%s:%d %s has no row on %s", from, line, item, resolved))
		}
		if delivered {
			violations = append(violations, fmt.Sprintf("%s:%d %s is delivered on %s", from, line, item, resolved))
		}
	}
	return violations
}

// askRuleMarkers are the phrases of a catalog Effects cell that name a remote
// or host effect, or a command that reveals a secret: the cells of the
// "Mutate" class that specs/cli/commands.md defines, never "Local" or
// "Observe" ones. A marker with a flag fires only for the invocation that
// carries the flag, because the command reveals the secret by that flag alone.
var askRuleMarkers = []struct{ phrase, flag string }{
	{phrase: "planned mutation"},
	{phrase: "planned removal"},
	{phrase: "host packages"},
	{phrase: "download"},
	{phrase: "ssh"},
	{phrase: "power operation"},
	{phrase: "host identity rewrite"},
	{phrase: "reveal"},
	{phrase: "confidential material", flag: "--secrets"},
	{phrase: "sensitive files", flag: "--sensitive"},
	{phrase: "local artifact writes", flag: "--sensitive"},
}

// askRuleNoAsk classifies each catalog row that has no marker: the command,
// and the phrase of its Effects cell that makes it a Local or Observe command
// (none, read local state, local writes of its own context, bounded observation)
// that needs no ask rule. A row whose Effects cell gains a marker, or loses the
// phrase, fails until it is classified again.
var askRuleNoAsk = map[string]string{
	"context init": "local", "context update": "local", "context use": "local",
	"context list": "read local state", "context current": "read local state",
	"context delete": "never resource mutation", "add-ons list": "read executable registry",
	"secret set": "local confidential write", "secret generate": "generated-material batch",
	"secret check": "no values emitted", "secret list": "read confidential-store metadata",
	"secret info": "read confidential-store metadata only", "secret delete": "local logical deletion",
	"secret encryption init": "idempotent initialization", "secret encryption status": "read confidential metadata",
	"secret encryption rotate": "local key rotation", "media list": "read local media",
	"media delete": "local media deletion", "validate": "no write",
	"preflight controller": "bounded local observation", "preflight infra": "observe unless",
	"preflight clusters": "observe unless", "preflight container-cluster": "observe unless",
	"preflight storage-cluster": "observe unless", "preflight add-ons": "bounded observation",
	"preflight all": "observe unless", "plan": "none", "status": "read local state",
	"diff": "read local state", "render effective": "none", "render storage": "never execution",
	"machine list": "read local state", "machine info": "read local inventory",
	"cluster list": "read local state", "cluster oc": "read context-owned access metadata",
	"cluster kubectl": "read context-owned access metadata", "version": "none", "help": "none",
	"schema": "none", "completion bash": "none", "completion zsh": "none", "completion fish": "none",
	"completion powershell": "none",
}

// askRuleInvocations are the shapes in which an agent may start a command:
// bare, through the repository binary by a relative or an absolute path, and
// with global flags between the binary and the command.
var askRuleInvocations = []string{
	"bootwright %s",
	"./bin/bootwright %s",
	"/home/user/projects/bootwright/bin/bootwright %s",
	"bin/bootwright --context c -o json %s",
}

// catalogCommand is one command row of the catalog: its local flags and its
// Effects cell with a "same" cell replaced by the cell it repeats.
type catalogCommand struct{ flags, effects string }

// catalogCommands reads the Effects column of specs/cli/commands.md.
func catalogCommands(t *testing.T) map[string]catalogCommand {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "specs", "cli", "commands.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, section, found := strings.Cut(string(data), "\n## Command and flag catalog\n")
	if !found {
		t.Fatal("specs/cli/commands.md has no command and flag catalog")
	}
	section, _, _ = strings.Cut(section, "\n## ")
	commands := map[string]catalogCommand{}
	previous := ""
	var order []string
	for line := range strings.SplitSeq(section, "\n") {
		if !strings.HasPrefix(line, "| `bootwright ") {
			continue
		}
		cells := strings.Split(strings.Trim(line, "| "), " | ")
		if len(cells) != 4 {
			t.Fatalf("specs/cli/commands.md row has %d cells, not 4: %s", len(cells), line)
		}
		var words []string
		for _, word := range strings.Fields(strings.Trim(cells[0], "`"))[1:] {
			if strings.HasPrefix(word, "[") || strings.HasPrefix(word, "<") {
				break
			}
			words = append(words, word)
		}
		name := strings.Join(words, " ")
		effects := cells[3]
		if effects == "same" {
			effects = commands[previous].effects
		}
		commands[name] = catalogCommand{flags: cells[1], effects: effects}
		order = append(order, name)
		previous = name
	}
	for _, name := range order {
		other, repeats := strings.CutPrefix(commands[name].effects, "same as `")
		if repeats {
			other, _, _ = strings.Cut(other, "`")
			commands[name] = catalogCommand{flags: commands[name].flags, effects: commands[other].effects}
		}
	}
	return commands
}

// askRuleViolations compares the catalog with the ask patterns of
// .claude/settings.json: every command whose Effects cell carries a marker has
// a pattern that matches its invocation, and every pattern naming a Bootwright
// command names one the catalog lists, with only flags that command accepts.
func askRuleViolations(commands map[string]catalogCommand, patterns []string, noAsk map[string]string) []string {
	var violations []string
	bodies := map[string]*regexp.Regexp{}
	for _, pattern := range patterns {
		body, isBash := strings.CutPrefix(pattern, "Bash(")
		body = strings.TrimSuffix(body, ")")
		if !isBash {
			continue
		}
		var quoted []string
		for part := range strings.SplitSeq(body, "*") {
			quoted = append(quoted, regexp.QuoteMeta(part))
		}
		bodies[pattern] = regexp.MustCompile("^" + strings.Join(quoted, ".*") + "$")
		_, after, named := strings.Cut(body, "bootwright")
		if !named {
			continue
		}
		var parts []string
		for part := range strings.SplitSeq(after, "*") {
			if part = strings.TrimSpace(part); part != "" {
				parts = append(parts, part)
			}
		}
		name := ""
		if len(parts) > 0 {
			name = parts[0]
		}
		command, listed := commands[name]
		if !listed || name == "" {
			violations = append(violations, fmt.Sprintf("the ask pattern %q names bootwright %q, which specs/cli/commands.md does not list", pattern, name))
			continue
		}
		for _, flag := range parts[1:] {
			if !strings.Contains(command.flags, "`"+flag) {
				violations = append(violations, fmt.Sprintf("the ask pattern %q names %s, which bootwright %s does not list", pattern, flag, name))
			}
		}
	}
	matches := func(invocation string) bool {
		for _, body := range bodies {
			if body.MatchString(invocation) {
				return true
			}
		}
		return false
	}
	for _, name := range sortedKeys(commands) {
		effects := strings.ToLower(commands[name].effects)
		marked := false
		for _, marker := range askRuleMarkers {
			if !strings.Contains(effects, marker.phrase) {
				continue
			}
			if marker.flag != "" && !strings.Contains(commands[name].flags, "`"+marker.flag+"`") {
				violations = append(violations, fmt.Sprintf("bootwright %s has %q effects but no %s flag; update askRuleMarkers", name, marker.phrase, marker.flag))
				continue
			}
			marked = true
			forms := []string{name}
			if marker.flag != "" {
				forms = []string{name + " " + marker.flag, name + " " + marker.flag + "=true", name + " --name x " + marker.flag}
			}
			for _, form := range forms {
				for _, shape := range askRuleInvocations {
					if invocation := fmt.Sprintf(shape, form); !matches(invocation) {
						violations = append(violations, fmt.Sprintf("bootwright %s has %q effects, but no ask pattern in .claude/settings.json matches %q", name, marker.phrase, invocation))
					}
				}
			}
			break
		}
		phrase, classified := noAsk[name]
		switch {
		case marked && classified:
			violations = append(violations, fmt.Sprintf("bootwright %s has marked effects and is also in askRuleNoAsk; remove it there", name))
		case !marked && !classified:
			violations = append(violations, fmt.Sprintf("bootwright %s has effects %q that match no ask marker and are not in askRuleNoAsk; add a marker and an ask pattern, or classify it as needing no ask", name, commands[name].effects))
		case classified && !strings.Contains(effects, phrase):
			violations = append(violations, fmt.Sprintf("askRuleNoAsk classifies bootwright %s by %q, which its effects %q no longer say; classify it again", name, phrase, commands[name].effects))
		}
	}
	for _, name := range slices.Sorted(maps.Keys(noAsk)) {
		if _, listed := commands[name]; !listed {
			violations = append(violations, fmt.Sprintf("askRuleNoAsk names bootwright %q, which specs/cli/commands.md does not list", name))
		}
	}
	return violations
}

// TestDocsAskRulesCoverEveryEffectfulCommand keeps the tracked Claude Code ask
// list complete: AGENTS.md forbids an agent to run a command with effects or
// one that reveals a secret on its own, and specs/cli/commands.md owns which
// commands those are.
func TestDocsAskRulesCoverEveryEffectfulCommand(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", ".claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var settings struct {
		Permissions struct {
			Ask []string `json:"ask"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf(".claude/settings.json: %v", err)
	}
	commands := catalogCommands(t)
	if len(commands) < 50 || len(settings.Permissions.Ask) == 0 {
		t.Fatalf("read %d catalog rows and %d ask patterns; the files' shape has changed", len(commands), len(settings.Permissions.Ask))
	}
	for _, violation := range askRuleViolations(commands, settings.Permissions.Ask, askRuleNoAsk) {
		t.Error(violation)
	}
}

func TestDocsAskRuleViolations(t *testing.T) {
	commands := map[string]catalogCommand{
		"apply":            {flags: "`--yes` false", effects: "complete planned mutation"},
		"destroy":          {flags: "`--yes` false", effects: "complete planned removal"},
		"machine exec":     {flags: "required `--name <list>`", effects: "run one pinned SSH client"},
		"cluster info":     {flags: "`--secrets` false", effects: "read local state and optional confidential material"},
		"render":           {flags: "`--sensitive` false", effects: "local artifact writes only"},
		"render installer": {flags: "`--sensitive` false", effects: "local placeholder files and optional sensitive files"},
		"status":           {flags: "none", effects: "read local state"},
	}
	noAsk := map[string]string{"status": "read local state"}
	asks := []string{"Bash(git push *)", "Bash(*bootwright *apply*)", "Bash(*bootwright *destroy*)", "Bash(*bootwright *machine exec*)", "Bash(*bootwright *cluster info*--secrets*)", "Bash(*bootwright *render*--sensitive*)"}
	if got := askRuleViolations(commands, asks, noAsk); len(got) != 0 {
		t.Fatalf("a complete ask list has violations: %v", got)
	}
	without := func(part string) []string {
		return slices.DeleteFunc(slices.Clone(asks), func(p string) bool { return strings.Contains(p, part) })
	}
	for name, test := range map[string]struct {
		asks []string
		want string
	}{
		"destroy pattern removed": {without("destroy"), `bootwright destroy has "planned removal" effects, but no ask pattern`},
		"secrets pattern removed": {without("--secrets"), `bootwright cluster info has "confidential material" effects`},
		"sensitive pattern gone":  {without("--sensitive"), `bootwright render installer has "sensitive files" effects`},
		"secrets flag only":       {append(without("--secrets"), "Bash(*bootwright *cluster info*--secrets)"), `"bootwright cluster info --secrets=true"`},
		"leading binary only":     {append(without("destroy"), "Bash(bootwright destroy*)"), `"./bin/bootwright destroy"`},
		"no global flags":         {append(without("destroy"), "Bash(*bootwright destroy*)"), `"bin/bootwright --context c -o json destroy"`},
		"orphan pattern":          {append(slices.Clone(asks), "Bash(*bootwright *machine wipe*)"), `names bootwright "machine wipe", which specs/cli/commands.md does not list`},
		"orphan without a space":  {append(slices.Clone(asks), "Bash(*bootwright machine wipe*)"), `names bootwright "machine wipe", which specs/cli/commands.md does not list`},
		"unknown flag":            {append(slices.Clone(asks), "Bash(*bootwright *apply*--force*)"), "names --force, which bootwright apply does not list"},
	} {
		got := askRuleViolations(commands, test.asks, noAsk)
		if len(got) == 0 || !strings.Contains(strings.Join(got, "\n"), test.want) {
			t.Errorf("%s: violations %v, want %q", name, got, test.want)
		}
	}
	added := maps.Clone(commands)
	added["machine wipe"] = catalogCommand{flags: "none", effects: "bounded power operation"}
	if got := askRuleViolations(added, asks, noAsk); len(got) != len(askRuleInvocations) || !strings.Contains(got[0], "bootwright machine wipe") {
		t.Errorf("a new effectful command without a pattern gave %v", got)
	}
	unclassified := maps.Clone(commands)
	unclassified["machine wipe"] = catalogCommand{flags: "none", effects: "rewrite the firmware"}
	if got := askRuleViolations(unclassified, asks, noAsk); len(got) != 1 || !strings.Contains(got[0], "bootwright machine wipe has effects") || !strings.Contains(got[0], "not in askRuleNoAsk") {
		t.Errorf("a command whose effects match no marker and no classification gave %v", got)
	}
	for name, test := range map[string]struct {
		noAsk map[string]string
		want  string
	}{
		"classified and marked": {map[string]string{"status": "read local state", "apply": "mutation"}, "bootwright apply has marked effects and is also in askRuleNoAsk"},
		"phrase gone":           {map[string]string{"status": "no write"}, `classifies bootwright status by "no write"`},
		"stale entry":           {map[string]string{"status": "read local state", "gone": "none"}, `askRuleNoAsk names bootwright "gone"`},
	} {
		if got := askRuleViolations(commands, asks, test.noAsk); len(got) != 1 || !strings.Contains(got[0], test.want) {
			t.Errorf("%s: violations %v, want %q", name, got, test.want)
		}
	}
}

// vendorSupportPhrase is what a spec must not say: a coverage row means that
// Bootwright realizes a capability, never that a vendor supports it (D157,
// D159 clause 15, D163).
var vendorSupportPhrase = regexp.MustCompile(`(?i)\b(?:IBM|vendor|Red Hat|provider)[- ]supported\b|\bsupported by (?:IBM|Red Hat|the vendor)\b|release-qualified|support status|Release support`)

// vendorSupportRecords may name a vendor support status: the delivery records
// and the decision-history rows, which state what was decided and when.
var (
	vendorSupportRecords = map[string]bool{"specs/milestones/backlog.md": true, "specs/milestones/delivered.md": true}
	decisionHistoryRow   = regexp.MustCompile(`^\| (?:OD|D)\d+ \|`)
)

// vendorSupportNegations are the sentences that say Bootwright records no
// vendor support status. An entry fails once the sentence it names is gone.
var vendorSupportNegations = []struct{ file, sentence string }{
	{"specs/cli/output.md", "vendor release or support status"},
	{"specs/milestones/m1.md", "records no vendor FIPS support status"},
}

// vendorSupportViolations lists each use of a vendor-support phrase in the
// specs outside the delivery records, the decision-history rows and the named
// negative statements, and each negative statement no spec makes any more.
func vendorSupportViolations(files []markdownFile) []string {
	var violations []string
	used := map[int]bool{}
	for _, file := range files {
		if !strings.HasPrefix(file.path, "specs/") || vendorSupportRecords[file.path] {
			continue
		}
		proseLines(file, func(number int, line string) {
			if decisionHistoryRow.MatchString(line) {
				return
			}
			text := line
			if number < len(file.lines) {
				text += " " + strings.TrimSpace(file.lines[number])
			}
			for _, match := range vendorSupportPhrase.FindAllStringIndex(text, -1) {
				if match[0] >= len(line) {
					continue
				}
				negated := false
				for index, negation := range vendorSupportNegations {
					if file.path == negation.file && strings.Contains(text, negation.sentence) {
						negated, used[index] = true, true
					}
				}
				if !negated {
					violations = append(violations, fmt.Sprintf("%s:%d says %q; a coverage row means Bootwright realizes the capability, never a vendor's support status", file.path, number, text[match[0]:match[1]]))
				}
			}
		})
	}
	for index, negation := range vendorSupportNegations {
		if !used[index] {
			violations = append(violations, fmt.Sprintf("%s no longer says %q; remove its entry from vendorSupportNegations", negation.file, negation.sentence))
		}
	}
	return violations
}

func TestDocsSpecsStateNoVendorSupportStatus(t *testing.T) {
	for _, violation := range vendorSupportViolations(guidanceFiles(t)) {
		t.Error(violation)
	}
}

func TestDocsVendorSupportViolations(t *testing.T) {
	negated := markdownFile{path: "specs/cli/output.md", lines: []string{"A refusal never claims a", "vendor release or support", "status of its own."}}
	if got := vendorSupportViolations([]markdownFile{negated}); len(got) != 1 || !strings.Contains(got[0], "specs/milestones/m1.md no longer says") {
		t.Errorf("a spec that makes only one negative statement gave %v", got)
	}
	pages := []markdownFile{
		negated,
		{path: "specs/milestones/m1.md", lines: []string{"Bootwright records no vendor FIPS support status (D159 clause 15)."}},
		{path: "specs/milestones/backlog.md", lines: []string{"The IBM-supported release."}},
		{path: "specs/milestones/m2.md", lines: []string{"| OD27 | Vendor support statuses | answered |", "```", "support status", "```"}},
	}
	if got := vendorSupportViolations(pages); len(got) != 0 {
		t.Errorf("allowed uses gave %v", got)
	}
	pages = append(pages, markdownFile{path: "specs/ceph-coverage.md", lines: []string{"Each row is required when", "IBM-supported by the frozen release; a Release Support", "table lists the support", "status of each."}})
	got := vendorSupportViolations(pages)
	if len(got) != 3 {
		t.Fatalf("a reintroduced phrase gave %v, want three violations", got)
	}
	variants := []markdownFile{{path: "specs/ceph.md", lines: []string{
		"The vendor supported release, the Red Hat-supported path, a provider supported layout,",
		"and a row supported by IBM, supported by Red Hat or supported by the vendor.",
		"A path Bootwright supports, a supported flag and supported by the schema are fine.",
	}}}
	if got := vendorSupportViolations(append(slices.Clone(pages[:4]), variants...)); len(got) != 6 {
		t.Errorf("the vendor-support variants gave %v, want six violations", got)
	}
	for _, want := range []string{`specs/ceph-coverage.md:2 says "IBM-supported"`, `specs/ceph-coverage.md:2 says "Release Support"`, `specs/ceph-coverage.md:3 says "support status"`} {
		if !slices.ContainsFunc(got, func(violation string) bool { return strings.HasPrefix(violation, want) }) {
			t.Errorf("no violation starts %q in %v", want, got)
		}
	}
}
