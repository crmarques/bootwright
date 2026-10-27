package cli

import "strings"

func lifecycleActionLabel(label, identity string, opts PresentationOptions) string {
	text := escapeDisplayLine(label)
	if identity == "" {
		return text
	}
	escaped := escapeDisplayLine(identity)
	for from := 0; from <= len(text)-len(escaped); {
		at := strings.Index(text[from:], escaped)
		if at < 0 {
			break
		}
		at += from
		end := at + len(escaped)
		if (at == 0 || !isNameByte(text[at-1])) && (end == len(text) || !isNameByte(text[end])) {
			return text[:at] + styleIdentity(escaped, opts) + text[end:]
		}
		from = at + 1
	}
	return text
}

func isNameByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '-' || b == '_' || b == '.'
}
