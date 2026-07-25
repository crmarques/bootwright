# virtctl client version output parsing

**Symptom:** Controller virtctl provisioning reports that the installed client
does not match the host cluster KubeVirt version even though the assertion
message shows the same `GitVersion` in `Client Version: version.Info{...}`.

**Root cause:** Ansible's `regex_search` filter returns a list when called with
a capture-group argument. The role compared that one-element list with the
KubeVirt API's scalar server-version string, so equal version text still failed
the exact-match assertion.

**Fix:** The `bootwright_virtctl_version` collection filter extracts
`GitVersion` as a scalar string. Both the pre-install decision and post-install
verification use the same filter, and unrecognized output resolves to an empty
string so provisioning still fails closed.
