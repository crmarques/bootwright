"""Frozen VIP and boot readiness gates refuse before advancing installation.

The virtual IP proof is checked by running preboot_vips.yml as ansible-core
would (Probe below) against a fake arping that answers each probed address with
a scripted exit code and output. iputils arping in duplicate-address mode (-D)
exits 1 when any host replied and 0 when none did (finish() in arping.c,
https://github.com/iputils/iputils/blob/master/arping.c, which returns
!!received in DAD mode), and 2 when it could not probe at all: a socket, bind or
device error (error(2, ...)) or a down interface (check_ifflags exits 2).

The proof that booted nodes answer is checked by running readiness.yml the same
way (Reach below) against scripted nodes and a simulated UTC clock. ansible-core's
wait_for (modules/wait_for.py in ansible-core 2.21) sets end = start + timeout
and connects only while the clock is before end, so a timeout of 0 never
connects at all.
"""

import datetime
import pathlib

import pytest

from ansible.parsing.dataloader import DataLoader
from ansible.template import Templar

ROLE = pathlib.Path(__file__).resolve().parents[2] / 'roles' / 'containercluster_install_agent'
LOADER = DataLoader()


def tasks(file):
    return LOADER.load_from_file(str(ROLE / 'tasks' / file), trusted_as_template=True)


API = {'address': '192.0.2.40', 'bridge': 'virbr-lab', 'slot': 'api'}
INGRESS = {'address': '192.0.2.41', 'bridge': 'virbr-lab', 'slot': 'ingress'}
IPV6 = {'address': '2001:db8::40', 'bridge': '', 'slot': 'api'}
OFF_NETWORK = {'address': '198.51.100.41', 'bridge': '', 'slot': 'ingress'}
REPLY = 'ARPING 192.0.2.40 from 0.0.0.0 virbr-lab\nUnicast reply from 192.0.2.40 [52:54:00:12:34:56]  0.712ms\n'
FRESH = {'powered': [], 'ownMedia': []}


def conditions(task):
    when = task.get('when')
    return [] if when is None else list(when) if isinstance(when, list) else [when]


class Probe:
    """Runs preboot_vips.yml as ansible-core would, against a fake arping.

    A looped task evaluates its condition for each item, registers a result per
    item that carries the item, and marks a skipped item skipped; a looped fail
    fails every item whose condition holds. The fake arping answers each address
    with the (rc, stdout) scripted for it, and `probed` keeps each command line.
    """

    def __init__(self, vips, answers, state=None):
        self.facts = {'bootwright_cluster_install_request': {'vips': vips},
                      'containercluster_install_agent_state': state or FRESH}
        self.answers = answers
        self.probed = []
        self.failures = []

    def rendering(self, **local):
        return Templar(loader=LOADER, variables=dict(self.facts, **local))

    def run(self):
        for task in tasks('preboot_vips.yml'):
            if 'ansible.builtin.set_fact' in task:
                rendering = self.rendering()
                self.facts.update({name: rendering.template(value) for name, value in task['ansible.builtin.set_fact'].items()})
                continue
            results = []
            for item in self.rendering().template(task['loop']):
                rendering = self.rendering(item=item)
                if not all(rendering.evaluate_conditional(condition) for condition in conditions(task)):
                    results.append({'item': item, 'skipped': True, 'changed': False})
                elif 'ansible.builtin.command' in task:
                    results.append(dict(self.arping(task, rendering), item=item))
                elif 'ansible.builtin.fail' in task:
                    self.failures.append(rendering.template(task['ansible.builtin.fail']['msg']))
                else:
                    raise AssertionError('the simulation has no model of task %r' % task.get('name'))
            if 'register' in task:
                self.facts[task['register']] = {'results': results, 'changed': False}
        return self

    def arping(self, task, rendering):
        command = [rendering.template(value) for value in task['ansible.builtin.command']['argv']]
        assert command[:7] == ['/usr/sbin/arping', '-D', '-c', '3', '-w', '3', '-I'], command
        self.probed.append(command)
        rc, stdout = self.answers[command[-1]]
        return {'rc': rc, 'stdout': stdout, 'stderr': '', 'changed': False}


