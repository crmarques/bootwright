"""The per-insert token is wired through boot, release and the ownership read."""

import json
import os
import pathlib

from ansible_collections.bootwright.core.tests.unit.install_phase_tasks import apply_tasks as phased_tasks

import pytest
from ansible.module_utils.testing import patch_module_args
from ansible.template import Templar

from ansible_collections.bootwright.core.plugins.module_utils import containercluster_publication as publication
from ansible_collections.bootwright.core.plugins.modules import containercluster_media_token as token
from ansible_collections.bootwright.core.tests.unit import listener_proof, role_tasks
from ansible_collections.bootwright.core.tests.unit.role_tasks import LOADER

ROLE = pathlib.Path(__file__).resolve().parents[2] / 'roles' / 'containercluster_install_agent'
MODULE = 'bootwright.core.containercluster_media_token'
TOKEN_URL = '{{ containercluster_install_agent_token.url }}'
RESERVED = ('public', 'private', 'clusters', 'cluster')


def tasks(file):
    if file == 'apply.yml':
        return phased_tasks(LOADER, ROLE)
    return LOADER.load_from_file(str(ROLE / 'tasks' / file), trusted_as_template=True)


def sequences(items):
    yield items
    for task in items:
        for key in ('block', 'rescue', 'always'):
            if task.get(key):
                yield from sequences(task[key])


def flat(items):
    for task in items:
        yield task
        for key in ('block', 'rescue', 'always'):
            yield from flat(task.get(key) or [])


def holds(condition, scope):
    return Templar(loader=LOADER, variables=scope).evaluate_expression(condition)


def render(value, scope):
    return Templar(loader=LOADER, variables=scope).template(value)


def boot_sequence():
    return next(items for items in sequences(tasks('boot.yml'))
                if any(MODULE in task and task[MODULE]['operation'] == 'publish' for task in items))


def only(items, predicate):
    found = [task for task in items if predicate(task)]
    assert len(found) == 1, found
    return found[0]


def index(items, task):
    return next(position for position, candidate in enumerate(items) if candidate is task)


def link_block():
    """The block that publishes and proves a fetching node's own link, and the
    sequence it sits in before that node's insert."""
    outer = next(items for items in sequences(tasks('boot.yml'))
                 if any(task.get('block') is not None and task['block'] == boot_sequence() for task in items))
    return only(outer, lambda t: t.get('block') == boot_sequence()), outer


INSERT_TOKEN = 'insert-' + '0123456789abcdef' * 4
INSERT_URL = 'https://192.0.2.1:8443/private/clusters/sno/' + INSERT_TOKEN + '/agent.iso'
SERVING_SECRET = 'artifact-server-tls'


def proof_block():
    """The block that proves this insert's link, and rescues a refusal by retiring it."""
    steps = boot_sequence()
    return only(steps, lambda t: any(listener_proof.includes(task) for task in t.get('block') or [])), steps


def insert_scope():
    return {'containercluster_install_agent_token': {'url': INSERT_URL},
            'containercluster_install_agent_url': 'https://192.0.2.1:8443/private/clusters/sno',
            'bootwright_cluster_install_request': {'tlsCertificateRef': SERVING_SECRET},
            'bootwright_cluster_install_material': {'artifactCertificate': '/run/material/artifact-ca'}}


