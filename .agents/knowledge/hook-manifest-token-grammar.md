# Add-on hook manifest token grammar (whole-scalar only)

**Whole-scalar tokens, no interpolation.** Hook manifest templates use a
whole-scalar token grammar (`internal/addons/hooks/tokens.go`): a token —
kinds `cluster`, `output`, `input`, `secret`, `exportDetails` — must be the
*entire* YAML scalar value `"{{ kind arg }}"`. Interpolation into a larger
string is deliberately rejected: `tokenRe` anchors `^...$` after trimming.
`ExtractTokens` ignores scalars that merely contain `{{` without being a
whole-scalar token.

**Why whole-scalar.** `RenderManifest` replaces whole scalars with the
programmatic value and re-marshals through yaml, so a multi-line JSON payload
(for example injected external-cluster details) survives intact with no
escaping or injection concerns — impossible if tokens could sit mid-string.
A `tokens_test` case pins this. Validation catches unknown token kinds/args,
and render fails only when the grammar cannot resolve a token.
