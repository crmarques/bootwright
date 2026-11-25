"""A removal that keeps a cluster's nodes ejects only its own image and restores nothing controller-wide.

The Go capability hands the adapter ``keeps: hosts`` when the frozen removal
names kept hosts. A kept node presenting another image is left as it is, its
token is not retired for it, and the eject of a node presenting this
cluster's image settles no BMC-wide verification or certificate; a removal
that deletes its nodes keeps ejecting whatever each presents and restoring
what its request froze.
"""
from pathlib import Path

from ansible.template import Templar
from ansible_collections.bootwright.core.tests.unit import role_tasks
from ansible_collections.bootwright.core.tests.unit.role_tasks import LOADER

ROLE = Path(__file__).resolve().parents[2] / 'roles/containercluster_install_agent'
NODE = {'machine': 'sno-01', 'controller': {'virtualMedia': {'restoreVerification': True, 'removeCertificate': True}}}


def scope(keeps, identity):
    variables = dict(LOADER.load_from_file(str(ROLE / 'defaults/main.yml'), trusted_as_template=True))
    variables.update({
        'bootwright_cluster_install_material': {'keeps': 'hosts'} if keeps else {},
        'containercluster_install_agent_node': NODE,
        'containercluster_install_agent_before': {'observation': {'url': 'https://192.0.2.1/agent.iso'}},
        'containercluster_install_agent_port_result': {'changed': True},
    })
    if identity is not None:
        variables['containercluster_install_agent_node_identity'] = identity
    return variables


def task(name):
    tasks = role_tasks.flatten(LOADER.load_from_file(str(ROLE / 'tasks/node_eject.yml'), trusted_as_template=True))
    return next(found for found in tasks if found.get('name') == name)


def ejects(keeps, identity):
    variables = scope(keeps, identity)
    eject = task("Eject through the node's substrate")
    if not Templar(loader=LOADER, variables=variables).evaluate_conditional(eject['when']):
        variables['containercluster_install_agent_port_result'] = {'stale': True}
        ejected = False
    else:
        ejected = True
    answer = Templar(loader=LOADER, variables=variables).template(
        task("Keep this node's eject answer")['ansible.builtin.set_fact']['containercluster_install_agent_node_ejected'])
    variables['containercluster_install_agent_node_ejected'] = answer
    retired = all(Templar(loader=LOADER, variables=variables).evaluate_conditional(condition)
                  for condition in task('Retire the private token after its node\'s eject succeeded')['when'])
    return ejected, retired, answer


def restores(keeps):
    port = role_tasks.flatten(LOADER.load_from_file(str(ROLE / 'tasks/port.yml'), trusted_as_template=True))
    include = next(found for found in port if 'ansible.builtin.include_role' in found)
    arguments = include['vars']['containercluster_install_agent_port_arguments']['media_eject']
    variables = scope(keeps, {'own': True})
    variables['bootwright_cluster_install_material'] = dict(variables['bootwright_cluster_install_material'], artifactCertificate='')
    templar = Templar(loader=LOADER, variables=variables)
    return templar.template(arguments['restore_verification']), templar.template(arguments['remove_certificate'])


def test_a_kept_node_presenting_another_image_is_left_as_it_is():
    for identity in ({'own': False}, {'skipped': True}, None):
        ejected, retired, answer = ejects(True, identity)
        assert not ejected and not retired, identity
        assert answer == {'containercluster_install_agent_node': NODE}, identity


def test_a_kept_node_presenting_its_own_image_is_ejected():
    assert ejects(True, {'own': True})[:2] == (True, True)


def test_a_removal_that_deletes_its_nodes_ejects_whatever_each_presents():
    for identity in ({'own': False}, {'own': True}, None):
        assert ejects(False, identity)[:2] == (True, True), identity


def test_only_a_removal_that_deletes_its_nodes_restores_what_its_request_froze():
    assert restores(False) == (True, True)
    assert restores(True) == (False, False)
