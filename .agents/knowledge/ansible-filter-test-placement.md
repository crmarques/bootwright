# Ansible filter test placement

**Symptom:** `ansible-lint` repeatedly reports that
`bootwright.core.test_credentials`, `bootwright.core.test_network`, or
`bootwright.core.test_redfish` has no `FilterModule`, even though lint completes
with zero findings.

**Cause:** Ansible treats every Python module under `plugins/filter` as a
runtime filter plugin. Unit-test modules in that directory are imported during
collection discovery and rejected because tests do not expose `FilterModule`.

**Constraint:** keep filter unit tests under
`tests/unit/plugins/filter`, outside the runtime plugin tree. `make python-test`
discovers them there, callback tests under the sibling `callback` suite, and
module tests under the sibling `modules` suite; each test loads its authored
plugin implementation from `plugins/<type>`. The Makefile must explicitly
enumerate every test-bearing sibling because unittest does not recurse into
non-package directories. The bundle builder excludes `test_*.py` files
regardless of their source-tree location.
