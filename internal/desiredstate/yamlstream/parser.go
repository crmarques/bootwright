package yamlstream

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"go.yaml.in/yaml/v3"
)

type Parser struct{}

func (Parser) Parse(ctx context.Context, files []desiredstate.SourceFile) ([]desiredstate.Document, []diagnostics.Diagnostic, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if len(files) > desiredstate.MaxFiles {
		err := diagnostics.NewFailure("input.limit", "YAML source files exceeds the ceiling of 4096", "")
		return nil, diagnostics.Of(err), err
	}
	ordered := slices.Clone(files)
	slices.SortStableFunc(ordered, func(a, b desiredstate.SourceFile) int {
		return strings.Compare(a.Path(), b.Path())
	})
	if err := checkFileSizes(ctx, ordered); err != nil {
		return nil, diagnostics.Of(err), err
	}
	p := parseSession{ctx: ctx}
	for _, file := range ordered {
		if err := p.parseFile(file); err != nil {
			if failure := diagnostics.Of(err); len(failure) != 0 {
				p.diagnostics = append(p.diagnostics, failure...)
			}
			diagnostics.Sort(p.diagnostics)
			return nil, p.diagnostics, err
		}
	}
	diagnostics.Sort(p.diagnostics)
	return p.documents, p.diagnostics, nil
}

type parseSession struct {
	ctx         context.Context
	documents   []desiredstate.Document
	diagnostics []diagnostics.Diagnostic
	documentNum int
	nodes       int
}

func checkFileSizes(ctx context.Context, files []desiredstate.SourceFile) error {
	total := 0
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		if file.Size() > desiredstate.MaxFileBytes {
			return limitFailure("YAML file bytes", desiredstate.MaxFileBytes, file.Path(), 0, nil)
		}
		if file.Size() > desiredstate.MaxAllFileBytes-total {
			return limitFailure("aggregate YAML bytes", desiredstate.MaxAllFileBytes, file.Path(), 0, nil)
		}
		total += file.Size()
	}
	return nil
}

func (p *parseSession) parseFile(file desiredstate.SourceFile) error {
	if err := p.ctx.Err(); err != nil {
		return err
	}
	data := file.Bytes()
	if !utf8.Valid(data) {
		return p.addSyntax(file.Path(), 0, 0)
	}
	text := newSourceText(data)
	decoder := yaml.NewDecoder(contextReader{ctx: p.ctx, reader: bytes.NewReader(data)})
	for index := 1; ; index++ {
		if err := p.ctx.Err(); err != nil {
			return err
		}
		var raw yaml.Node
		err := decoder.Decode(&raw)
		if canceled := p.ctx.Err(); canceled != nil {
			return canceled
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return p.addSyntax(file.Path(), index, parserErrorLine(err))
		}
		p.documentNum++
		if index > desiredstate.MaxFileDocuments {
			return limitFailure("YAML documents per file", desiredstate.MaxFileDocuments, file.Path(), index, &raw)
		}
		if p.documentNum > desiredstate.MaxDocuments {
			return limitFailure("aggregate YAML documents", desiredstate.MaxDocuments, file.Path(), index, &raw)
		}
		if err := p.checkRepresentation(file.Path(), index, &raw); err != nil {
			return err
		}
		root, err := convert(p.ctx, &raw, text)
		if err != nil {
			return err
		}
		p.documents = append(p.documents, desiredstate.Document{Path: file.Path(), Index: index, Root: root})
	}
}

func (p *parseSession) addSyntax(path string, document, line int) error {
	// At most one syntax failure is retained per byte-bounded source file.
	// The compiler applies the returned-diagnostic ceiling after resource
	// selection, so excluded files cannot consume its diagnostic allowance.
	p.diagnostics = append(p.diagnostics, diagnostics.Diagnostic{
		Severity: "error", Code: "yaml.syntax", Message: "Input is not valid UTF-8 YAML.",
		Source: &diagnostics.SourceLocation{Path: path, Document: document, Line: line},
	})
	return nil
}

func (p *parseSession) checkRepresentation(path string, document int, root *yaml.Node) error {
	type frame struct {
		node *yaml.Node
		next int
	}
	stack := []frame{{node: root, next: -1}}
	count := 0
	for len(stack) != 0 {
		if err := p.ctx.Err(); err != nil {
			return err
		}
		current := &stack[len(stack)-1]
		if current.next == -1 {
			count++
			p.nodes++
			if len(stack)-1 > desiredstate.MaxDepth {
				return limitFailure("YAML representation depth", desiredstate.MaxDepth, path, document, current.node)
			}
			if count > desiredstate.MaxDocumentNodes {
				return limitFailure("YAML representation nodes per document", desiredstate.MaxDocumentNodes, path, document, current.node)
			}
			if p.nodes > desiredstate.MaxNodes {
				return limitFailure("aggregate YAML representation nodes", desiredstate.MaxNodes, path, document, current.node)
			}
			current.next = 0
		}
		if current.node.Kind == yaml.AliasNode || current.next == len(current.node.Content) {
			stack = stack[:len(stack)-1]
			continue
		}
		child := current.node.Content[current.next]
		current.next++
		stack = append(stack, frame{node: child, next: -1})
	}
	return nil
}

func limitFailure(resource string, ceiling int, path string, document int, node *yaml.Node) error {
	source := &diagnostics.SourceLocation{Path: path, Document: document}
	if node != nil {
		source.Line, source.Column = node.Line, node.Column
	}
	return &diagnostics.Failure{Diagnostics: []diagnostics.Diagnostic{{
		Severity: "error", Code: "input.limit",
		Message: fmt.Sprintf("%s exceeds the ceiling of %d", resource, ceiling), Source: source,
	}}}
}

func parserErrorLine(err error) int {
	text := err.Error()
	const prefix = "yaml: line "
	if !strings.HasPrefix(text, prefix) {
		return 0
	}
	number, _, found := strings.Cut(text[len(prefix):], ":")
	if !found {
		return 0
	}
	line, parseErr := strconv.Atoi(number)
	if parseErr != nil || line < 1 {
		return 0
	}
	return line
}

type contextReader struct {
	ctx    context.Context
	reader *bytes.Reader
}

func (r contextReader) Read(data []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(data)
}
