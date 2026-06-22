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
