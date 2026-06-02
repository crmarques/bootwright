# Bootwright Core Collection

`bootwright.core` is the embedded Ansible collection used by the Bootwright
CLI. It contains workflow playbooks, task playbooks, roles, and filter plugins
for declarative OpenShift agent-install provisioning.

Operators normally do not run this collection directly. Bootwright renders the
inventory and variables, extracts the embedded bundle, and invokes the relevant
collection playbook by fully qualified collection name.