def test_an_arp_reply_refuses_naming_address_slot_and_hardware():
    probe = Probe([API, INGRESS], {API['address']: (1, REPLY), INGRESS['address']: (0, '')}).run()
    assert probe.probed == [['/usr/sbin/arping', '-D', '-c', '3', '-w', '3', '-I', 'virbr-lab', vip['address']]
                            for vip in (API, INGRESS)]
    assert probe.failures == ['virtual IP 192.0.2.40 (api) is not proved free; answering hardware addresses: 52:54:00:12:34:56.']


def test_no_arp_reply_proceeds():
    probe = Probe([API, INGRESS], {API['address']: (0, ''), INGRESS['address']: (0, '')}).run()
    assert len(probe.probed) == 2
    assert probe.failures == []


def test_a_probe_that_could_not_run_refuses_as_failed_not_free():
    probe = Probe([API], {API['address']: (2, 'Interface "virbr-lab" is down\n')}).run()
    assert probe.failures == ['virtual IP 192.0.2.40 (api) is not proved free; the ARP probe failed.']


@pytest.mark.parametrize('state', [
    {'powered': ['master-0'], 'ownMedia': ['master-0']},
    {'powered': ['master-0', 'master-1', 'master-2'], 'ownMedia': []},
    {'powered': ['master-1'], 'ownMedia': ['master-0']},
])
def test_any_powered_node_skips_every_probe(state):
    probe = Probe([API, INGRESS], {}, state=state).run()
    assert probe.probed == []
    assert probe.failures == []


def test_unprobeable_vips_are_never_probed_and_reach_completion_evidence():
    probe = Probe([API, IPV6, OFF_NETWORK], {API['address']: (0, '')}).run()
    assert [command[-1] for command in probe.probed] == [API['address']]
    assert probe.failures == []
    assert probe.facts['containercluster_install_agent_vips_not_probed'] == [IPV6['address'], OFF_NETWORK['address']]
    evidence = next(t for t in tasks('finish.yml') if t.get('bootwright.core.containercluster_install_protocol', {}).get('phase') == 'completed')
    passed = probe.rendering().template(evidence['bootwright.core.containercluster_install_protocol']['vips_not_probed'])
    assert passed == [IPV6['address'], OFF_NETWORK['address']]


NODES = [{'machine': 'master-%d' % index, 'address': '192.0.2.%d' % (20 + index)} for index in range(3)]
BOOT = 1_800_000_000


