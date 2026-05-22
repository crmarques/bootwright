# sudo / ansible Duplicate become password prompt

**Symptom:** ansible prints `Duplicate become password prompt` or hangs mid-play waiting for a password that is never answered (or is answered wrong and silently rejected).

**Root cause:** The bootwright process has neither root privileges nor passwordless sudo. Ansible's per-task `become: true` fires a password prompt that stdin cannot satisfy when the process was launched non-interactively.

**Fix / invariant:** Scoped apply and destroy commands expose `--ask-become-pass`. Two invocation patterns are supported:
1. Run as root: `sudo bootwright apply <target>`
2. NOPASSWD sudo configured for the invoking user, with `--ask-become-pass=false`

Remote provider hosts are not probed from the controller — their sudo policy lives on the target box. The play surfaces the failure there if NOPASSWD is missing.
