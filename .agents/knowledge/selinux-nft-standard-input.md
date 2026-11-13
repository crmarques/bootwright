# SELinux keeps nft from reading systemd's standard input

A routing unit can exit successfully without loading a table when its
confined `nft` process cannot read the memfd systemd supplies for
`StandardInputText=` or `StandardInputData=`. In SELinux policies that
transition `iptables_exec_t` into `iptables_t`, the process may not inherit
that `tmpfs_t` descriptor. An empty standard input then makes `nft -f -`
load nothing and exit 0. An unprivileged namespace test does not exercise
this SELinux boundary.

A ruleset created under `/etc/bootwright/routing/` uses the directory's
default file label and can be read under policies that permit `iptables_t`
to read `etc_t`. Stage it in the same directory and rename it into place;
Bootwright does not relabel host files.

Decision: the routing unit loads an owned ruleset file under
`/etc/bootwright/routing/` with `nft -f <file>` and lists the table after each
load, so a load that leaves nothing fails the unit
([routing table](../../specs/substrates.md#routing-table)). Feed no confined
program its input through `StandardInput*=`; give it a file in a path its
domain reads, and never prove a load by the exit status alone.

Code:
[libvirt_routing.py](../../ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/libvirt_routing.py);
tests
`test_the_unit_reads_no_standard_input_and_its_ruleset_file_is_the_mark_then_the_ruleset`
and
`test_a_host_holding_the_earlier_standard_input_unit_with_no_table_converges_by_reloading`
in
[test_libvirt_routing.py](../../ansible/collections/ansible_collections/bootwright/core/tests/unit/plugins/module_utils/test_libvirt_routing.py);
item [B1335](../../specs/milestones/delivered.md#x444--owner-acceptance-of-validated-operator-gates).
