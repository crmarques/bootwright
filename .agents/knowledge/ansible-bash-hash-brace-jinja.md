# Never use Bash ${#...} in shell scripts Ansible may template

**Constraint:** `eject_libvirt_media.sh`
(`container_cluster_media_libvirt/files/`) must never contain the Bash
length expansion `${#var}` / `${#arr[@]}`. Ansible's Jinja templating parses
`{#` as a Jinja comment opener, which silently swallows script text from
that point on when the content passes through templating.

**Enforcement:** `TestBootRedfishLibvirtVirtualMediaDetachFallback` in
`internal/repo/checks/ansible_boot_redfish_test.go` fails with
`libvirt media cleanup script uses Bash syntax that Ansible parses as a
Jinja comment` if `${#` appears in the script.

**Workaround used:** count with an explicit counter variable (see
`target_count` incremented in a loop) instead of `${#targets[@]}`. Apply the
same rule to any new shell content that Ansible renders or inlines.