def test_boot_publishes_then_proves_the_listener_then_inserts():
    block, steps = proof_block()
    publish = only(steps, lambda t: t.get(MODULE, {}).get('operation') == 'publish')
    linked, outer = link_block()
    insert = only(outer, lambda t: t.get('vars', {}).get('containercluster_install_agent_port_operation') == 'media_insert')
    assert publish['register'] == 'containercluster_install_agent_token'
    assert publish[MODULE]['source'] == '{{ containercluster_install_agent_url }}'
    assert index(steps, publish) < index(steps, block)
    assert index(outer, linked) < index(outer, insert)
    # A pushed node is handed the file, so it publishes, proves and retires no link.
    for kind, links in [('fetch', True), ('push', False)]:
        assert holds(linked['when'], {'bootwright_cluster_install_request': {'deliveryKind': kind}}) is links
    probe = only(block['block'], listener_proof.includes)
    assert probe['vars']['infrastructureservices_artifact_listener_url'] == TOKEN_URL
    for status, passes in [(200, True), (206, True), (404, False), (302, False), (-1, False)]:
        outcome = listener_proof.run(probe, insert_scope(), {'status': status, 'msg': 'refused ' + INSERT_URL})
        assert outcome.fetched['url'] == INSERT_URL and outcome.fetched['ca_path'] == '/run/material/artifact-ca'
        assert (outcome.refusal is None) is passes
        if not passes:
            assert INSERT_TOKEN not in outcome.refusal and outcome.refusal.startswith('the private insert image could not be')
    assert 'its certificate did not verify against the serving certificate Secret %s holds' % SERVING_SECRET in listener_proof.run(
        probe, insert_scope(), {'status': -1, 'msg': 'Request failed: certificate verify failed'}).refusal


def boot_through_the_proof(answer, raised=None):
    """Run the proof block over one fetch answer, or a fetch that failed with `raised` past its failed_when:
    the interpreter, what it retired, and the refusal it ended with."""
    block, _steps = proof_block()
    retired = []

    def execute(task, templar):
        if listener_proof.includes(task):
            listener_proof.perform(task, interpreter.variables, answer, raised)
            return {'changed': False}
        if MODULE in task:
            retired.append(templar.template(task[MODULE]))
            return {'changed': True}
        return None

    interpreter = role_tasks.Interpreter(ROLE, dict(insert_scope(), containercluster_install_agent_lifecycle_refusal=''), execute)
    try:
        interpreter.run_tasks([block])
    except role_tasks.TaskFailed as failure:
        return interpreter, retired, failure.result['msg']
    return interpreter, retired, None


def test_an_unproved_link_is_retired_before_the_gate_and_never_inserted():
    block, _steps = proof_block()
    retire = only(block['rescue'], lambda t: t.get(MODULE, {}).get('operation') == 'remove')
    assert retire[MODULE]['image'] == TOKEN_URL
    assert retire[MODULE]['source'] == '{{ containercluster_install_agent_url }}'
    assert retire['no_log'] is True and 'no_log' not in block['rescue'][-1]
    for status in (200, 206):
        interpreter, retired, refusal = boot_through_the_proof({'status': status})
        assert refusal is None and retired == [] and interpreter.variables['containercluster_install_agent_lifecycle_refusal'] == ''
    for status in (404, 403, 500, -1):
        interpreter, retired, refusal = boot_through_the_proof({'status': status, 'msg': 'refused ' + INSERT_URL})
        assert [entry['image'] for entry in retired] == [INSERT_URL]
        assert interpreter.variables['containercluster_install_agent_lifecycle_refusal'] == 'private-delivery'
        assert refusal.startswith('the private insert image could not be') and INSERT_TOKEN not in refusal, refusal
        # The judging step's own message, the one refusal printed as it is.
        assert refusal == listener_proof.run(only(block['block'], listener_proof.includes), insert_scope(),
                                             {'status': status, 'msg': 'refused ' + INSERT_URL}).refusal


FIXED = 'the private insert image was not proved through its artifact server listener'


def budget_report():
    """The boot block's first rescue step, which names a spent boot budget."""
    bounded = next(task for task in flat(tasks('boot.yml')) if 'rescue' in task and 'timeout' in task)
    report = bounded['rescue'][0]
    assert 'containercluster_install_agent_boot_spent_hint' in report['ansible.builtin.fail']['msg']
    return report


