# Jinja rejects comprehensions — use filter chains and accumulator set_facts

**Symptom:** A template that reads naturally as a Python list/dict comprehension
(`{{ [x.name for x in items if x.enabled] }}`, or a nested `for … for …`) fails
to render, or is rejected outright, in the deployed Ansible templating path.

**Root cause:** Jinja is not Python. The Jinja expression grammar has no
list/dict/set comprehension and no multi-`for` generator syntax; comprehensions
are a Python-language feature that Jinja never adopted. Templating that looks
like a comprehension is a syntax error, not a value error — so it cannot be
worked around by quoting or defaulting.

**Fix:** Express the transform as a filter chain, and where a comprehension would
have nested or accumulated, split it across `set_fact` steps:

- map/extract:      `items | map(attribute='name') | list`
- filter:           `items | selectattr('enabled') | list`
- reject:           `items | rejectattr('type', 'equalto', 'x') | list`
- flatten one level: `groups | map('extract', hostvars) | list`
- accumulate across a loop or a second dimension: `set_fact` an accumulator var
  and fold into it (`{{ acc + [item] }}`) over a `loop`, rather than writing a
  nested comprehension in one expression.

Keep each `set_fact` template to a single filter chain; when the logic wants a
second `for`, that is the signal to introduce an intermediate `set_fact` rather
than to reach for comprehension syntax the templater does not accept.
