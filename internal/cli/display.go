package cli

import (
	"io"
	"strings"
)

const (
	displayIndent = "  "
	displayGap    = 2
)

// display records raw facts; the renderer owns escaping, styling, spacing and width.
type display struct {
	blocks []func(*displayRenderer)
	// text preserves ordered native previews, flushed before the next block.
	text strings.Builder
}

type field struct{ Label, Value string }
type step struct {
	Text    string
	Effects []string
}

// tableStyle declares semantic columns independently of resource names and headers.
type tableStyle struct {
	Kind     string
	Identity int
	States   []int
}

type displayRenderer struct {
	options PresentationOptions
	text    strings.Builder
}

func (d *display) flushText() {
	if d.text.Len() == 0 {
		return
	}
	value := d.text.String()
	d.text.Reset()
	d.blocks = append(d.blocks, func(r *displayRenderer) { r.text.WriteString(value) })
}

func (d *display) custom(render func(*displayRenderer)) {
	d.flushText()
	d.blocks = append(d.blocks, render)
}

func (d *display) headline(status, title string) {
	d.custom(func(r *displayRenderer) {
		r.gap()
		if status == "" {
			r.paragraph(title, "  ", "identity")
			return
		}
		mark := "○"
		switch status {
		case "OK", "DONE":
			mark = "✓"
		case "FAIL", "UNKNOWN", "CANCELED":
			mark = "!"
		}
		r.styledParagraph(escapeDisplayLine(title), "  "+styleStatusSymbol(mark, status, r.options)+" ", "plain")
	})
}

func (d *display) section(title string) {
	d.custom(func(r *displayRenderer) {
		r.gap()
		if title != "" {
			r.paragraph(title, "  ", "accent")
		}
	})
}

func (d *display) subsection(title string) {
	d.custom(func(r *displayRenderer) { r.paragraph(title, "    ", "muted") })
}

func (d *display) separate() { d.section("") }

func (d *display) fields(values ...field) {
	copy := append([]field(nil), values...)
	d.custom(func(r *displayRenderer) { r.fields(copy, false, false) })
}

func (d *display) mutedFields(values ...field) {
	copy := append([]field(nil), values...)
	d.custom(func(r *displayRenderer) { r.fields(copy, true, false) })
}

func (d *display) rows(rows [][]string) {
	d.custom(func(r *displayRenderer) { r.table(nil, rows, tableStyle{Identity: -1}) })
}

func (d *display) table(headers []string, rows [][]string) {
	d.custom(func(r *displayRenderer) { r.table(headers, rows, tableStyle{Identity: -1}) })
}

func (d *display) resourceTable(headers []string, rows [][]string, style tableStyle) {
	d.custom(func(r *displayRenderer) { r.table(headers, rows, style) })
}

func (d *display) resourceHeading(kind, name, state string) {
	d.custom(func(r *displayRenderer) { r.resourceHeading(kind, name, state) })
}

func (d *display) steps(items []step) {
	d.custom(func(r *displayRenderer) {
		for _, item := range items {
			r.paragraph("○ "+item.Text, "    ", "plain")
			for _, effect := range item.Effects {
				r.paragraph(effect, "      ", "muted")
			}
		}
	})
}

func (d *display) lines(items []string) {
	d.custom(func(r *displayRenderer) {
		for _, item := range items {
			r.paragraph(item, "    ", "plain")
		}
	})
}

func (d *display) mutedVerbatimLines(items []string) {
	d.custom(func(r *displayRenderer) {
		for _, item := range items {
			r.verbatimLine(escapeDisplayLine(item), "      ", "muted")
		}
	})
}

func (d *display) verbatimFields(values ...field) {
	copy := append([]field(nil), values...)
	d.custom(func(r *displayRenderer) { r.fields(copy, false, true) })
}

func (d *display) mutedVerbatimFields(values ...field) {
	copy := append([]field(nil), values...)
	d.custom(func(r *displayRenderer) { r.fields(copy, true, true) })
}

func (d *display) mutedLines(items []string) {
	d.custom(func(r *displayRenderer) {
		for _, item := range items {
			r.paragraph(item, "      ", "muted")
		}
	})
}

