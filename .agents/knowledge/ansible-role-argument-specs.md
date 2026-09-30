# ansible-core role argument specs

Observed on ansible-core 2.21.4, the version `./scripts/ansible-check`
installs (`scripts/tools/ansible-requirements.txt`), on 2026-09-29. Production
resolves the latest 2.21 patch, so later 2.21 patches are UNVERIFIED here.
[Substrates](../../specs/substrates.md#adapter-boundary) owns the rule this
informs: the machine port is a set of role entry points whose inputs
ansible-core validates.

## Which include is validated

A role's `meta/argument_specs.yml` declares entry points by key. An
`include_role` or `import_role` stores its `tasks_from` exactly as written
([role_include.py line 148](https://github.com/ansible/ansible/blob/v2.21.4/lib/ansible/playbook/role_include.py#L148)),
and the role looks its specification up by that string
([role/\_\_init\_\_.py lines 360-361](https://github.com/ansible/ansible/blob/v2.21.4/lib/ansible/playbook/role/__init__.py#L360-L361)).
So `tasks_from: pre_boot` finds the `pre_boot` entry point and prepends a
validation task, while `tasks_from: pre_boot.yml` finds none and runs the same
file unvalidated. A scratch playbook confirmed both: the extension ran the
entry point's first task with a required input missing, and the key refused it
with `missing required arguments`. Include an entry point by its key, and test
that every consumer does, as the collection's
[test_substrate_port.py](../../ansible/collections/ansible_collections/bootwright/core/tests/unit/test_substrate_port.py) does.

## What the validation does

The prepended task
([role/\_\_init\_\_.py lines 374-403](https://github.com/ansible/ansible/blob/v2.21.4/lib/ansible/playbook/role/__init__.py#L374-L403))
runs `validate_argument_spec`, is tagged `always` (line 402) and carries no
`no_log`. The action reads each declared name from the task variables and
renders it
([validate_argument_spec.py lines 18-35](https://github.com/ansible/ansible/blob/v2.21.4/lib/ansible/plugins/action/validate_argument_spec.py#L18-L35)),
then validates what it rendered
([lines 78-80](https://github.com/ansible/ansible/blob/v2.21.4/lib/ansible/plugins/action/validate_argument_spec.py#L78-L80)).
Observed in the same scratch playbook:

- A role default is a task variable, so it satisfies a `required` option. A
  required input must therefore have no role default, or a consumer that forgot
  it is never refused.
- A type error echoes the value it refused, in both `msg` and
  `argument_errors`: a `bool` given `secret-looking-value-7d1f` printed that
  string. Because the task has no `no_log`, pass a credential as the path of
  its material file, never as its bytes.
- The validator accepts `''` for a required `str` and `[]` for a required
  `list`. `required` proves only that a consumer passed the input; an entry
  point that needs a non-empty value asserts it itself.
- `ignore_errors: true` on the include does not cover a refused validation, and
  the play stopped there. A `rescue` around the include does catch it.

That test renders every consumer's include of the machine port and validates
it with `ArgumentSpecValidator`, as this action does, rather than waiting for a
host to refuse it.
