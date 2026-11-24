package docs_test

import (
	"fmt"
	"maps"
	"path"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// specHeading is one heading of a spec page with the anchor GitHub gives it.
type specHeading struct {
	line, level int
	anchor      string
}

// specHeadings lists the headings of a page outside code fences, each with its
// anchor, repeated headings carrying the numeric suffix GitHub appends.
func specHeadings(file markdownFile) []specHeading {
	var headings []specHeading
	seen := map[string]int{}
	proseLines(file, func(number int, line string) {
		if !strings.HasPrefix(line, "#") {
			return
		}
		text := strings.TrimLeft(line, "#")
		slug := githubSlug(strings.TrimSpace(text))
		anchor := slug
		if count := seen[slug]; count > 0 {
			anchor = slug + "-" + strconv.Itoa(count)
		}
		seen[slug]++
		headings = append(headings, specHeading{line: number, level: len(line) - len(text), anchor: anchor})
	})
	return headings
}

// enclosingAnchors returns the anchors of the headings that enclose a line,
// from level 2 down to the nearest one.
func enclosingAnchors(headings []specHeading, line int) []string {
	var stack []specHeading
	for _, heading := range headings {
		if heading.line >= line {
			break
		}
		for len(stack) > 0 && stack[len(stack)-1].level >= heading.level {
			stack = stack[:len(stack)-1]
		}
		stack = append(stack, heading)
	}
	var anchors []string
	for _, heading := range stack {
		if heading.level >= 2 {
			anchors = append(anchors, heading.anchor)
		}
	}
	return anchors
}

// itemSectionLinks returns the resolved link targets of an item's "### B<n>"
// section on a milestone page: the lines up to the next heading of level three
// or above.
func itemSectionLinks(page markdownFile, item string) map[string]bool {
	links := map[string]bool{}
	inside := false
	proseLines(page, func(_ int, line string) {
		if strings.HasPrefix(line, "#") {
			level := len(line) - len(strings.TrimLeft(line, "#"))
			if inside && level <= 3 {
				inside = false
			}
			if strings.TrimRight(line, " ") == "### "+item {
				inside = true
			}
			return
		}
		if !inside {
			return
		}
		for _, match := range markdownLink.FindAllStringSubmatch(line, -1) {
			target, anchor, _ := strings.Cut(match[1], "#")
			if target != "" && anchor != "" {
				links[path.Join(path.Dir(page.path), target)+"#"+anchor] = true
			}
		}
	})
	return links
}

// notYetMetOwnerViolations reports, as "page B<n>", each item that a Not yet
// met paragraph of a normative spec names and whose section on its milestone
// page or the backlog links none of the spec sections that enclose the
// paragraph, so the item does not name the section that owns the gap.
func notYetMetOwnerViolations(files []markdownFile) []string {
	byPath := map[string]markdownFile{}
	for _, file := range files {
		byPath[file.path] = file
	}
	found := map[string]bool{}
	for _, file := range files {
		if !strings.HasPrefix(file.path, "specs/") ||
			strings.HasPrefix(file.path, "specs/milestones/") || strings.HasPrefix(file.path, "specs/deferred/") {
			continue
		}
		headings := specHeadings(file)
		start, paragraph := 0, []string(nil)
		flush := func() {
			if start != 0 && strings.HasPrefix(strings.TrimSpace(paragraph[0]), "Not yet met:") {
				anchors := enclosingAnchors(headings, start)
				text := inlineCode.ReplaceAllString(strings.Join(paragraph, " "), "")
				for _, link := range notYetMetLink.FindAllStringSubmatch(text, -1) {
					linked, _, _ := strings.Cut(link[2], "#")
					page, known := byPath[path.Join(path.Dir(file.path), linked)]
					if !known || !notYetMetPage.MatchString(page.path) {
						continue
					}
					links := itemSectionLinks(page, link[1])
					owned := slices.ContainsFunc(anchors, func(anchor string) bool { return links[file.path+"#"+anchor] })
					if !owned {
						found[file.path+" "+link[1]] = true
					}
				}
			}
			start, paragraph = 0, nil
		}
		proseLines(file, func(number int, line string) {
			if strings.TrimSpace(line) == "" {
				flush()
				return
			}
			if start == 0 {
				start = number
			}
			paragraph = append(paragraph, line)
		})
		flush()
	}
	return slices.Sorted(maps.Keys(found))
}

// TestDocsNotYetMetItemsLinkTheirOwningSection holds each item a Not yet met
// paragraph names to the section that carries the paragraph: the item's own
// section links that spec section or one that encloses it. The list of items
// that do not yet only shrinks.
func TestDocsNotYetMetItemsLinkTheirOwningSection(t *testing.T) {
	found := notYetMetOwnerViolations(guidanceFiles(t))
	for _, violation := range knownListViolations(found, readKnownList(t, "docs-not-yet-met-owner.txt"), "docs-not-yet-met-owner.txt") {
		t.Error(violation)
	}
}

func TestDocsNotYetMetOwnerViolations(t *testing.T) {
	milestone := markdownFile{path: "specs/milestones/m2.md", lines: strings.Split(
		"| [B1](#b1) | x |\n| [B2](#b2) | x |\n| [B3](#b3) | x |\n\n### B1\n\nOwned by [the section](../a.md#sec).\n\n### B2\n\nOwned by [the page](../a.md).\n\n### B3\n\nOwned by [the parent](../a.md#top) and [a sibling](../a.md#other).\n\n```\n### B4\n```\n", "\n")}
	spec := markdownFile{path: "specs/a.md", lines: strings.Split(
		"# A\n\n## Top\n\n### Sec\n\nNot yet met: one; tracked as [B1](milestones/m2.md#b1).\n\nNot yet met: two; tracked as [B2](milestones/m2.md#b2).\n\n### Other\n\nNot yet met: three; tracked as [B3](milestones/m2.md#b3).\n\nNot yet met: four; tracked as [B1](milestones/m2.md#b1).\n\nA rule. See [B2](milestones/m2.md#b2).\n", "\n")}
	want := []string{"specs/a.md B1", "specs/a.md B2"}
	if got := notYetMetOwnerViolations([]markdownFile{spec, milestone}); !slices.Equal(got, want) {
		t.Fatalf("violations = %q, want %q", got, want)
	}
	if got := fmt.Sprint(enclosingAnchors(specHeadings(spec), 7)); got != "[top sec]" {
		t.Fatalf("anchors = %s", got)
	}
}