func displayValue(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func (d *display) writeTo(out io.Writer) error {
	d.flushText()
	r := &displayRenderer{options: presentationFor(out)}
	if r.options.Format != "text" {
		return nil
	}
	for _, render := range d.blocks {
		render(r)
	}
	text := strings.TrimRight(r.text.String(), "\n")
	if text == "" {
		return nil
	}
	text += "\n"
	n, err := io.WriteString(out, text)
	if err == nil && n != len(text) {
		err = io.ErrShortWrite
	}
	return err
}

func (r *displayRenderer) gap() {
	if r.text.Len() > 0 && !strings.HasSuffix(r.text.String(), "\n\n") {
		r.text.WriteByte('\n')
	}
}

func (r *displayRenderer) line(value string) { r.emit(value, true) }

func (r *displayRenderer) wholeLine(value string) { r.emit(value, false) }

func (r *displayRenderer) emit(value string, fit bool) {
	if r.options.ASCII {
		value = presentationASCII(value)
	}
	value = strings.TrimRight(value, " ")
	if fit {
		value = fitDisplayRow(value, r.options.columns())
	}
	r.text.WriteString(value)
	r.text.WriteByte('\n')
}

func (r *displayRenderer) verbatimLine(escaped, indent, tone string) {
	r.wholeLine(indent + r.tone(escaped, tone))
}

func (r *displayRenderer) tone(value, tone string) string {
	switch tone {
	case "muted":
		return styleMuted(value, r.options)
	case "identity":
		return styleIdentity(value, r.options)
	case "accent":
		return styleAccent(value, r.options)
	case "state":
		return styleState(value, r.options)
	}
	return value
}

func (r *displayRenderer) paragraph(value, indent, tone string) {
	r.styledParagraph(escapeDisplayLine(value), indent, tone)
}

func (r *displayRenderer) styledParagraph(escaped, indent, tone string) {
	if r.options.ASCII {
		escaped = presentationASCII(escaped)
	}
	width := max(1, r.options.columns()-displayCellWidth(indent))
	for _, line := range wrapDisplayText(escaped, width) {
		r.emit(indent+r.tone(line, tone), displayCellWidth(line) <= width)
		indent = strings.Repeat(" ", displayCellWidth(indent))
	}
}

func (r *displayRenderer) fields(values []field, muted, verbatim bool) {
	labelWidth := 0
	for _, value := range values {
		labelWidth = max(labelWidth, displayCellWidth(escapeDisplayLine(value.Label)))
	}
	for _, value := range values {
		label := escapeDisplayLine(value.Label)
		indent, tone := "    ", "plain"
		if muted {
			indent, tone = "      ", "muted"
		}
		prefix := label + strings.Repeat(" ", labelWidth-displayCellWidth(label)+displayGap)
		if !verbatim && r.options.columns()-displayCellWidth(indent+prefix) < 12 {
			r.paragraph(value.Label+": "+value.Value, indent, tone)
			continue
		}
		width := r.options.columns() - displayCellWidth(indent+prefix)
		escaped := escapeDisplayLine(value.Value)
		if r.options.ASCII {
			escaped = presentationASCII(escaped)
		}
		lines := []string{escaped}
		if !verbatim {
			lines = wrapDisplayText(escaped, width)
		}
		for n, line := range lines {
			fit := displayCellWidth(line) <= width
			if n == 0 {
				if muted {
					r.emit(indent+styleMuted(prefix+line, r.options), fit)
				} else {
					r.emit(indent+styleMuted(prefix, r.options)+line, fit)
				}
			} else {
				r.emit(indent+strings.Repeat(" ", displayCellWidth(prefix))+r.tone(line, tone), fit)
			}
		}
	}
}

// Wrap at spaces; a token wider than a whole row keeps a line of its own, unsplit.
func wrapDisplayText(value string, width int) []string {
	width = max(1, width)
	var lines []string
	line := ""
	for index, word := range strings.Split(value, " ") {
		separator := ""
		if index > 0 && (line != "" || len(lines) == 0) {
			separator = " "
		}
		if displayCellWidth(line+separator+word) <= width {
			line += separator + word
			continue
		}
		if line != "" {
			lines = append(lines, strings.TrimRight(line, " "))
			line = ""
		}
		line = word
	}
	return append(lines, strings.TrimRight(line, " "))
}

func (r *displayRenderer) resourceHeading(kind, name, state string) {
	prefix := "  ○ "
	if kind != "" {
		prefix += escapeDisplayLine(kind) + " | "
	}
	name = escapeDisplayLine(name)
	if displayCellWidth(prefix+name) > r.options.columns() {
		prefix = "  ○ "
	}
	if r.options.ASCII {
		name = presentationASCII(name)
	}
	line := prefix + styleIdentity(name, r.options)
	if state != "" {
		suffix := styleMuted(" · ", r.options) + styleState(escapeDisplayLine(state), r.options)
		if displayCellWidth(line+suffix) <= r.options.columns() {
			r.line(line + suffix)
			return
		}
		r.wholeLine(line)
		r.styledParagraph(escapeDisplayLine(state), "    ", "state")
		return
	}
	r.wholeLine(line)
}

func (r *displayRenderer) table(headers []string, rows [][]string, style tableStyle) {
	widths := displayWidths(append([][]string{headers}, rows...))
	if r.options.ASCII {
		for _, row := range append([][]string{headers}, rows...) {
			for col, cell := range row {
				widths[col] = max(widths[col], displayCellWidth(presentationASCII(escapeDisplayLine(cell))))
			}
		}
	}
	total := 2 + max(0, len(widths)-1)*3
	for _, width := range widths {
		total += width
	}
	stateColumn := func(column int) bool {
		for _, index := range style.States {
			if index == column {
				return true
			}
		}
		return false
	}
	if total > r.options.columns() && len(headers) == 0 {
		for _, row := range rows {
			r.paragraph(strings.Join(row, "  "), "    ", "plain")
		}
		return
	}
	if total > r.options.columns() && len(headers) > 0 {
		for n, row := range rows {
			if n > 0 {
				r.gap()
			}
			headedState := -1
			if style.Identity >= 0 && style.Identity < len(row) {
				state := ""
				for _, col := range style.States {
					if col < len(row) {
						state = row[col]
						headedState = col
						break
					}
				}
				r.resourceHeading(style.Kind, row[style.Identity], state)
			}
			var values []field
			for col, value := range row {
				if col >= len(headers) || col == style.Identity || col == headedState {
					continue
				}
				values = append(values, field{Label: headers[col], Value: value})
			}
			r.fields(values, true, false)
		}
		return
	}
	printRow := func(row []string, heading bool) {
		last := len(row) - 1
		for last >= 0 && row[last] == "" {
			last--
		}
		var line strings.Builder
		line.WriteString("  ")
		for col := 0; col <= last; col++ {
			value := escapeDisplayLine(row[col])
			if r.options.ASCII {
				value = presentationASCII(value)
			}
			styled := value
			if heading || style.Identity >= 0 {
				styled = styleMuted(value, r.options)
			}
			if !heading && col == style.Identity {
				styled = styleIdentity(value, r.options)
			} else if !heading && stateColumn(col) {
				styled = styleState(value, r.options)
			}
			line.WriteString(styled)
			if col < last {
				line.WriteString(strings.Repeat(" ", widths[col]-displayCellWidth(value)+3))
			}
		}
		r.line(line.String())
	}
	if len(headers) > 0 {
		printRow(headers, true)
	}
	for _, row := range rows {
		printRow(row, false)
	}
}

func displayWidths(rows [][]string) []int {
	var widths []int
	for _, row := range rows {
		for index, cell := range row {
			for index >= len(widths) {
				widths = append(widths, 0)
			}
			widths[index] = max(widths[index], displayCellWidth(escapeDisplayLine(cell)))
		}
	}
	return widths
}

func displayRow(row []string, widths []int) string {
	var text strings.Builder
	last := len(row) - 1
	for last >= 0 && row[last] == "" {
		last--
	}
	for index := 0; index <= last; index++ {
		cell := escapeDisplayLine(row[index])
		text.WriteString(cell)
		if index != last {
			text.WriteString(strings.Repeat(" ", widths[index]-displayCellWidth(cell)+displayGap))
		}
	}
	return text.String()
}