def test_a_proof_the_boot_budget_stopped_stays_a_budget_refusal_and_still_retires_its_link():
    stopped = {'failed': True, 'timedout': {'frame': 'SECRET', 'period': 1}, 'msg': 'The task timed out ' + INSERT_URL}
    interpreter, retired, refusal = boot_through_the_proof({'status': 206}, raised=stopped)
    assert [entry['image'] for entry in retired] == [INSERT_URL]
    assert interpreter.variables['containercluster_install_agent_lifecycle_refusal'] == ''
    assert refusal == FIXED
    # The re-raise carries no timedout of its own; the boot block's rescue still names the budget.
    report = budget_report()
    scope = dict(interpreter.variables, ansible_failed_result={'failed': True, 'msg': refusal})
    assert holds(report['when'], scope) is True
    assert holds(report['when'], {'ansible_failed_result': {'failed': True, 'timedout': {'period': 1}}}) is True
    assert holds(report['when'], {'ansible_failed_result': {'failed': True, 'msg': 'refused'}}) is False


def test_a_proof_whose_fetch_failed_by_exception_prints_only_the_fixed_refusal():
    raised = {'failed': True, 'msg': 'An unhandled exception occurred fetching ' + INSERT_URL, 'exception': INSERT_URL}
    interpreter, retired, refusal = boot_through_the_proof({'status': 206}, raised=raised)
    assert [entry['image'] for entry in retired] == [INSERT_URL]
    assert interpreter.variables['containercluster_install_agent_lifecycle_refusal'] == 'private-delivery'
    assert interpreter.variables['containercluster_install_agent_proof_timedout'] is False
    assert refusal == FIXED


def test_a_proof_refused_for_its_input_prints_its_fixed_check():
    scope = insert_scope()
    scope['containercluster_install_agent_token'] = {'url': 'file:///' + INSERT_TOKEN + '/agent.iso'}
    block, _steps = proof_block()
    retired = []

    def execute(task, templar):
        if listener_proof.includes(task):
            listener_proof.perform(task, interpreter.variables, {'status': 206})
            return {'changed': False}
        if MODULE in task:
            retired.append(templar.template(task[MODULE]))
            return {'changed': True}
        return None

    interpreter = role_tasks.Interpreter(ROLE, dict(scope, containercluster_install_agent_lifecycle_refusal=''), execute)
    with pytest.raises(role_tasks.TaskFailed) as failed:
        interpreter.run_tasks([block])
    assert failed.value.result['msg'] == 'a listener proof needs an http or https URL and the subject it names'
    assert len(retired) == 1 and interpreter.variables['containercluster_install_agent_lifecycle_refusal'] == 'private-delivery'


def test_the_insert_serves_the_per_insert_token_never_the_cluster_wide_publication():
    scope = {'containercluster_install_agent_token': {'url': 'https://192.0.2.1/private/c/insert-aa/agent.iso'},
             'containercluster_install_agent_url': 'https://192.0.2.1/private/c/abcdef/agent.iso'}
    steps = list(flat(tasks('boot.yml')))
    insert = only(steps, lambda t: t.get('vars', {}).get('containercluster_install_agent_port_operation') == 'media_insert')
    material = only(steps, lambda t: t.get('ansible.builtin.copy', {}).get('content') == TOKEN_URL)
    assert index(steps, material) < index(steps, insert)
    assert material['ansible.builtin.copy']['mode'] == '0600'
    assert render(material['ansible.builtin.copy']['content'], scope) == scope['containercluster_install_agent_token']['url']
    port = only(tasks('port.yml'), lambda t: 'ansible.builtin.include_role' in t)
    arguments = port['vars']['containercluster_install_agent_fetched_insert']
    assert arguments['image_url_file'] == material['ansible.builtin.copy']['dest']
    chosen = port['vars']['containercluster_install_agent_port_arguments']['media_insert']
    for kind, used in [('fetch', 'fetched'), ('push', 'pushed')]:
        selection = {'bootwright_cluster_install_request': {'deliveryKind': kind},
                     'containercluster_install_agent_fetched_insert': 'fetched', 'containercluster_install_agent_pushed_insert': 'pushed'}
        assert render(chosen, selection) == used
    removals = [t for t in steps if t.get(MODULE, {}).get('operation') == 'remove']
    assert len(removals) == 2
    for removal in removals:
        assert removal[MODULE]['image'] == TOKEN_URL
        assert render(removal[MODULE]['image'], scope) != scope['containercluster_install_agent_url']
    assert not [t for t in steps if t.get('bootwright.core.redfish_boot', {}).get('image') == '{{ containercluster_install_agent_url }}']