class Reach:
    """Runs readiness.yml as ansible-core would, against scripted nodes and a simulated clock.

    `answers` maps (address, port) to the boot second from which it accepts a
    connection; any other pair never does. wait_for is modelled on ansible-core
    2.21 modules/wait_for.py: end = start + timeout, and while the clock is
    before end it connects, a refused or dropped connection costing
    connect_timeout and then sleep, and once the loop ends unanswered the
    result carries msg. A fail task that fires stops the run with its message.
    """

    def __init__(self, answers, rendezvous=NODES[0]['address'], state=None, deadline=60):
        self.seconds = BOOT
        self.answers = answers
        self.waited = []
        self.facts = dict(LOADER.load_from_file(str(ROLE / 'defaults' / 'main.yml'), trusted_as_template=True))
        self.facts.update({
            'bootwright_cluster_install_request': {'nodes': NODES, 'rendezvous': rendezvous, 'budgets': {'bootSeconds': 311}},
            'containercluster_install_agent_state': state or FRESH,
            'containercluster_install_agent_boot_deadline': BOOT + deadline})

    def now(self, utc=False, fmt=None):
        assert utc is True and fmt is None, 'the readiness clock must be read in UTC'
        return datetime.datetime.fromtimestamp(self.seconds, datetime.timezone.utc).replace(tzinfo=None)

    def rendering(self, **local):
        return Templar(loader=LOADER, variables=dict(self.facts, now=self.now, **local))

    def run(self):
        for task in tasks('readiness.yml'):
            if 'ansible.builtin.set_fact' in task:
                rendering = self.rendering()
                if all(rendering.evaluate_conditional(condition) for condition in conditions(task)):
                    self.facts.update({name: rendering.template(value) for name, value in task['ansible.builtin.set_fact'].items()})
                continue
            if 'ansible.builtin.fail' in task:
                rendering = self.rendering()
                if all(rendering.evaluate_conditional(condition) for condition in conditions(task)):
                    return ' '.join(rendering.template(task['ansible.builtin.fail']['msg']).split())
                continue
            assert 'ansible.builtin.wait_for' in task, task.get('name')
            results = []
            for item in self.rendering().template(task['loop']):
                rendering = self.rendering(item=item)
                if not all(rendering.evaluate_conditional(condition) for condition in conditions(task)):
                    results.append({'item': item, 'skipped': True, 'changed': False})
                    continue
                arguments = {name: rendering.template(value) for name, value in task['ansible.builtin.wait_for'].items()}
                results.append(dict(self.wait_for(**arguments), item=item))
            self.facts[task['register']] = {'results': results, 'changed': False}
        return None

    def wait_for(self, host, port, timeout, connect_timeout, sleep):
        self.waited.append((host, port, int(timeout)))
        start = self.seconds
        while self.seconds < start + int(timeout):
            opens = self.answers.get((host, port))
            if opens is not None and self.seconds - BOOT >= opens:
                return {'changed': False, 'elapsed': self.seconds - start}
            self.seconds += connect_timeout + sleep
        return {'changed': False, 'failed': False, 'msg': 'Timeout when waiting for %s:%s' % (host, port)}


def answering(*ports, nodes=NODES, at=0):
    return {(node['address'], port): at for node in nodes for port in ports}


def test_a_node_that_never_answers_is_named_and_the_later_ones_that_answer_are_not():
    answers = answering(22, nodes=NODES[1:])
    message = Reach(answers, rendezvous=NODES[1]['address']).run()
    assert message.startswith('Machine master-0 (192.0.2.20) never answered TCP 22.')
    assert 'master-1' not in message and 'master-2' not in message


def test_nodes_that_all_answer_proceed_after_proving_only_the_rendezvous_service():
    reach = Reach(answering(22, 8090))
    assert reach.run() is None
    assert [waited[:2] for waited in reach.waited] == [
        (node['address'], 22) for node in NODES] + [(NODES[0]['address'], 8090)]


def test_a_node_answering_late_within_the_deadline_is_not_named():
    reach = Reach(answering(22, 8090) | {(NODES[2]['address'], 22): 40})
    assert reach.run() is None
    assert reach.seconds - BOOT >= 40


def test_after_the_deadline_each_node_is_still_tried_once():
    reach = Reach(answering(22, nodes=NODES[1:]), deadline=-5)
    message = reach.run()
    assert [waited[2] for waited in reach.waited] == [1, 1, 1]
    assert 'master-0' in message and 'master-1' not in message and 'master-2' not in message


def test_a_rendezvous_service_that_never_answers_is_named_with_the_reach_causes():
    message = Reach(answering(22)).run()
    assert message.startswith('Machine master-0 (192.0.2.20) never answered TCP 8090.')
    assert 'TCP 22' not in message


@pytest.mark.parametrize('rendezvous, state, proved', [
    (NODES[0]['address'], {'powered': ['master-0'], 'ownMedia': ['master-0']}, False),
    (NODES[0]['address'], {'powered': ['master-0'], 'ownMedia': []}, True),
    (NODES[0]['address'], {'powered': ['master-1'], 'ownMedia': ['master-1']}, True),
    (NODES[1]['address'], {'powered': ['master-0'], 'ownMedia': ['master-0']}, True),
])
def test_the_rendezvous_service_is_proved_only_when_its_node_was_booted_in_this_attempt(rendezvous, state, proved):
    reach = Reach(answering(22), rendezvous=rendezvous, state=state)
    message = reach.run()
    assert [waited[1] for waited in reach.waited].count(8090) == (1 if proved else 0)
    if proved:
        assert message.startswith('Machine %s never answered TCP 8090.' % next(
            '%s (%s)' % (node['machine'], node['address']) for node in NODES if node['address'] == rendezvous))
    else:
        assert message is None


