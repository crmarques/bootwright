package logview

import (
	"strings"
	"time"
	"unicode/utf8"
)

const noValue = "-"

func RenderEvents(events []Event, fullTimes bool) []string {
	blockWidth, eventWidth := 0, 0
	for _, event := range events {
		blockWidth = max(blockWidth, utf8.RuneCountInString(blockOf(event)))
		eventWidth = max(eventWidth, utf8.RuneCountInString(event.Event))
	}
	lines := make([]string, 0, len(events))
	for _, event := range events {
		lines = append(lines, renderEvent(event, blockWidth, eventWidth, fullTimes))
	}
	return lines
}

func followWidth(blocks []string) int {
	width := len(noValue)
	for _, block := range blocks {
		width = max(width, utf8.RuneCountInString(block))
	}
	return width
}

func renderEvent(event Event, blockWidth, eventWidth int, fullTimes bool) string {
	detail := event.Detail
	if event.Group != "" {
		detail = strings.TrimSuffix(event.Group+" "+event.Detail, " ")
	}
	line := pad(timeOf(event, fullTimes), timeWidth(fullTimes)) + "  " + pad(blockOf(event), blockWidth) + "  " + pad(event.Event, eventWidth) + "  " + detail
	return strings.TrimRight(line, " ")
}

func timeOf(event Event, fullTimes bool) string {
	stamp, err := time.Parse(time.RFC3339, event.Time)
	switch {
	case err != nil:
		return noValue
	case fullTimes:
		return event.Time
	}
	return stamp.UTC().Format(time.TimeOnly)
}

func timeWidth(fullTimes bool) int {
	if fullTimes {
		return len("2006-01-02T15:04:05Z")
	}
	return len(time.TimeOnly)
}

func blockOf(event Event) string {
	if event.Block == "" {
		return noValue
	}
	return event.Block
}

func pad(value string, width int) string {
	return value + strings.Repeat(" ", max(width-utf8.RuneCountInString(value), 0))
}
