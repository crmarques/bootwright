# shellquote: Quote is for display, QuoteWord is for execution

**Constraint:** `internal/host/shellquote` has two quoting levels with
different safety guarantees. `Quote(args)` is display-oriented and uses
a denylist (whitespace and `' " $ ` + backslash); `QuoteWord`/`QuoteWords`
are for security-sensitive shell embedding (export values, ssh command
arguments) and use a conservative allowlist (`[A-Za-z0-9]` plus
`_@%+=:,./-`), so shell-active characters the denylist misses
(`~ * ; & | < > ( )`) are still neutralised.

**Why:** A display string only needs to read unambiguously; a word
embedded in a generated command line that a shell executes must be inert
under every expansion the shell performs.

**When it bites:** Using `Quote` for a value that ends up in an executed
command line (an ssh remote command, an `export NAME=value` line) lets a
crafted or merely unlucky value (`~`, `*`, `;`) expand or chain
commands. Use `QuoteWord` for anything embedded in a generated command
line that executes.
