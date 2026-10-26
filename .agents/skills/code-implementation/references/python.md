# Python Engineering

[Architecture](../../../../specs/architecture.md) owns
[which language holds which logic](../../../../specs/architecture.md#go-and-ansible-responsibility-boundary)
and the [plugin contract](../../../../specs/architecture.md#ansible-collection-plugins-and-results);
[security](../../../../specs/security.md) owns process, secret and network
boundaries; [source formatting](formatting.md) owns layout. This reference adds
Python-specific guidance for the embedded collection's plugins and module
utilities, and for the repository tools under `scripts/tools/`. Role YAML and
collection tests follow the [Ansible reference](ansible.md).

## Placement

Python in the collection answers one question: how one already-selected effect
is reached and proved on its target. It holds protocol publication, native
transport, bounded native-API clients, result normalization, the evidence
fields of a declared postcondition, and a fixed, spec-owned procedure inside
one frozen request. It never chooses product workflow or a variant from
authored data, orders work across blocks, authorizes, writes operation state or
judges whether evidence proves a postcondition: Go does.

## Execution sites

Each file runs at exactly one site; know which before editing it.

- Action plugins run on the controller, in the runtime venv the
  [controller](../../../../specs/controller.md) builds from the locked runtime.
- Modules and the module utilities they import run on the managed host under
  its own interpreter, which may be as old as the managed-host floor
  (`REMOTE_PYTHON_FLOOR` in
  `ansible/collections/ansible_collections/bootwright/core/tests/unit/test_remote_python_floor.py`,
  pinned by `scripts/tools/ansible-check-floor-interpreter.json`). Start every
  such file with `from __future__ import annotations`, use no syntax or library
  call the floor lacks, and import nothing beyond the standard library,
  `ansible.module_utils` and the collection except a library the target system
  itself provides.
- A fixed worker shipped as source and run as `/usr/bin/python3 -I -c`, often
  through `sudo -n`, imports nothing beyond the target's standard library that
  its own shipped source does not carry; so does a script run inside a
  component's image.
- Repository tools are covered [below](#repository-tools).

## Implementation

- Validate the frozen request before any effect: closed key sets, exact types
  and bounds. Check a JSON integer with `type(value) is int`, because
  `isinstance` admits `True`; check a Boolean with `type(value) is bool`.
- Start a process only with an absolute executable and an argv list, never a
  shell; bound its output and its time, and reap it. A remote command string
  carries only literals and validated, quoted tokens.
- Reuse the canonical JSON encoder and the bounded process runner already at
  the file's site instead of writing another; their consolidation is tracked as
  [B818](../../../../specs/milestones/backlog.md#b818).
- Publish adapter records only through `plugins/module_utils/adapter_protocol.py`.
- Take every endpoint, trust anchor and credential from the request or the
  runner, never from ambient proxy variables or user configuration: an opener
  is built with an empty `ProxyHandler`.
- Keep every function within the collection's length limit
  (`test_function_length.py` in the collection's unit tests).
- A change to what an effect is moves its implementation's content digest, as
  the [Ansible reference](ansible.md) says.

## Tests and gates

Collection tests live under the collection's `tests/unit/`. The probe is one
file:
`./scripts/ansible-check --suite units <path to a test_*.py file>`. The gate's
units suite runs every test on the controller's Python and again runs the
module and module-utility tests under the pinned floor interpreter; its sanity
suite imports every module under both. Report unavailable, skipped, flaky and
failed checks under the shared workflow.

## Repository tools

A shell script under `scripts/` only resolves a pinned tool and executes it;
selection and parsing logic belongs in a launcher under `scripts/tools/`.
`scripts/quick-test` and `scripts/check-commits` are named exceptions until
their logic moves behind a tested launcher, and the runtime hook
`scripts/hooks/stop-check` stays a short POSIX `sh` guard. A launcher uses only
the standard library, runs on the host's CPython 3.12 or newer, and runs every
check it dispatches through a pinned wrapper or the Ansible check environment.
A launcher's tests sit beside it as `*_test.py`; one that needs a locked
library runs in that environment.