def test_release_retires_the_token_only_after_an_eject_that_reported_no_failure():
    release = tasks('release.yml')
    eject = only(release, lambda t: t.get('vars', {}).get('containercluster_install_agent_port_operation') == 'media_eject')
    gate = only(release, lambda t: 'ansible.builtin.assert' in t and 'released' in str(t['ansible.builtin.assert']['that']))
    retire = only(release, lambda t: t.get(MODULE, {}).get('operation') == 'remove')
    retained = release[index(release, eject) + 1]['ansible.builtin.set_fact']
    assert retained['containercluster_install_agent_released'] == '{{ containercluster_install_agent_port_result }}'
    assert index(release, eject) < index(release, gate) < index(release, retire)
    assert not {'ignore_errors', 'rescue', 'block'} & (set(gate) | set(eject) | set(retire))
    for released, passes in [({'changed': True}, True), ({'msg': 'HTTP 503'}, False)]:
        scope = {'containercluster_install_agent_released': released}
        assert holds(gate['ansible.builtin.assert']['that'][0], scope) is passes


def test_release_retires_this_attempts_token_else_what_the_controller_presented():
    retire = only(tasks('release.yml'), lambda t: t.get(MODULE, {}).get('operation') == 'remove')
    assert retire[MODULE]['source'] == '{{ containercluster_install_agent_before.observation.url }}'
    own, presented = 'https://192.0.2.1/private/c/insert-aa/agent.iso', 'https://192.0.2.1/private/c/insert-bb/agent.iso'
    scope = {'containercluster_install_agent_node': {'machine': 'sno-01'}, 'containercluster_install_agent_index': 1,
             'containercluster_install_agent_controllers': {'results': [{'media': ''}, {'media': presented}]}}
    assert render(retire[MODULE]['image'], dict(scope, containercluster_install_agent_insert_tokens={'sno-01': own})).strip() == own
    assert render(retire[MODULE]['image'], scope).strip() == presented
    for url, runs in [('', False), ('https://192.0.2.1/private/c/abcdef/agent.iso', True)]:
        assert holds(retire['when'], {'containercluster_install_agent_before': {'observation': {'url': url}}}) is runs


@pytest.fixture
def trusted_tree(tmp_path, monkeypatch):
    system = set()
    for parent in tmp_path.parents:
        info = os.stat(parent)
        system.add((info.st_dev, info.st_ino))
    original = publication.trusted

    def trusted(fd, path):
        info = os.fstat(fd)
        if (info.st_dev, info.st_ino) not in system:
            original(fd, path)

    monkeypatch.setattr(publication, 'OWNER', os.getuid())
    monkeypatch.setattr(publication, 'trusted', trusted)


