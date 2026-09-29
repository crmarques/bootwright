# Bootwright core

This collection is embedded in Bootwright. Controller entrypoints consume only
the frozen request supplied by the Go application service. They are not public
operator playbooks. The repository Controller and architecture specifications
own their request, result, authorization, and recovery contracts.

The runtime runs the one qualified ansible-core minor that `meta/runtime.yml`
declares, and Bootwright's embedded configuration makes loading it under any
other minor an error. The bound never starts below 2.19, where the explicit
template trust and boolean conditional behavior the collection relies on
begin, as the
[2.19 porting guide](https://docs.ansible.com/projects/ansible-core/2.19/porting_guides/porting_guide_core_2.19.html)
describes. Bootwright resolves the latest patch of that minor before approval,
and the repository Ansible check lock pins the same minor for development
tests; the lock does not pin the installed controller release.
