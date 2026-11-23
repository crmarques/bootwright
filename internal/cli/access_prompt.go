package cli

import (
	"io"
	"strings"
	"sync"
)

type AccessPrompt struct {
	out    io.Writer
	opened sync.Once
}

func NewAccessPrompt(out io.Writer) *AccessPrompt { return &AccessPrompt{out: out} }

func (p *AccessPrompt) Line(prompt string) string {
	options := presentationFor(p.out)
	if options.Format != "text" {
		return prompt
	}
	p.opened.Do(func() {
		var group display
		group.section("First access")
		group.subsection("Typing stays hidden. Passwords are kept in memory for this run only.")
		io.WriteString(p.out, "\n")
		group.writeTo(p.out)
		io.WriteString(p.out, "\n")
	})
	label, hint := strings.TrimSuffix(prompt, ": "), ""
	if open := strings.LastIndex(label, " ("); open >= 0 && strings.HasSuffix(label, ")") {
		label, hint = label[:open], label[open:]
	}
	return "    " + escapeDisplayLine(label) + styleMuted(escapeDisplayLine(hint), options) + ": "
}