def test_the_ownership_read_calls_an_insert_link_own_and_any_other_file_foreign(tmp_path, trusted_tree):
    root = tmp_path.joinpath('services', 'ctx', 'artifact-server', 'server', *RESERVED)
    root.mkdir(parents=True)
    base = 'https://192.0.2.1/private/cluster'
    for name in ('abcdef', 'insert-foreign'):
        (root / name).mkdir()
        (root / name / 'agent.iso').write_bytes(b'image')
    (root / 'insert-own').mkdir()
    os.link(root / 'abcdef' / 'agent.iso', root / 'insert-own' / 'agent.iso')
    request = {'deliveryKind': 'fetch', 'publicationRoot': {'path': str(root), 'url': base}}
    published = base + '/abcdef/agent.iso'
    presented = ['', published, base + '/insert-own/agent.iso', base + '/insert-foreign/agent.iso']
    controllers = [{'item': {'machine': 'sno-%02d' % number}, 'media': image, 'power': 'On'} for number, image in enumerate(presented)]
    inspect = only(list(flat(tasks('node_state.yml'))), lambda t: t.get(MODULE, {}).get('operation') == 'inspect')
    results = []
    for controller in controllers:
        scope = {'bootwright_cluster_install_request': request, 'containercluster_install_agent_node_read': controller,
                 'containercluster_install_agent_before': {'observation': {'url': published}}}
        arguments = dict(render(inspect[MODULE], scope))
        assert set(arguments) == {'request', 'source', 'image', 'operation'}
        results.append(dict(token.operate(**arguments), item=controller))
    assert [result['own'] for result in results] == [False, True, True, False]
    resolve = only(tasks('resolve_state.yml'), lambda t: 'containercluster_install_agent_state' in t.get('ansible.builtin.set_fact', {}))
    facts = resolve['ansible.builtin.set_fact']['containercluster_install_agent_state']
    scope = {'bootwright_cluster_install_request': request,
             'containercluster_install_agent_media_identity': {'results': results},
             'containercluster_install_agent_controllers': {'results': controllers},
             'containercluster_install_agent_state': {'cluster': '', 'release': '', 'completed': '', 'missing': []}}
    scope.update(resolve['vars'])
    own = render(facts['ownMedia'], dict(scope, containercluster_install_agent_presenting=[c for c in controllers if c['media']]))
    assert list(own) == ['sno-01', 'sno-02']


def sweep_refusals():
    found = []
    for file in ('apply.yml', 'destroy_release.yml'):
        steps = list(flat(tasks(file)))
        for position, task in enumerate(steps):
            if task.get(MODULE, {}).get('operation') == 'sweep':
                found.append((task, steps[position + 1]))
    assert len(found) == 3
    return found


def test_a_sweep_refusal_reaches_the_operator_with_counts_and_no_url(tmp_path, trusted_tree, capsys):
    root = tmp_path.joinpath('services', 'ctx', 'artifact-server', 'server', *RESERVED)
    (root / 'abcdef').mkdir(parents=True)
    (root / 'abcdef' / 'agent.iso').write_bytes(b'image')
    base = 'https://192.0.2.1:8443/private/cluster'
    request = {'publicationRoot': {'path': str(root), 'url': base}}
    source = base + '/abcdef/agent.iso'
    kept = token.operate(request, source, '', 'publish')['url']
    stale = token.operate(request, source, '', 'publish')['url']
    (root / 'insert-foreign').mkdir()
    (root / 'insert-foreign' / 'agent.iso').write_bytes(b'image')
    capsys.readouterr()
    with patch_module_args({'request': request, 'source': source, 'presented': [kept], 'operation': 'sweep'}), pytest.raises(SystemExit):
        token.main()
    refused = json.loads(capsys.readouterr().out)
    assert refused['failed']
    for sweep, follow in sweep_refusals():
        assert sweep['failed_when'] is False and sweep['no_log'] is True and 'no_log' not in follow
        for swept, stops in [(dict(refused, failed=False), True), ({'changed': False, 'removed': 0, 'kept': 1, 'refused': 0}, False),
                             ({'changed': False, 'skipped': True, 'skip_reason': 'Conditional result was False'}, False)]:
            scope = {sweep['register']: swept}
            assert holds(follow['when'], scope) is stops
        message = render(follow['ansible.builtin.fail']['msg'], {sweep['register']: refused})
        assert '1 entries' in message and '1 unpresented insert links were removed' in message
        for secret in ['https://', '192.0.2.1', 'insert-foreign', kept.split('/')[-2], stale.split('/')[-2], 'abcdef']:
            assert secret not in message, secret
