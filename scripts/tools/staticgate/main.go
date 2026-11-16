// Command staticgate fails the gate on a staticcheck finding the exclusion
// list does not name, and on an exclusion that no longer matches as many
// findings as it counts, so the list only shrinks. It reads staticcheck's JSON
// stream. Each exclusion line holds a check, a file relative to the module
// root, the number of findings it covers and the start of their message,
// which names the symbol, separated by tabs; a finding counts against the
// matching exclusion with the longest message start. The lines are sorted
// and unique, and a line starting with # is a comment.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

type finding struct {
	Code     string `json:"code"`
	Location struct {
		File   string `json:"file"`
		Line   int    `json:"line"`
		Column int    `json:"column"`
	} `json:"location"`
	Message string `json:"message"`
}

type exclusion struct {
	check, file, subject string
	count                int
	line                 int
}

func (e exclusion) String() string {
	return strings.Join([]string{e.check, e.file, strconv.Itoa(e.count), e.subject}, "\t")
}

func main() {
	findings, exclusions, root := "", "", ""
	flag.StringVar(&findings, "findings", "", "staticcheck -f json output")
	flag.StringVar(&exclusions, "exclusions", "", "the reviewed exclusion list")
	flag.StringVar(&root, "root", "", "the module root the findings' file names are relative to")
	flag.Parse()
	if findings == "" || exclusions == "" || root == "" {
		fail(errors.New("-findings, -exclusions and -root are required"))
	}
	stream, err := os.ReadFile(findings)
	if err != nil {
		fail(err)
	}
	list, err := os.ReadFile(exclusions)
	if err != nil {
		fail(err)
	}
	problems, err := check(stream, list, root)
	if err != nil {
		fail(err)
	}
	if len(problems) > 0 {
		fmt.Fprintln(os.Stderr, strings.Join(problems, "\n"))
		fail(fmt.Errorf("%d problems; fix each finding, or remove or lower each exclusion named, in %s", len(problems), exclusions))
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "staticgate:", err)
	os.Exit(1)
}

func parseFindings(stream []byte, root string) ([]finding, error) {
	var found []finding
	decoder := json.NewDecoder(bytes.NewReader(stream))
	for {
		var f finding
		if err := decoder.Decode(&f); errors.Is(err, io.EOF) {
			return found, nil
		} else if err != nil {
			return nil, fmt.Errorf("the findings are no staticcheck JSON stream: %w", err)
		}
		if f.Code == "" {
			return nil, errors.New("the findings hold an entry without a check")
		}
		if relative, err := filepath.Rel(root, f.Location.File); err == nil && !strings.HasPrefix(relative, "..") {
			f.Location.File = filepath.ToSlash(relative)
		}
		found = append(found, f)
	}
}

func parseExclusions(list []byte) ([]exclusion, []string) {
	var entries []exclusion
	var problems []string
	var previous string
	scanner := bufio.NewScanner(bytes.NewReader(list))
	for number := 1; scanner.Scan(); number++ {
		text := scanner.Text()
		if strings.HasPrefix(text, "#") {
			continue
		}
		fields := strings.Split(text, "\t")
		count, err := 0, error(nil)
		if len(fields) == 4 {
			count, err = strconv.Atoi(fields[2])
		}
		if len(fields) != 4 || err != nil || count < 1 || fields[0] == "" || fields[1] == "" || fields[3] == "" {
			problems = append(problems, fmt.Sprintf("exclusion line %d is not check, file, count and message start separated by tabs", number))
			continue
		}
		key := strings.Join([]string{fields[0], fields[1], fields[3]}, "\t")
		if key <= previous {
			problems = append(problems, fmt.Sprintf("exclusion line %d is out of order or repeats the line before it", number))
		}
		previous = key
		entries = append(entries, exclusion{check: fields[0], file: fields[1], subject: fields[3], count: count, line: number})
	}
	return entries, problems
}

// check returns every finding no exclusion covers and every exclusion whose
// count differs from the findings it covers.
func check(stream, list []byte, root string) ([]string, error) {
	found, err := parseFindings(stream, root)
	if err != nil {
		return nil, err
	}
	entries, problems := parseExclusions(list)
	covered := make([]int, len(entries))
	for _, f := range found {
		best := -1
		for i, e := range entries {
			if e.check == f.Code && e.file == f.Location.File && strings.HasPrefix(f.Message, e.subject) && (best < 0 || len(e.subject) > len(entries[best].subject)) {
				best = i
			}
		}
		if best < 0 {
			problems = append(problems, fmt.Sprintf("%s:%d:%d: %s (%s) is no listed exclusion", f.Location.File, f.Location.Line, f.Location.Column, f.Message, f.Code))
			continue
		}
		covered[best]++
	}
	for i, e := range entries {
		switch {
		case covered[i] == 0:
			problems = append(problems, fmt.Sprintf("exclusion line %d matches no finding; remove it: %s", e.line, e))
		case covered[i] < e.count:
			problems = append(problems, fmt.Sprintf("exclusion line %d covers %d findings, fewer than it counts; lower it to %d: %s", e.line, covered[i], covered[i], e))
		case covered[i] > e.count:
			problems = append(problems, fmt.Sprintf("exclusion line %d covers %d findings, more than it counts; fix the new findings: %s", e.line, covered[i], e))
		}
	}
	slices.Sort(problems)
	return problems, nil
}