@pytest.mark.parametrize('answers', [answering(), answering(22)])
def test_a_reach_failure_names_its_causes_and_never_promises_another_boot(answers):
    message = Reach(answers).run()
    assert "did not boot this cluster's image" in message
    assert 'no network configuration for the hardware address it booted with' in message
    assert 'boots the others' not in message and 'before every node was booted' not in message
    assert 'budget of 311 seconds' in message


def test_rendezvous_gate_skips_replayed_nodes():
    task = [t for t in tasks('readiness.yml') if 'ansible.builtin.wait_for' in t][1]
    scope = {'item': {'machine': 'one', 'address': '192.0.2.10'},
             'bootwright_cluster_install_request': {'rendezvous': '192.0.2.10'},
             'containercluster_install_agent_state': {'powered': ['one'], 'ownMedia': ['one']}}
    templar = Templar(loader=LOADER, variables=scope)
    assert templar.evaluate_expression(task['when'][0]) is True
    assert templar.evaluate_expression(task['when'][1]) is False


def test_arp_precedes_boot_mark_and_readiness_precedes_bootstrap():
    prepare = tasks('prepare_boot.yml')
    preboot = next(i for i, task in enumerate(prepare) if task.get('ansible.builtin.import_tasks') == 'preboot_vips.yml')
    mark = next(i for i, task in enumerate(prepare) if '.bootwright-booted' in str(task.get('ansible.builtin.copy', {})))
    assert preboot < mark
    plays = LOADER.load_from_file(str(ROLE.parents[1] / 'playbooks/containercluster/install_apply.yml'))
    phases = [play['tasks'][0]['ansible.builtin.import_role']['tasks_from'] for play in plays]
    assert phases.index('prepare_boot') < phases.index('node_boot') < phases.index('waits')
    node_boot = tasks('node_boot.yml')[1]['block']
    assert node_boot[0]['ansible.builtin.import_tasks'] == 'boot.yml'
    assert node_boot[1]['ansible.builtin.import_tasks'] == 'readiness.yml'
    assert node_boot[1]['vars']['containercluster_install_agent_readiness_nodes'] == '{{ [containercluster_install_agent_node] }}'


def test_partial_destroy_retires_only_successfully_ejected_tokens():
    release = next(t for t in tasks('destroy.yml') if 'always' in t)
    assert release['block'][1]['ansible.builtin.include_tasks'] == 'eject_node_serial.yml'
    assert tasks('eject_node_serial.yml')[0]['vars']['containercluster_install_agent_port_operation'] == 'media_eject'
    assert 'ansible.builtin.fail' in release['block'][2]
    task = release['always'][0]
    for result, expected in [({'changed': True}, True), ({'msg': 'unknown eject'}, False)]:
        scope = {'containercluster_install_agent_cleanup_index': 0,
                 'containercluster_install_agent_ejected': {'results': [result]}}
        assert Templar(loader=LOADER, variables=scope).evaluate_expression(task['when'][2]) is expected


def test_readiness_ports_share_one_deadline_and_a_spent_budget_still_tries_once(monkeypatch):
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
    for deadline, low, high in [(epoch + 20, 20, 20), (epoch, 1, 1), (epoch - 1, 1, 1), (epoch - 600, 1, 1)]:
        templar = Templar(loader=LOADER, variables={'containercluster_install_agent_boot_deadline': deadline})
        for wait in waits:
            assert low <= templar.template(wait['timeout']) <= high
            assert wait['connect_timeout'] == 1
