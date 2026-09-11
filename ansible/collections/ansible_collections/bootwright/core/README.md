# Bootwright core

This collection is embedded in Bootwright. Controller entrypoints consume only
the frozen request supplied by the Go application service. They are not public
operator playbooks. The repository Controller and architecture specifications
own their request, result, authorization, and recovery contracts.

The runtime requires ansible-core 2.19 or later for its explicit template trust
and boolean conditional behavior, described in the
[2.19 porting guide](https://docs.ansible.com/projects/ansible-core/2.19/porting_guides/porting_guide_core_2.19.html).
Bootwright resolves the latest compatible release or the requested override
before approval. The exact versions in the repository Ansible check lock are
development test dependencies; they do not pin the installed controller release.
