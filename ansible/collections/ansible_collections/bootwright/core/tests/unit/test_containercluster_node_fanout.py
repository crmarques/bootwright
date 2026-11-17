"""Apply, observe and destroy read and eject their nodes through one fan-out.

Every node operation of the installation role runs in a node play bounded by the
frozen fan-out, never as a loop over the nodes on the controller, and one task
file decides which nodes present their own image, for a fetched and a pushed
image alike.
"""
import re
from pathlib import Path

import yaml
from ansible.template import Templar
from ansible_collections.bootwright.core.tests.unit import role_tasks
from ansible_collections.bootwright.core.tests.unit.role_tasks import LOADER

COLLECTION = Path(__file__).resolve().parents[2]
TASKS = COLLECTION / 'roles/containercluster_install_agent/tasks'
PLAYBOOKS = COLLECTION / 'playbooks/containercluster'
FAN_OUT = '{{ bootwright_cluster_install_request.budgets.fanOut | default(1) }}'


def node_phases(playbook):
    plays = yaml.safe_load((PLAYBOOKS / playbook).read_text())
    found = []
    for play in plays:
        if play['hosts'] != 'bootwright_nodes':
            continue
        assert play['serial'] == play['throttle'] == FAN_OUT, playbook
        found.append(play['tasks'][0]['ansible.builtin.import_role']['tasks_from'])
    return found


def test_observe_and_destroy_fan_out_their_node_reads_and_ejects():
    assert node_phases('install_observe.yml') == ['node_state']
    assert node_phases('install_destroy.yml') == ['node_state', 'node_eject', 'node_state']
    assert 'node_state' in node_phases('install_apply.yml')


def test_no_task_file_walks_the_nodes_through_their_ports_one_at_a_time():
    for path in sorted(TASKS.glob('*.yml')):
        for task in role_tasks.flatten(yaml.safe_load(path.read_text()) or []):
            included = task.get('ansible.builtin.include_tasks') or task.get('ansible.builtin.import_tasks')
            if 'loop' in task and included:
                assert 'bootwright_cluster_install_request.nodes' not in str(task['loop']), (path.name, task['name'])


def test_one_task_file_resolves_which_nodes_present_their_own_image():
    resolving = [path.name for path in sorted(TASKS.glob('*.yml')) if re.search(r'^ +ownMedia:', path.read_text(), re.M)]
    assert resolving == ['resolve_state.yml']


def resolved(delivery, read, identity):
    """The ownMedia one node's read and identity proof resolve to through the shared files."""
    node = {'machine': 'sno-01', 'name': 'master-0'}
    scope = {
        'bootwright_cluster_install_request': {'deliveryKind': delivery, 'nodes': [node]},
        'containercluster_install_agent_node_read': read,
    }
    pushed = next(task for task in role_tasks.flatten(LOADER.load_from_file(str(TASKS / 'node_state.yml'), trusted_as_template=True))
                  if task.get('name') == 'Prove the presented pushed image identity')
    if Templar(loader=LOADER, variables=scope).evaluate_conditional(pushed['when']):
        identity = Templar(loader=LOADER, variables=scope).template(
            pushed['ansible.builtin.set_fact']['containercluster_install_agent_node_identity'])
    collect = LOADER.load_from_file(str(TASKS / 'collect_nodes.yml'), trusted_as_template=True)[1]
    scope.update({
        'node_index': 0, 'node': node,
        'containercluster_install_agent_controllers': {'results': []},
        'containercluster_install_agent_media_identity': {'results': []},
        'hostvars': {'bootwright_node_000': {'containercluster_install_agent_node_read': read,
                                             'containercluster_install_agent_node_identity': identity}},
    })
    scope.update(Templar(loader=LOADER, variables=scope).template(collect['ansible.builtin.set_fact']))
    resolve = LOADER.load_from_file(str(TASKS / 'resolve_state.yml'), trusted_as_template=True)[0]
    scope.update(resolve['vars'])
    scope['containercluster_install_agent_state'] = {}
    return list(Templar(loader=LOADER, variables=scope).template(
        resolve['ansible.builtin.set_fact']['containercluster_install_agent_state']['ownMedia']))


def test_a_pushed_image_is_own_by_the_ports_answer_alone():
    for answer, own in (('own', ['sno-01']), ('foreign', []), ('none', []), ('unknown', [])):
        read = {'media': 'kubevirt-01-media', 'media_state': answer}
        assert resolved('push', read, {'own': False}) == own, answer


def test_a_fetched_image_is_own_by_its_inode_proof_alone():
    read = {'media': 'https://192.0.2.1/agent.iso', 'media_state': 'own'}
    assert resolved('fetch', read, {'own': True}) == ['sno-01']
    assert resolved('fetch', read, {'own': False}) == []
    assert resolved('fetch', read, {'skipped': True}) == []
