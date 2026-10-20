"""Frozen VIP and boot readiness gates refuse before advancing installation."""

import pathlib

from ansible.parsing.dataloader import DataLoader
from ansible.template import Templar

ROLE = pathlib.Path(__file__).resolve().parents[2] / 'roles' / 'containercluster_install_agent'
LOADER = DataLoader()


def tasks(file):
    return LOADER.load_from_file(str(ROLE / 'tasks' / file), trusted_as_template=True)


def test_arp_reply_refusal_names_address_slot_and_hardware():
    task = next(t for t in tasks('preboot_vips.yml') if 'ansible.builtin.fail' in t)
    scope = {'item': {'item': {'address': '192.0.2.40', 'slot': 'api'}, 'rc': 1,
                      'stdout': 'Unicast reply from 192.0.2.40 [52:54:00:12:34:56]'}}
    text = Templar(loader=LOADER, variables=scope).template(task['ansible.builtin.fail']['msg'])
    assert all(value in text for value in ['192.0.2.40', 'api', '52:54:00:12:34:56'])


def test_arp_probes_only_fresh_boots_and_declared_bridges():
    task = next(t for t in tasks('preboot_vips.yml') if 'ansible.builtin.command' in t)
    assert task['ansible.builtin.command']['argv'][:7] == ['/usr/sbin/arping', '-D', '-c', '3', '-w', '3', '-I']
    guard = task['when'][1]
    for powered, expected in [([], True), (['one'], False)]:
        scope = {'containercluster_install_agent_state': {'powered': powered, 'ownMedia': ['one']}}
        assert Templar(loader=LOADER, variables=scope).evaluate_expression(guard) is expected


def test_readiness_failure_leads_with_unreachable_machine():
    task = next(t for t in tasks('readiness.yml') if 'ansible.builtin.fail' in t)
    scope = {'containercluster_install_agent_ssh_ready': {'results': [
        {'item': {'machine': 'one'}}, {'item': {'machine': 'two', 'address': '192.0.2.22'}, 'msg': 'Timeout'}]},
        'containercluster_install_agent_boot_spent_hint': 'boot budget spent'}
    text = Templar(loader=LOADER, variables=scope).template(task['ansible.builtin.fail']['msg'])
    assert 'Machine two (192.0.2.22) never answered TCP 22.' in text


def test_rendezvous_gate_skips_replayed_nodes():
    task = [t for t in tasks('readiness.yml') if 'ansible.builtin.wait_for' in t][1]
    scope = {'item': {'machine': 'one', 'address': '192.0.2.10'},
             'bootwright_cluster_install_request': {'rendezvous': '192.0.2.10'},
             'containercluster_install_agent_state': {'powered': ['one'], 'ownMedia': ['one']}}
    templar = Templar(loader=LOADER, variables=scope)
    assert templar.evaluate_expression(task['when'][0]) is True
    assert templar.evaluate_expression(task['when'][1]) is False


def test_arp_precedes_boot_mark_and_readiness_precedes_bootstrap():
    apply = tasks('apply.yml')

    def index(predicate):
        return next(i for i, task in enumerate(apply) if predicate(task))

    assert index(lambda t: t.get('ansible.builtin.import_tasks') == 'preboot_vips.yml') < index(
        lambda t: '.bootwright-booted' in str(t.get('ansible.builtin.copy', {})))
    assert index(lambda t: t.get('ansible.builtin.import_tasks') == 'readiness.yml') < index(
        lambda t: t.get('vars', {}).get('containercluster_install_agent_milestone') == 'bootstrap-complete')


def test_partial_destroy_retires_only_successfully_ejected_tokens():
    release = next(t for t in tasks('destroy.yml') if 'always' in t)
    assert release['block'][0]['bootwright.core.redfish_boot']['operation'] == 'eject'
    assert 'ansible.builtin.fail' in release['block'][1]
    task = release['always'][0]
    for result, expected in [({'changed': True}, True), ({'msg': 'unknown eject'}, False)]:
        scope = {'containercluster_install_agent_cleanup_index': 0,
                 'containercluster_install_agent_ejected': {'results': [result]}}
        assert Templar(loader=LOADER, variables=scope).evaluate_expression(task['when'][2]) is expected


def test_readiness_ports_share_one_deadline_and_spent_budget_is_zero(monkeypatch):
    import datetime
    from types import SimpleNamespace
    from ansible._internal._templating import _jinja_plugins

    class Clock(datetime.datetime):
        @classmethod
        def now(cls, tz=None):
            return cls(2026, 1, 1, tzinfo=tz)

    monkeypatch.setattr(_jinja_plugins, 'datetime', SimpleNamespace(datetime=Clock, timezone=datetime.timezone))

    waits = [t['ansible.builtin.wait_for'] for t in tasks('readiness.yml') if 'ansible.builtin.wait_for' in t]
    assert [task['port'] for task in waits] == [22, 8090]
    assert waits[0]['timeout'] == waits[1]['timeout']
    epoch = int(Clock.now(datetime.timezone.utc).timestamp())
    for deadline, low, high in [(epoch + 20, 20, 20), (epoch - 1, 0, 0)]:
        templar = Templar(loader=LOADER, variables={'containercluster_install_agent_boot_deadline': deadline})
        for wait in waits:
            assert low <= templar.template(wait['timeout']) <= high
            assert wait['connect_timeout'] == 1
