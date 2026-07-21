# `from_json` on empty stdout throws — guard with `default('...', true)`

**Symptom:** A task pipes a command's `stdout` into `from_json` and the play
fails with a JSON parse error (`Expecting value: line 1 column 1 (char 0)`) even
though the task has `failed_when: false` / `ignore_errors` and the play was
"supposed" to tolerate the command failing.

**Root cause:** When a `command`/`shell` task returns a non-zero rc with empty
output, `stdout` is the empty string `''`, which is a *defined* value, not
undefined. `'' | from_json` is evaluated and throws, because the empty string is
not valid JSON. A plain `default('{}')` does **not** help: `default` only
substitutes for *undefined* values, and `''` is defined, so the empty string
flows straight into `from_json`. The same trap hits eager `fail_msg`/`assert`
expressions that reference the parsed value.

**Fix:** Use the two-argument form `default('{}', true)` (the second argument
`true` makes `default` also substitute for values that are merely *falsey* —
including `''`) before `from_json`:

```yaml
{{ result.stdout | default('{}', true) | from_json }}
```

Apply the same guard to any list-shaped payload (`default('[]', true)`). Never
assume a tolerated command produced parseable stdout; a skipped or failed command
leaves `''`, and `from_json` will throw on it.
