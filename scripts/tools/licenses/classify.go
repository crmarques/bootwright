package main

import (
	"embed"
	"regexp"
	"strings"
)

//go:embed references/*.txt
var referenceFiles embed.FS

var referenceIDs = []string{"MIT", "BSD-2-Clause", "BSD-3-Clause", "ISC", "Apache-2.0", "MPL-2.0"}

type reviewedText struct {
	sha256, license, reviewed string
}

var reviewedTexts = []reviewedText{
	{"d18f6323b71b0b768bb5e9616e36da390fbd39369a81807cca352de4e4e6aa0b", "MIT AND Apache-2.0",
		"go.yaml.in/yaml/v3 v3.0.5: a header naming two licenses, the MIT text of the libyaml port, then the Apache-2.0 notice of the rest"},
}

var (
	nonWord       = regexp.MustCompile(`[^a-z0-9]+`)
	clauseNumber  = regexp.MustCompile(` [123] (redistributions|neither) `)
	thirdClause   = regexp.MustCompile(`neither the name of ([a-z0-9 ]{1,120}?) nor the names of its contributors`)
	copyrightLine = regexp.MustCompile(`^(copyright\b|\(c\)|\x{a9})[^.]*\.?( all rights reserved\.?)?$`)
	restricting   = map[string]bool{"not": true, "no": true, "except": true, "only": true, "without": true, "unless": true}
	knownTitles   = map[string]bool{"mit license": true, "the mit license": true, "the mit license mit": true, "isc license": true,
		"bsd 2 clause license": true, "bsd 3 clause license": true, "all rights reserved": true}
	endOfTerms   = "end of terms and conditions"
	wordSynonyms = strings.NewReplacer("copyright owner", "copyright holder")
)

type canonicalText struct{ head, tail string }

func words(line string) string {
	return strings.Join(strings.Fields(nonWord.ReplaceAllString(strings.ToLower(line), " ")), " ")
}

func statement(line string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(line))
	for _, word := range strings.Fields(words(trimmed)) {
		if restricting[word] {
			return false
		}
	}
	return copyrightLine.MatchString(trimmed)
}

func headerLine(line string) bool {
	return strings.TrimSpace(line) == "" || statement(line) || knownTitles[words(line)]
}

func normalize(lines []string) string {
	body := " " + words(strings.Join(lines, " ")) + " "
	body = clauseNumber.ReplaceAllString(body, " $1 ")
	body = thirdClause.ReplaceAllStringFunc(wordSynonyms.Replace(body), func(clause string) string {
		for _, word := range strings.Fields(thirdClause.FindStringSubmatch(clause)[1]) {
			if restricting[word] {
				return clause
			}
		}
		return "neither the name of the copyright holder nor the names of its contributors"
	})
	return strings.TrimSpace(body)
}

func canonical(text string) canonicalText {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for len(lines) > 0 && headerLine(lines[0]) {
		lines = lines[1:]
	}
	for index, line := range lines {
		if words(line) == endOfTerms {
			var tail []string
			for _, rest := range lines[index+1:] {
				if !statement(rest) {
					tail = append(tail, rest)
				}
			}
			return canonicalText{normalize(lines[:index]), normalize(tail)}
		}
	}
	return canonicalText{head: normalize(lines)}
}

func classify(text string, references map[string]canonicalText) (string, bool) {
	body := canonical(text)
	if body.head == "" {
		return "", false
	}
	for _, id := range referenceIDs {
		if reference := references[id]; reference.head == body.head && (body.tail == "" || body.tail == reference.tail) {
			return id, true
		}
	}
	return "", false
}

func loadReferences() (map[string]canonicalText, error) {
	references := map[string]canonicalText{}
	for _, id := range referenceIDs {
		data, err := referenceFiles.ReadFile("references/" + id + ".txt")
		if err != nil {
			return nil, err
		}
		references[id] = canonical(string(data))
	}
	return references, nil
}
