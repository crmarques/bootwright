"""Every insert link no controller presents is retired, and observation retires none."""

import pathlib

from ansible.parsing.dataloader import DataLoader
from ansible.playbook.helpers import load_list_of_blocks
from ansible.playbook.play import Play
from ansible.template import Templar

ROLE = pathlib.Path(__file__).resolve().parents[2] / 'roles' / 'containercluster_install_agent'
LOADER = DataLoader()
MODULE = 'bootwright.core.containercluster_media_token'
BASE = 'https://192.0.2.1/private/clusters/sno/'
TOKEN = 'insert-' + 'a' * 64
OTHER = 'insert-' + 'b' * 64


def tasks(file):
    return LOADER.load_from_file(str(ROLE / 'tasks' / file), trusted_as_template=True)


def walk(items):
    for task in items or []:
        yield task
        for key in ('block', 'rescue', 'always'):
            yield from walk(task.get(key))


def sweeps(items):
    return [task for task in items if task.get(MODULE, {}).get('operation') == 'sweep']


def position(items, predicate):
    return next(index for index, task in enumerate(items) if predicate(task))


def reports_its_refusal(items, index):
    sweep, follow = items[index], items[index + 1]
    assert sweep['register'] == 'containercluster_install_agent_swept'
    assert sweep['failed_when'] is False and sweep['no_log'] is True
    assert 'no_log' not in follow and follow['when'] == 'containercluster_install_agent_swept.msg is defined'
    assert 'containercluster_install_agent_swept.msg' in follow['ansible.builtin.fail']['msg']
    return follow


def controllers(*media):
    return {'results': [{'item': {'machine': 'sno-%02d' % index}, 'media': image, 'power': 'Off'}
                        for index, image in enumerate(media)]}


def evaluate(task, scope, field='presented'):
    variables = dict(scope)
    templar = Templar(loader=LOADER, variables=variables)
    for name, value in (task.get('vars') or {}).items():
        variables[name] = templar.template(value)
    return Templar(loader=LOADER, variables=variables).template(task[MODULE][field])


def guard_holds(task, scope):
    conditions = task['when'] if isinstance(task['when'], list) else [task['when']]
    templar = Templar(loader=LOADER, variables=scope)
    return all(templar.evaluate_expression(condition) for condition in conditions)


def test_apply_sweeps_right_after_reading_its_nodes_and_before_any_boot():
    apply = tasks('apply.yml')
    read = position(apply, lambda t: t.get('ansible.builtin.import_tasks') == 'state.yml')
    first = apply[read + 1]
    assert first.get(MODULE, {}).get('operation') == 'sweep' and first['no_log'] is True
    assert reports_its_refusal(apply, read + 1) is apply[read + 2]
    assert first[MODULE]['source'] == '{{ containercluster_install_agent_before.observation.url }}'
    assert read + 1 < position(apply, lambda t: '.bootwright-booted' in str(t.get('ansible.builtin.copy', {})))
    assert read + 1 < position(apply, lambda t: t.get('ansible.builtin.include_tasks') == 'boot.yml')
    scope = {'containercluster_install_agent_controllers': controllers(BASE + TOKEN + '/agent.iso', '')}
    assert evaluate(first, scope) == [BASE + TOKEN + '/agent.iso', '']
    for url, runs in [('', False), (BASE + 'abcdef/agent.iso', True)]:
        assert guard_holds(first, {'containercluster_install_agent_before': {'observation': {'url': url}}}) is runs


def test_apply_sweeps_once_every_node_released_its_media():
    apply = tasks('apply.yml')
    release = position(apply, lambda t: t.get('ansible.builtin.include_tasks') == 'release.yml')
    later = [index for index, task in enumerate(apply) if index > release and task in sweeps(apply)]
    assert len(later) == 1
    reread = position(apply[release:], lambda t: t.get('ansible.builtin.import_tasks') == 'state.yml') + release
    assert later[0] == reread + 1
    reports_its_refusal(apply, later[0])
    scope = {'containercluster_install_agent_controllers': controllers('', '')}
    assert evaluate(apply[later[0]], scope) == ['', '']
    assert len(sweeps(apply)) == 2


