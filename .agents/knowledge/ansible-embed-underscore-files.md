# Go embed: underscore/dot-prefixed files silently dropped

**Symptom:** Ansible collection modules are missing from the extracted bundle — `_respawn.py`, `__init__.py`, or other files whose names start with `_` or `.` are not found at runtime.

**Root cause:** The default Go `//go:embed` pattern silently skips files and directories whose names begin with `_` or `.`. Collections like `ansible.posix` ship `module_utils/_respawn.py` and `__init__.py`, which are dropped without the `all:` prefix.

**Fix:** Use the `all:` modifier, which includes underscore- and dot-prefixed entries that the default pattern excludes.

**Where this still bites:** The Ansible collection no longer hits it — `scripts/sync-ansible-bundle.py` zips the tree and `internal/converge/bundle/ansible.go` embeds the finished archive (`//go:embed ansible_bundle*`), a plain filename the default pattern keeps. The live exposure is `add-ons/embed.go`, which embeds shipped add-on **directories** without `all:`; an add-on that ever ships an underscore- or dot-prefixed file loses it silently at build time.
