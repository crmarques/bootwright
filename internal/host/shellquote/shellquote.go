package shellquote

import "strings"

func Quote(args []string) string {
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == "" {
			quoted = append(quoted, "''")
			continue
		}
		if strings.ContainsAny(arg, " \t\n'\"$`\\") {
			quoted = append(quoted, "'"+strings.ReplaceAll(arg, "'", "'\\''")+"'")
			continue
		}
		quoted = append(quoted, arg)
	}
	return strings.Join(quoted, " ")
}

func QuoteWord(value string) string {
	if value == "" {
		return "''"
	}
	if isShellSafeWord(value) {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func QuoteWords(values []string) string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, QuoteWord(value))
	}
	return strings.Join(out, " ")
}

func isShellSafeWord(value string) bool {
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case strings.ContainsRune("_@%+=:,./-", r):
		default:
			return false
		}
	}
	return true
}