def test_destroy_sweeps_after_its_ejects_keeping_only_unejected_media():
    destroy = tasks('destroy.yml')
    block = next(t for t in destroy if 'always' in t)
    always = block['always']
    sweep = always[-2]
    assert sweep in sweeps(always) and sweep['no_log'] is True
    assert reports_its_refusal(always, len(always) - 2) is always[-1]
    assert position(always, lambda t: t.get(MODULE, {}).get('operation') == 'remove') < len(always) - 2
    ejects = [{'containercluster_install_agent_node': {'machine': 'sno-00'}},
              {'containercluster_install_agent_node': {'machine': 'sno-01'}, 'msg': 'HTTP 503'}]
    scope = {'containercluster_install_agent_controllers': controllers(BASE + TOKEN + '/agent.iso', BASE + OTHER + '/agent.iso'),
             'containercluster_install_agent_ejected': {'results': ejects}}
    assert evaluate(sweep, scope) == [BASE + OTHER + '/agent.iso']
    unejected = {'containercluster_install_agent_controllers': scope['containercluster_install_agent_controllers']}
    assert evaluate(sweep, unejected) == [BASE + TOKEN + '/agent.iso', BASE + OTHER + '/agent.iso']


def failed_insert_tasks():
    boot = list(walk(tasks('boot.yml')))
    rescue = next(t for t in boot if 'rescue' in t and 'timeout' in t)['rescue']
    read = position(rescue, lambda t: 'bootwright.core.redfish_system_read' in t)
    return rescue, read


def test_a_failed_insert_reads_its_controller_and_retires_its_link_when_not_presented():
    rescue, read = failed_insert_tasks()
    assert position(rescue, lambda t: (t.get('when') or '') == 'ansible_failed_result.timedout is defined') < read
    assert rescue[read]['bootwright.core.redfish_system_read']['media'] is True
    remove = rescue[read + 2]
    assert remove[MODULE]['operation'] == 'remove'
    assert remove[MODULE]['image'] == '{{ containercluster_install_agent_token.url }}'
    token = {'url': BASE + TOKEN + '/agent.iso'}
    failed = {'msg': 'HTTP 500'}
    for insertion, after, retires in [
        (failed, {'media': '', 'power': 'Off'}, True),
        (failed, {'media': BASE + OTHER + '/agent.iso', 'power': 'Off'}, True),
        (failed, {'media': 'HTTPS://192.0.2.1:443/private/clusters/sno/' + TOKEN + '/agent.iso', 'power': 'Off'}, False),
        (failed, {'msg': 'HTTP 403', 'failed_when_result': False}, False),
        ({'changed': True}, {'skipped': True}, False),
    ]:
        scope = {'containercluster_install_agent_insertion': insertion,
                 'containercluster_install_agent_after_insert': after,
                 'containercluster_install_agent_token': token}
        assert guard_holds(remove, scope) is retires, (insertion, after)


def test_a_read_after_a_failed_insert_runs_only_after_one():
    rescue, read = failed_insert_tasks()
    for insertion, runs in [({'msg': 'HTTP 500'}, True), ({'changed': True}, False)]:
        assert guard_holds(rescue[read], {'containercluster_install_agent_insertion': insertion}) is runs


def compiled_rescue():
    play = Play.load({'hosts': 'localhost', 'gather_facts': False, 'tasks': []}, loader=LOADER)
    blocks = load_list_of_blocks(tasks('boot.yml'), play=play, loader=LOADER)
    pending = list(blocks)
    while pending:
        block = pending.pop()
        if block.rescue and block.timeout is not None and 'deadline' in str(block.timeout):
            return block.rescue
        for task in block.block + block.rescue + block.always:
            if hasattr(task, 'block'):
                pending.append(task)
    raise AssertionError('no rescue of the timed boot block')


def test_the_read_after_a_failed_insert_takes_its_own_timeout_not_what_the_boot_budget_left():
    rescue = compiled_rescue()
    read = next(task for task in rescue if task.action == 'bootwright.core.redfish_system_read')
    report = next(task for task in rescue if task.name == 'Report the boot the boot phase\'s budget stopped')
    spent = {'containercluster_install_agent_boot_deadline': 0}
    assert int(Templar(loader=LOADER, variables=spent).template(report.timeout)) == 1
    assert read.timeout == 150
    assert int(Templar(loader=LOADER, variables=spent).template(read.timeout)) == 150


def test_observation_never_sweeps_or_removes_a_link():
    for file in ['observe.yml', 'state.yml', 'cluster_state.yml']:
        for task in walk(tasks(file)):
            assert task.get(MODULE, {}).get('operation', 'inspect') == 'inspect', (file, task.get('name'))
            assert task.get('ansible.builtin.import_tasks') in (None, 'state.yml', 'cluster_state.yml'), file
