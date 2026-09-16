package cli

import (
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// The shared human layout indents every group body by one unit and separates
// aligned columns by one gap. Both are fixed so unrelated commands cannot drift.
const (
	displayIndent = "  "
	displayGap    = 2
)

// display accumulates one command's human result in the shared layout: an
// optional headline, titled sections, aligned label/value fields and aligned
// columns. Every caller-supplied value crosses the display boundary here, so
// callers pass raw values and never pre-escape or pre-pad them.
type display struct{ text strings.Builder }

type field struct{ Label, Value string }

func (d *display) headline(status, text string) {
	d.separate()
	if status != "" {
		fmt.Fprintf(&d.text, "[%s] %s\n", status, escapeDisplayLine(text))
		return
	}
	fmt.Fprintf(&d.text, "%s\n", escapeDisplayLine(text))
}

// section opens a group. A titled group prints its heading; an untitled group
// only separates its fields from the preceding block.
func (d *display) section(title string) {
	d.separate()
	if title != "" {
		fmt.Fprintf(&d.text, "%s\n", escapeDisplayLine(title))
	}
}

func (d *display) separate() {
	if d.text.Len() != 0 {
		d.text.WriteByte('\n')
	}
}

func (d *display) fields(values ...field) {
	rows := make([][]string, 0, len(values))
	for _, value := range values {
		rows = append(rows, []string{value.Label, value.Value})
	}
	d.rows(rows)
}

// rows aligns each column to its widest cell. Trailing padding is never
// written, so a short final cell cannot leave invisible whitespace.
func (d *display) rows(rows [][]string) {
	widths := displayWidths(rows)
	for _, row := range rows {
		d.text.WriteString(displayIndent)
		d.text.WriteString(displayRow(row, widths))
		d.text.WriteByte('\n')
	}
}

// table prints an aligned column header above its rows. The header participates
// in column width so a wide heading never collides with its values.
func (d *display) table(headers []string, rows [][]string) {
	widths := displayWidths(append([][]string{headers}, rows...))
	d.text.WriteString(displayRow(headers, widths))
	d.text.WriteByte('\n')
	for _, row := range rows {
		d.text.WriteString(displayRow(row, widths))
		d.text.WriteByte('\n')
	}
}

// step is one numbered planned change with the effects it causes. An effect
// belongs to its step, so an operator never has to guess which change in the
// list produces the one they are reading.
type step struct {
	Text    string
	Effects []string
}

// steps numbers an ordered list. The marker column is padded so a two-digit
// step keeps its text aligned, and a single space follows it as prose expects.
// Effects are indented one unit past their step's text.
func (d *display) steps(items []step) {
	width := 0
	markers := make([]string, len(items))
	for index := range items {
		markers[index] = fmt.Sprintf("%d.", index+1)
		if size := utf8.RuneCountInString(markers[index]); size > width {
			width = size
		}
	}
	effectIndent := displayIndent + strings.Repeat(" ", width+1) + displayIndent
	for index, item := range items {
		d.text.WriteString(displayIndent)
		d.text.WriteString(markers[index])
		d.text.WriteString(strings.Repeat(" ", width-utf8.RuneCountInString(markers[index])+1))
		d.text.WriteString(escapeDisplayLine(item.Text))
		d.text.WriteByte('\n')
		for _, effect := range item.Effects {
			d.text.WriteString(effectIndent)
			d.text.WriteString(escapeDisplayLine(effect))
			d.text.WriteByte('\n')
		}
	}
}

func (d *display) lines(items []string) {
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		rows = append(rows, []string{item})
	}
	d.rows(rows)
}

func (d *display) writeTo(out io.Writer) error {
	text := d.text.String()
	if n, err := io.WriteString(out, text); err != nil || n != len(text) {
		if err == nil {
			err = io.ErrShortWrite
		}
		return err
	}
	return nil
}

func displayWidths(rows [][]string) []int {
	var widths []int
	for _, row := range rows {
		for index, cell := range row {
			for index >= len(widths) {
				widths = append(widths, 0)
			}
			if width := utf8.RuneCountInString(escapeDisplayLine(cell)); width > widths[index] {
				widths[index] = width
			}
		}
	}
	return widths
}

func displayRow(row []string, widths []int) string {
	var text strings.Builder
	last := -1
	for index, cell := range row {
		if cell != "" {
			last = index
		}
	}
	for index := 0; index <= last; index++ {
		cell := escapeDisplayLine(row[index])
		text.WriteString(cell)
		if index == last {
			break
		}
		text.WriteString(strings.Repeat(" ", widths[index]-utf8.RuneCountInString(cell)+displayGap))
	}
	return text.String()
}
