// Package shellquote renders argv into a shell-safe single-line string. It is a
// generic host primitive (no domain knowledge), so the orchestration engine,
// access summaries, and any other caller share one quoting implementation
// instead of redefining it.
package shellquote

import "strings"

// Quote returns a shell-safe representation of argv suitable for display or for
// embedding in a generated command line: empty args become ”, and args
// containing whitespace or shell metacharacters are single-quoted with embedded
// single quotes escaped.
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

// QuoteWord quotes a single value for security-sensitive shell embedding (an
// export value, an ssh command argument). Unlike Quote it uses a conservative
// allowlist: anything outside [A-Za-z0-9] and a small safe-punctuation set is
// single-quoted, so shell-active characters Quote's display-oriented denylist
// misses (~ * ; & | < > ( ) …) are still neutralised.
func QuoteWord(value string) string {
	if value == "" {
		return "''"
	}
	if isShellSafeWord(value) {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

// QuoteWords applies QuoteWord to each value and joins the results with spaces.
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
