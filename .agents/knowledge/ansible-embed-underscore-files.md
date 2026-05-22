# Go embed: underscore/dot-prefixed files silently dropped

**Symptom:** Ansible collection modules are missing from the extracted bundle — `_respawn.py`, `__init__.py`, or other files whose names start with `_` or `.` are not found at runtime.

**Root cause:** The default Go `//go:embed` pattern silently skips files and directories whose names begin with `_` or `.`. Collections like `ansible.posix` ship `module_utils/_respawn.py` and `__init__.py`, which are dropped without the `all:` prefix.

**Fix:** Use `//go:embed all:bundle` (see `internal/embedded/ansible.go`). The `all:` modifier includes underscore- and dot-prefixed entries that the default pattern excludes.
