# Ansible Callback Skipped Dispatch Warning

**Symptom:** During `bootwright apply`, Ansible can print:
`Callback dispatch 'v2_runner_on_skipped' failed for plugin 'default':
CallbackModule.v2_runner_on_skipped() takes 2 positional arguments but 4 were
given`. It may appear near skipped tasks such as
`host_proxy : Build proxy environment facts`.

**Root cause:** The managed controller environment used an unstable
`ansible-core` 2.21 pin. In that line, callback dispatch and the bundled
`default` callback can disagree on the skipped-task callback signature.

**Fix:** Pin Bootwright-managed `ansible-core` to the latest stable 2.20.x
release. Recreate the managed Ansible venv when the installed package version
does not match the rendered component pin.
