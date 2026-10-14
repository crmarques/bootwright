package cli

import (
	"strconv"
)

const (
	unifiedCellBound = 4_000_000
	unifiedKeep      = ' '
	unifiedDelete    = '-'
	unifiedInsert    = '+'
)

type unifiedLine struct {
	op   byte
	text string
}

type unifiedHunk struct {
	oldStart, oldLines, newStart, newLines int
	lines                                  []unifiedLine
}

func unifiedHunks(old, new []string, context int) []unifiedHunk {
	return groupHunks(editScript(old, new), max(context, 0))
}

func editScript(old, new []string) []unifiedLine {
	prefix := 0
	for prefix < len(old) && prefix < len(new) && old[prefix] == new[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(old)-prefix && suffix < len(new)-prefix && old[len(old)-1-suffix] == new[len(new)-1-suffix] {
		suffix++
	}
	script := make([]unifiedLine, 0, len(old)+len(new)-prefix-suffix)
	for _, text := range old[:prefix] {
		script = append(script, unifiedLine{unifiedKeep, text})
	}
	script = append(script, middleScript(old[prefix:len(old)-suffix], new[prefix:len(new)-suffix])...)
	for _, text := range old[len(old)-suffix:] {
		script = append(script, unifiedLine{unifiedKeep, text})
	}
	return script
}

func middleScript(old, new []string) []unifiedLine {
	script := make([]unifiedLine, 0, len(old)+len(new))
	if len(old) != 0 && len(new) != 0 && len(old) <= unifiedCellBound/len(new) {
		return append(script, commonScript(old, new)...)
	}
	for _, text := range old {
		script = append(script, unifiedLine{unifiedDelete, text})
	}
	for _, text := range new {
		script = append(script, unifiedLine{unifiedInsert, text})
	}
	return script
}

func commonScript(old, new []string) []unifiedLine {
	width := len(new) + 1
	common := make([]int32, (len(old)+1)*width)
	for i := len(old) - 1; i >= 0; i-- {
		for j := len(new) - 1; j >= 0; j-- {
			if old[i] == new[j] {
				common[i*width+j] = common[(i+1)*width+j+1] + 1
			} else {
				common[i*width+j] = max(common[(i+1)*width+j], common[i*width+j+1])
			}
		}
	}
	var script []unifiedLine
	i, j := 0, 0
	for i < len(old) || j < len(new) {
		switch {
		case i < len(old) && j < len(new) && old[i] == new[j]:
			script = append(script, unifiedLine{unifiedKeep, old[i]})
			i, j = i+1, j+1
		case i < len(old) && (j == len(new) || common[(i+1)*width+j] >= common[i*width+j+1]):
			script = append(script, unifiedLine{unifiedDelete, old[i]})
			i++
		default:
			script = append(script, unifiedLine{unifiedInsert, new[j]})
			j++
		}
	}
	return script
}

func groupHunks(script []unifiedLine, context int) []unifiedHunk {
	var hunks []unifiedHunk
	oldBefore, newBefore := 0, 0
	for index := 0; index < len(script); {
		if script[index].op == unifiedKeep {
			oldBefore, newBefore = oldBefore+1, newBefore+1
			index++
			continue
		}
		start := max(index-context, 0)
		last := changeEnd(script, index)
		for next := nextChange(script, last); next >= 0 && next-last <= 2*context; next = nextChange(script, last) {
			last = changeEnd(script, next)
		}
		end := min(last+context, len(script))
		oldBefore, newBefore = oldBefore-(index-start), newBefore-(index-start)
		hunk := unifiedHunk{lines: script[start:end]}
		for _, line := range hunk.lines {
			hunk.oldLines += boolInt(line.op != unifiedInsert)
			hunk.newLines += boolInt(line.op != unifiedDelete)
		}
		hunk.oldStart, hunk.newStart = startLine(oldBefore, hunk.oldLines), startLine(newBefore, hunk.newLines)
		hunks = append(hunks, hunk)
		oldBefore += hunk.oldLines
		newBefore += hunk.newLines
		index = end
	}
	return hunks
}

func changeEnd(script []unifiedLine, from int) int {
	end := from
	for end < len(script) && script[end].op != unifiedKeep {
		end++
	}
	return end
}

func nextChange(script []unifiedLine, from int) int {
	for index := from; index < len(script); index++ {
		if script[index].op != unifiedKeep {
			return index
		}
	}
	return -1
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func startLine(before, lines int) int {
	if lines == 0 {
		return before
	}
	return before + 1
}

func formatUnified(oldLabel, newLabel string, hunks []unifiedHunk) []string {
	lines := []string{"--- " + oldLabel, "+++ " + newLabel}
	for _, hunk := range hunks {
		lines = append(lines, "@@ -"+strconv.Itoa(hunk.oldStart)+","+strconv.Itoa(hunk.oldLines)+" +"+strconv.Itoa(hunk.newStart)+","+strconv.Itoa(hunk.newLines)+" @@")
		for _, line := range hunk.lines {
			lines = append(lines, string(rune(line.op))+line.text)
		}
	}
	return lines
}
