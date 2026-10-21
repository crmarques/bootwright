"""Compiler-rendered package and networking programs against isolated fake oc."""
from __future__ import annotations

import copy
import json
from pathlib import Path

import pytest

from ansible_collections.bootwright.core.tests.unit.test_cluster_objects import ScriptedOC, owned, response
# pytest fixture registration
from ansible_collections.bootwright.core.tests.unit.test_cluster_objects import object_request  # pylint: disable=unused-import
from ansible_collections.bootwright.core.plugins.module_utils import cluster_objects_model as model
from ansible_collections.bootwright.core.plugins.module_utils.cluster_objects_engine import Engine, Refused


class Cluster:
    def __init__(self, directory, request, fault):
        self.oc = ScriptedOC(directory, [])
        self.request = request
        self.fault = fault
        self.objects = {}
        self.counts = {}
        self.events = []
        self.subscription = next((s['subscribe'] for s in request['program']['steps'] if s['kind'] == 'subscribe'), None)

    def live(self, obj):
        live = owned(self.request, obj)
        if obj['kind'] == 'Subscription':
            live['status'] = {'installedCSV': self.subscription['csv'], 'installPlanRef': {'name': 'install-plan'}}
        return live

    def external(self, obj):
        kind = obj['kind']
        live = {'metadata': {'uid': 'external'}}
        if kind == 'ClusterServiceVersion':
            live.update(status={'phase': 'Succeeded'}, spec={'version': '1.0.0'})
        elif kind == 'InstallPlan':
            sub = self.subscription
            spec = sub['object']['body']['spec']
            live = {'metadata': {'uid': 'plan-1', 'ownerReferences': [{'kind': 'Subscription', 'uid': 'uid-1'}]},
                    'spec': {'approval': 'Manual', 'approved': False, 'clusterServiceVersionNames': [sub['csv']]},
                    'status': {'bundleLookups': [{'catalogSourceRef': {'name': spec['source'], 'namespace': spec['sourceNamespace']}}]}}
            if self.fault == 'extra-csv':
                live['spec']['clusterServiceVersionNames'].append('unrequested.v1')
        elif kind == 'CatalogSource':
            live['spec'] = {'image': 'registry.example/catalog:1'}
        return live

    def read(self, obj, fields):
        key = model.identity(obj)
        managed = obj['kind'] in ('Namespace', 'OperatorGroup', 'Subscription', 'NMState', 'MetalLB',
                                  'IPAddressPool', 'L2Advertisement', 'NodeNetworkConfigurationPolicy', 'ClusterUserDefinedNetwork')
        live = copy.deepcopy(self.objects.get(key)) if managed else self.external(obj)
        if self.fault == 'foreign' and obj['kind'] in ('NMState', 'MetalLB', 'NodeNetworkConfigurationPolicy'):
            live = {'metadata': {'uid': 'foreign', 'annotations': {}}}
        if live and 'status.conditions' in fields and obj['kind'] != 'Subscription':
            count = self.counts.get(key, 0)
            self.counts[key] = count + 1
            condition = {'CustomResourceDefinition': 'Established', 'ClusterUserDefinedNetwork': 'NetworkCreated'}.get(obj['kind'], 'Available')
            live['status'] = {'conditions': [{'type': condition, 'status': 'True' if count else 'False', 'reason': 'Ready' if count else 'Progressing'}]}
            if self.fault == 'degraded' and obj['kind'] == 'NodeNetworkConfigurationPolicy':
                live['status']['conditions'].append({'type': 'Degraded', 'status': 'True', 'reason': 'Failed'})
            self.events.append(('ready' if count else 'progressing', obj['kind'], obj['name']))
        self.oc.responses.write_text(json.dumps([response(live)]))
        return self.oc.read(obj, fields)

    def write(self, action, obj, extra):
        self.events.append((action, obj['kind'], obj['name']))
        if action == 'create':
            live = self.live(obj)
            self.objects[model.identity(obj)] = live
            self.oc.responses.write_text(json.dumps([response(live)]))
        else:
            self.oc.responses.write_text(json.dumps([response()]))
        return self.oc.write(action, obj, extra)


def program_request(request, name):
    path = Path(__file__).parent / 'fixtures' / 'cluster_objects' / (name + '.json')
    request['program'] = json.loads(path.read_text())
    return request


@pytest.mark.parametrize('name', ['nmstate', 'metallb', 'network'])
def test_full_program_orders_operand_creation_after_readiness(tmp_path, object_request, name):
    request = program_request(object_request, name)
    cluster = Cluster(tmp_path, request, '')
    result = Engine(request, cluster.read, cluster.write, clock=lambda: 0, sleep=lambda ignored_seconds: None).execute()
    assert result['state']['postcondition']
    events = cluster.events
    creates = [(i, kind) for i, (action, kind, ignored_name) in enumerate(events) if action == 'create']
    crds = {'NMState': 'nmstates.nmstate.io', 'MetalLB': 'metallbs.metallb.io', 'IPAddressPool': 'ipaddresspools.metallb.io',
            'L2Advertisement': 'l2advertisements.metallb.io', 'NodeNetworkConfigurationPolicy': 'nodenetworkconfigurationpolicies.nmstate.io',
            'ClusterUserDefinedNetwork': 'clusteruserdefinednetworks.k8s.ovn.org'}
    for index, kind in creates:
        before = events[:index]
        if kind in crds:
            assert ('ready', 'CustomResourceDefinition', crds[kind]) in before
        if kind in ('IPAddressPool', 'L2Advertisement'):
            assert ('ready', 'MetalLB', 'metallb') in before
        if kind == 'ClusterUserDefinedNetwork':
            policies = [obj for obj in cluster.objects.values() if obj['kind'] == 'NodeNetworkConfigurationPolicy']
            assert policies
            assert all(('ready', 'NodeNetworkConfigurationPolicy', p['metadata']['name']) in before for p in policies)
    assert any(event[0] == 'progressing' for event in events)
    assert {call['argv'][3] for call in cluster.oc.calls()} <= {'get', 'create', 'patch'}


@pytest.mark.parametrize('name,fault', [('nmstate', 'extra-csv'), ('metallb', 'extra-csv'), ('nmstate', 'foreign'),
                                        ('metallb', 'foreign'), ('network', 'foreign'), ('network', 'degraded')])
def test_full_program_failure_never_creates_downstream_objects(tmp_path, object_request, name, fault):
    request = program_request(object_request, name)
    cluster = Cluster(tmp_path, request, fault)
    with pytest.raises(Refused):
        Engine(request, cluster.read, cluster.write, clock=lambda: 0, sleep=lambda ignored_seconds: None).execute()
    downstream = {'IPAddressPool', 'L2Advertisement', 'ClusterUserDefinedNetwork'}
    if fault == 'extra-csv':
        downstream |= {'NMState', 'MetalLB'}
        assert not any(action == 'approve' for action, ignored_kind, ignored_name in cluster.events)
    assert not any(action == 'create' and kind in downstream for action, kind, ignored_name in cluster.events)
