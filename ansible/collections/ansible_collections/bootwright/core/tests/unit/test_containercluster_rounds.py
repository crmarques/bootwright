"""Run the real node-phase shell against scripted boot and readiness ports."""
import json
import os
from pathlib import Path
import subprocess
import sys

import yaml

COLLECTION = Path(__file__).resolve().parents[2]
TASKS = COLLECTION / 'roles/containercluster_install_agent/tasks'
PLAYBOOK = COLLECTION / 'playbooks/containercluster/install_apply.yml'


def test_node_failure_waits_for_its_siblings_and_never_starts_installer_wait(tmp_path):
    plays = yaml.safe_load(PLAYBOOK.read_text())
    boot = next(play for play in plays if play['tasks'][0]['ansible.builtin.import_role']['tasks_from'] == 'node_boot')
    assert boot['serial'] == boot['throttle'] == '{{ bootwright_cluster_install_request.budgets.fanOut | default(1) }}'
    assert boot['any_errors_fatal'] is False
    (tmp_path / 'node_boot.yml').write_text((TASKS / 'node_boot.yml').read_text())
    (tmp_path / 'boot.yml').write_text(yaml.safe_dump([{'name': 'Scripted boot port', 'ansible.builtin.copy': {
        'content': 'booted', 'dest': str(tmp_path / 'booted-') + '{{ bootwright_node_index }}', 'mode': '0600'}}]))
    sibling = tmp_path / 'sibling-ready'
    (tmp_path / 'readiness.yml').write_text(yaml.safe_dump([
        {'name': 'Scripted typed refusal',
         'ansible.builtin.set_fact': {'containercluster_install_agent_lifecycle_refusal': 'node-unreachable'},
         'when': "inventory_hostname == 'bootwright_node_000'"},
        {'name': 'Scripted readiness port', 'ansible.builtin.assert': {'that': "inventory_hostname != 'bootwright_node_000'"}},
        {'name': 'Record the sibling completed readiness', 'ansible.builtin.copy': {'content': 'ready', 'dest': str(sibling), 'mode': '0600'}},
    ]))
    typed = tmp_path / 'typed-refusal'
    aggregation = yaml.safe_load((TASKS / 'phase_failures.yml').read_text())
    publication = next(task for task in aggregation if 'bootwright.core.containercluster_install_protocol' in task)
    publication.pop('bootwright.core.containercluster_install_protocol')
    publication['ansible.builtin.copy'] = {
        'content': ("{{ hostvars[phase_host].containercluster_install_agent_lifecycle_refusal }}:"
                    "{{ hostvars[phase_host].bootwright_node_index }}:{{ lookup('ansible.builtin.file', '" + str(sibling) + "') }}"),
        'dest': str(typed), 'mode': '0600',
    }
    phase_failures = tmp_path / 'phase_failures.yml'
    phase_failures.write_text(yaml.safe_dump(aggregation))
    boot['post_tasks'][0].pop('ansible.builtin.import_role')
    boot['post_tasks'][0]['ansible.builtin.import_tasks'] = str(phase_failures)
    boot['tasks'] = [{'name': 'Run real per-node wrapper', 'ansible.builtin.include_tasks': str(tmp_path / 'node_boot.yml')}]
    never = tmp_path / 'installer-wait'
    finish = {'hosts': 'bootwright_target', 'gather_facts': False, 'tasks': [
        {'name': 'Aggregate real node failures', 'ansible.builtin.include_tasks': str(phase_failures)},
        {'name': 'Installer wait sentinel', 'ansible.builtin.copy': {'content': 'bad', 'dest': str(never), 'mode': '0600'}},
    ]}
    nodes = [{'name': 'master-%d' % i, 'machine': 'machine-%d' % i, 'address': '192.0.2.%d' % (20 + i)} for i in range(4)]
    state = {'powered': [], 'ownMedia': []}
    inventory = {'all': {'vars': {'ansible_connection': 'local', 'ansible_python_interpreter': sys.executable,
                                  'bootwright_cluster_install_request': {'nodes': nodes, 'budgets': {'fanOut': 2}},
                                  'bootwright_cluster_install_material': {}},
                         'children': {'bootwright_target': {'hosts': {'controller': {
                             'containercluster_install_agent_before': {'observation': {}},
                             'containercluster_install_agent_state': state,
                         }}}, 'bootwright_nodes': {'hosts': {
                             'bootwright_node_%03d' % i: {'bootwright_node_index': i, 'bootwright_controller_host': 'controller'} for i in range(4)
                         }}}}}
    inventory_path = tmp_path / 'inventory.json'
    inventory_path.write_text(json.dumps(inventory))
    variables = tmp_path / 'variables.json'
    variables.write_text(json.dumps(inventory['all']['vars']))
    playbook = tmp_path / 'play.json'
    playbook.write_text(json.dumps([boot, finish]))
    config = tmp_path / 'ansible.cfg'
    config.write_text('')
    env = dict(os.environ, ANSIBLE_CONFIG=str(config), ANSIBLE_HOME=str(tmp_path / 'home'), ANSIBLE_LOCAL_TEMP=str(tmp_path / 'local'))
    ran = subprocess.run([sys.executable, '-m', 'ansible.cli.playbook', '--forks', '2', '-i', str(inventory_path),
                          '--extra-vars', '@' + str(variables), str(playbook)],
                         capture_output=True, text=True, env=env, timeout=30, check=False)
    assert ran.returncode != 0, ran.stdout + ran.stderr
    assert sibling.exists(), ran.stdout + ran.stderr
    assert sibling.read_text() == 'ready', ran.stdout + ran.stderr
    assert typed.read_text() == 'node-unreachable:0:ready', ran.stdout + ran.stderr
    assert not (tmp_path / 'booted-2').exists(), ran.stdout + ran.stderr
    assert not never.exists(), ran.stdout + ran.stderr
    assert 'Node master-0 (Machine/machine-0, 192.0.2.20) failed' in ran.stdout, ran.stdout + ran.stderr


