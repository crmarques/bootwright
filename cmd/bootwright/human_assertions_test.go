package main

import (
	"strings"
	"unicode"
)

// humanContent compares all presentation content independently of indentation
// and wrapping, including wraps within long synthetic temporary paths. Raw
// secret bytes, JSON documents, prompts and receipts use their exact assertions.
func humanContent(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, value)
}
