package yamlstream

import (
	"bytes"
	"context"
	"unicode/utf8"

	"github.com/crmarques/bootwright/internal/desiredstate"
	"go.yaml.in/yaml/v3"
)

func convert(ctx context.Context, root *yaml.Node, text sourceText) (*desiredstate.Node, error) {
	type frame struct {
		raw   *yaml.Node
		owned *desiredstate.Node
		next  int
	}
	owned := copyNode(root, text)
	stack := []frame{{raw: root, owned: owned}}
	for len(stack) != 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		current := &stack[len(stack)-1]
		if current.raw.Kind == yaml.AliasNode || current.next == len(current.raw.Content) {
			*current.raw = yaml.Node{}
			stack = stack[:len(stack)-1]
			continue
		}
		child := current.raw.Content[current.next]
		current.next++
		copy := copyNode(child, text)
		current.owned.Content = append(current.owned.Content, copy)
		stack = append(stack, frame{raw: child, owned: copy})
	}
	return owned, nil
}

func copyNode(raw *yaml.Node, text sourceText) *desiredstate.Node {
	node := &desiredstate.Node{
		Value: raw.Value, Tag: raw.Tag, Anchor: raw.Anchor,
		Line: raw.Line, Column: raw.Column, ExplicitTag: raw.Style&yaml.TaggedStyle != 0,
	}
	switch raw.Kind {
	case yaml.DocumentNode:
		node.Kind = desiredstate.DocumentKind
	case yaml.MappingNode:
		node.Kind = desiredstate.MappingKind
	case yaml.SequenceNode:
		node.Kind = desiredstate.SequenceKind
	case yaml.ScalarNode:
		node.Kind = desiredstate.ScalarKind
	case yaml.AliasNode:
		node.Kind = desiredstate.AliasKind
	}
	switch {
	case raw.Style&yaml.DoubleQuotedStyle != 0:
		node.Style = desiredstate.DoubleQuotedStyle
	case raw.Style&yaml.SingleQuotedStyle != 0:
		node.Style = desiredstate.SingleQuotedStyle
	case raw.Style&yaml.LiteralStyle != 0:
		node.Style = desiredstate.LiteralStyle
	case raw.Style&yaml.FoldedStyle != 0:
		node.Style = desiredstate.FoldedStyle
	case raw.Style&yaml.FlowStyle != 0:
		node.Style = desiredstate.FlowStyle
	}
	if !node.ExplicitTag && node.Kind != desiredstate.DocumentKind && text.nonSpecificTag(raw) {
		node.ExplicitTag = true
		if node.Kind == desiredstate.ScalarKind {
			node.Tag = "!!str"
		}
	}
	if len(raw.Content) != 0 && raw.Kind != yaml.AliasNode {
		node.Content = make([]*desiredstate.Node, 0, len(raw.Content))
	}
	return node
}

type sourceText struct {
	data       []byte
	runeStarts []int
	lineStarts []int
}

func newSourceText(data []byte) sourceText {
	text := sourceText{data: data}
	if !bytes.ContainsRune(data, '!') {
		return text
	}
	text.lineStarts = []int{0}
	for offset := 0; offset < len(data); {
		if offset == 0 && bytes.HasPrefix(data, []byte("\xef\xbb\xbf")) {
			offset = 3
			continue
		}
		text.runeStarts = append(text.runeStarts, offset)
		r, size := utf8.DecodeRune(data[offset:])
		offset += size
		if r == '\r' && offset < len(data) && data[offset] == '\n' {
			offset++
		}
		if lineBreak(r) {
			text.lineStarts = append(text.lineStarts, len(text.runeStarts))
		}
	}
	text.runeStarts = append(text.runeStarts, len(data))
	return text
}

func (t sourceText) nonSpecificTag(node *yaml.Node) bool {
	if node.Line < 1 || node.Line > len(t.lineStarts) || node.Column < 1 {
		return false
	}
	runeIndex := t.lineStarts[node.Line-1] + node.Column - 1
	if runeIndex >= len(t.runeStarts) {
		return false
	}
	offset := t.runeStarts[runeIndex]
	if node.Anchor != "" && offset < len(t.data) && t.data[offset] == '&' {
		offset += 1 + len(node.Anchor)
		for offset < len(t.data) {
			if t.data[offset] == '#' {
				for offset < len(t.data) {
					r, size := utf8.DecodeRune(t.data[offset:])
					if lineBreak(r) {
						break
					}
					offset += size
				}
			} else {
				r, size := utf8.DecodeRune(t.data[offset:])
				if r != ' ' && r != '\t' && !lineBreak(r) {
					break
				}
				offset += size
			}
		}
	}
	if offset >= len(t.data) || t.data[offset] != '!' {
		return false
	}
	if offset+1 == len(t.data) {
		return true
	}
	next, _ := utf8.DecodeRune(t.data[offset+1:])
	return lineBreak(next) || next == ' ' || next == '\t' || next == ',' ||
		next == '[' || next == ']' || next == '{' || next == '}'
}

func lineBreak(r rune) bool {
	return r == '\n' || r == '\r' || r == '\u0085' || r == '\u2028' || r == '\u2029'
}