def test_each_install_port_request_matches_the_selected_arm_entrypoint():
    from ansible.module_utils.common.arg_spec import ArgumentSpecValidator
    from ansible.parsing.dataloader import DataLoader
    from ansible.template import Templar

    loader = DataLoader()
    port = loader.load_from_file(str(TASKS / 'port.yml'), trusted_as_template=True)[1]
    for arm in ('libvirt', 'baremetal'):
        specs = yaml.safe_load((COLLECTION / ('roles/substrate_%s_machine/meta/argument_specs.yml' % arm)).read_text())['argument_specs']
        for operation in ('state_read', 'media_insert', 'media_eject', 'power'):
            scope = dict(port['vars'], containercluster_install_agent_port_operation=operation,
                         containercluster_install_agent_node={
                             'physical': arm == 'baremetal', 'controller': {
                                 'endpoint': 'https://bmc.example.test', 'tlsVerify': True,
                                 'virtualMedia': {'trust': 'importCertificate', 'restoreVerification': True, 'removeCertificate': True}}},
                         containercluster_install_agent_user='/material/user', containercluster_install_agent_password='/material/password',
                         containercluster_install_agent_ca='/material/ca', containercluster_install_agent_index=0,
                         bootwright_run_scratch='/scratch', bootwright_cluster_install_material={'artifactCertificate': '/material/certificate'})
            request = Templar(loader=loader, variables=scope).template(port['vars']['bootwright_machine_port'])
            if operation == 'media_insert':
                assert request['private'] is (arm == 'baremetal')
            result = ArgumentSpecValidator(specs[operation]['options']).validate({'bootwright_machine_port': request})
            assert not result.error_messages, (arm, operation, result.error_messages)


def test_node_collection_uses_the_ordered_inventory_aliases():
    from ansible.parsing.dataloader import DataLoader
    from ansible.template import Templar

    loader = DataLoader()
    collect = loader.load_from_file(str(TASKS / 'collect_state.yml'), trusted_as_template=True)[2]
    for index in (0, 9, 10, 27):
        scope = {
            'node_index': index, 'node': {'machine': 'fixture-node'},
            'containercluster_install_agent_controllers': {'results': []},
            'containercluster_install_agent_media_identity': {'results': []},
            'hostvars': {'bootwright_node_%03d' % index: {
                'containercluster_install_agent_node_read': {'power': 'Off'},
                'containercluster_install_agent_node_identity': {'own': False},
            }},
        }
        result = Templar(loader=loader, variables=scope).template(collect['ansible.builtin.set_fact'])
        assert result['containercluster_install_agent_controllers']['results'][0]['power'] == 'Off'
        assert result['containercluster_install_agent_media_identity']['results'][0]['own'] is False
