"""An owner-approved upgrade reads complete only through the Subscription's own proved chain (B1184)."""
from __future__ import annotations

import copy

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import cluster_objects_model as model
from ansible_collections.bootwright.core.plugins.module_utils.cluster_objects_engine import Engine, Refused
from ansible_collections.bootwright.core.tests.unit.test_cluster_objects import owned
from ansible_collections.bootwright.core.tests.unit.test_cluster_objects import object_request  # pylint: disable=unused-import

SOURCE = {"name": "catalog", "namespace": "marketplace"}


def plan(name, generation, csv, replaces="", approved=True, approval="Manual", source=None, owner="uid-1"):
    lookup = {"identifier": csv, "catalogSourceRef": dict(source or SOURCE), "path": "registry.example/bundle:" + csv}
    if replaces:
        lookup["replaces"] = replaces
    return {"metadata": {"name": name, "uid": name + "-uid", "ownerReferences": [{"kind": "Subscription", "uid": owner}]},
            "spec": {"approval": approval, "approved": approved, "generation": generation, "clusterServiceVersionNames": [csv]},
            "status": {"bundleLookups": [lookup]}}


class Cluster:
    def __init__(self, request, installed, plans, replaces=None, phase="Succeeded", current=None):
        self.request = request
        self.creator = request["program"]["steps"][0]["create"]
        self.subscription = {"apiVersion": "operators.coreos.com/v1alpha1", "kind": "Subscription", "namespace": "operators",
                             "name": "sample", "namespaced": True, "desiredDigest": "d" * 64,
                             "body": {"spec": {"name": "sample", "channel": "stable", "source": "catalog", "sourceNamespace": "marketplace",
                                               "startingCSV": "sample.v1", "installPlanApproval": "Manual"}, "metadata": {"name": "sample"}}}
        self.live = owned(request, self.subscription)
        self.live["status"] = {"installedCSV": installed, "currentCSV": current or installed,
                               "installPlanRef": {"name": plans[-1]["metadata"]["name"] if plans else "initial"}}
        self.plans = plans
        self.csv = {"metadata": {"name": installed, "uid": "csv-uid"}, "status": {"phase": phase},
                    "spec": {"version": installed.split(".v", 1)[1] + ".0", "replaces": replaces if replaces is not None else ""}}
        self.writes = []

    def program(self, approval="exact", dependent=False, check=False):
        if dependent:
            wanted = {"namespace": "operators", "subscription": "sample", "package": "sample", "channel": "stable",
                      "source": "catalog", "sourceNamespace": "marketplace", "csv": "sample.v1", "creator": model.ref(self.creator)}
            steps = [{"kind": "approveDependent", "approveDependent": wanted}]
        else:
            steps = [{"kind": "subscribe", "subscribe": {"object": self.subscription, "csv": "sample.v1", "approval": approval}}]
        self.request["program"]["steps"] = steps
        if check:
            self.request["program"]["checks"] = [{"csvSucceeded": {"namespace": "operators", "subscription": "sample"}}]

    def read(self, ref, selected):
        if ref["kind"] == "Namespace":
            value = owned(self.request, self.creator)
        elif ref["kind"] == "Subscription":
            value = self.live
        elif ref["kind"] == "InstallPlan":
            value = {"items": self.plans} if not ref["name"] else next((p for p in self.plans if p["metadata"]["name"] == ref["name"]), None)
        elif ref["kind"] == "ClusterServiceVersion":
            value = {"items": [self.csv]} if not ref["name"] else self.csv if ref["name"] == self.csv["metadata"]["name"] else None
        else:
            value = {"spec": {"image": "registry.example/catalog:v1"}}
        return {"answered": True, "object": copy.deepcopy(value)}

    def observe(self):
        result = Engine(self.request, self.read, lambda *args: self.writes.append(args), clock=lambda: 0).execute(observe=True)
        assert not self.writes
        return result["state"]


def two_links():
    return [plan("initial", 1, "sample.v1"), plan("upgrade-a", 2, "sample.v2", "sample.v1"), plan("upgrade-b", 3, "sample.v3", "sample.v2")]


@pytest.mark.parametrize("links,installed", [(2, "sample.v2"), (3, "sample.v3")])
def test_chain_through_approved_plans_reads_complete(object_request, links, installed):
    cluster = Cluster(object_request, installed, two_links()[:links], replaces="sample.v%d" % (links - 1))
    cluster.program(check=True)
    state = cluster.observe()
    assert state["classification"] == "completed"
    fact = state["olm"][0]
    assert fact["installedCSV"] == installed and fact["csvVersion"] == installed.split(".v")[1] + ".0"
    assert fact["installPlan"] == {"name": "initial", "uid": "initial-uid", "csvs": ["sample.v1"]}
    assert fact["chain"] == [{"plan": p["metadata"]["name"], "csv": p["spec"]["clusterServiceVersionNames"][0]} for p in two_links()[:links]]
    assert fact["bundleImage"] == "registry.example/bundle:" + installed
    assert fact["upgradePending"] is False and state["checks"][0]["satisfied"] is True


def test_skip_upgrade_records_its_replaced_csv(object_request):
    # OLM writes the replaced installed CSV as `replaces` for a skips or skipRange edge too.
    cluster = Cluster(object_request, "sample.v3", [plan("initial", 1, "sample.v1"), plan("upgrade-b", 2, "sample.v3", "sample.v1")], replaces="sample.v1")
    cluster.program()
    state = cluster.observe()
    assert state["classification"] == "completed"
    assert state["olm"][0]["chain"] == [{"plan": "initial", "csv": "sample.v1"}, {"plan": "upgrade-b", "csv": "sample.v3"}]


def test_pinned_install_records_no_chain(object_request):
    cluster = Cluster(object_request, "sample.v1", [plan("initial", 1, "sample.v1")])
    cluster.program()
    state = cluster.observe()
    assert state["classification"] == "completed" and state["olm"][0]["chain"] == []


def broken(name):
    plans = two_links()
    replaces = "sample.v2"
    if name == "outside":
        plans[2] = plan("upgrade-b", 3, "sample.v3", "sample.v9")
    elif name == "source":
        plans[1] = plan("upgrade-a", 2, "sample.v2", "sample.v1", source={"name": "other", "namespace": "marketplace"})
    elif name == "unapproved":
        plans[1]["spec"]["approved"] = False
    elif name == "automatic":
        plans[2] = plan("upgrade-b", 3, "sample.v3", "sample.v2", approval="Automatic")
    elif name == "collected":
        del plans[1]
    elif name == "pin-collected":
        del plans[0]
    elif name == "other-owner":
        plans[1] = plan("upgrade-a", 2, "sample.v2", "sample.v1", owner="uid-other")
    elif name == "generation":
        plans[1]["spec"]["generation"] = 4
    elif name == "duplicate":
        plans.append(plan("upgrade-c", 4, "sample.v3", "sample.v2"))
    elif name == "csv-replaces":
        replaces = "sample.v1"
    return plans, replaces


@pytest.mark.parametrize("name", ["outside", "source", "unapproved", "automatic", "collected", "pin-collected",
                                  "other-owner", "generation", "duplicate", "csv-replaces"])
@pytest.mark.parametrize("approval", ["exact", "operatorManaged"])
def test_unproved_chain_stays_unknown(object_request, name, approval):
    plans, replaces = broken(name)
    cluster = Cluster(object_request, "sample.v3", plans, replaces=replaces)
    cluster.program(approval=approval, check=True)
    state = cluster.observe()
    assert state["classification"] == "unknown"
    gap = RETENTION.get(name)
    assert state["olm"][0].get("chainGap") == gap if gap else "chainGap" not in (state["olm"] or [{}])[0]
    assert state["observations"][0]["ownership"] == ("own" if gap else "unknown")
    if gap:
        assert state["olm"][0]["chain"] == [] and state["olm"][0]["installedCSV"] == "sample.v3"
        assert state["checks"][0]["satisfied"] is False and not state["checks"][0].get("unknown")


RETENTION = {"duplicate": {"cause": "duplicatePlans", "csv": "sample.v3", "plans": ["upgrade-b", "upgrade-c"]}}


def seven_links():
    return [plan("initial", 1, "sample.v1")] + [plan("upgrade-%d" % n, n, "sample.v%d" % n, "sample.v%d" % (n - 1)) for n in range(2, 8)]


@pytest.mark.parametrize("deleted,missing", [(2, "sample.v2"), (1, "sample.v1")])
@pytest.mark.parametrize("approval", ["exact", "operatorManaged"])
def test_a_link_olm_collected_is_a_retention_gap(object_request, deleted, missing, approval):
    # gcInstallPlans: above five plans per namespace, lowest generations first.
    cluster = Cluster(object_request, "sample.v7", seven_links()[deleted:], replaces="sample.v6")
    cluster.program(approval=approval, check=True)
    state = cluster.observe()
    assert state["classification"] == "unknown" and state["observations"][0]["ownership"] == "own"
    assert state["olm"][0]["chainGap"] == {"cause": "planCollected", "csv": missing} and state["olm"][0]["chain"] == []
    assert state["checks"][0]["satisfied"] is False and not state["checks"][0].get("unknown")


@pytest.mark.parametrize("change", ["fewer than five plans", "an older plan survives", "a plan without generation"])
def test_a_missing_link_olm_would_not_have_collected_is_no_retention_gap(object_request, change):
    plans = seven_links()[2:]
    if change == "fewer than five plans":
        del plans[0]
    elif change == "an older plan survives":
        plans.append(plan("other-upgrade", 1, "other.v2", "other.v1"))
    else:
        plans.append(plan("other-upgrade", 9, "other.v2", "other.v1"))
        del plans[-1]["spec"]["generation"]
    cluster = Cluster(object_request, "sample.v7", plans, replaces="sample.v6")
    cluster.program(check=True)
    state = cluster.observe()
    assert state["classification"] == "unknown" and state["observations"][0]["ownership"] == "unknown"
    assert all("chainGap" not in fact for fact in state["olm"])


@pytest.mark.parametrize("phase", ["Failed", "Replacing", "Installing"])
def test_a_retention_gap_needs_a_succeeded_installed_csv(object_request, phase):
    cluster = Cluster(object_request, "sample.v7", seven_links()[2:], replaces="sample.v6", phase=phase)
    cluster.program(check=True)
    state = cluster.observe()
    assert state["classification"] == "unknown" and state["observations"][0]["ownership"] == "unknown"
    assert all("chainGap" not in fact for fact in state["olm"])


def test_a_chain_past_sixteen_csvs_is_a_retention_gap(object_request):
    plans = [plan("initial", 1, "sample.v1")] + [plan("upgrade-%02d" % n, n, "sample.v%d" % n, "sample.v%d" % (n - 1)) for n in range(2, 18)]
    cluster = Cluster(object_request, "sample.v17", plans, replaces="sample.v16")
    cluster.program(check=True)
    state = cluster.observe()
    assert state["classification"] == "unknown" and state["olm"][0]["chainGap"] == {"cause": "chainTooLong"}
    assert state["observations"][0]["ownership"] == "own"


def test_the_installed_csvs_own_plan_missing_is_no_retention_gap(object_request):
    plans = two_links()[:2]
    cluster = Cluster(object_request, "sample.v3", plans, replaces="sample.v2")
    cluster.program(check=True)
    state = cluster.observe()
    assert state["classification"] == "unknown" and state["observations"][0]["ownership"] == "unknown"
    assert all("chainGap" not in fact for fact in state["olm"])


@pytest.mark.parametrize("phase", ["Installing", "Failed", "Replacing"])
def test_chain_to_unsucceeded_csv_stays_unknown(object_request, phase):
    cluster = Cluster(object_request, "sample.v3", two_links(), replaces="sample.v2", phase=phase)
    cluster.program()
    assert cluster.observe()["classification"] == "unknown"


def test_pending_upgrade_past_chain_is_reported_and_left(object_request):
    plans = two_links()[:2] + [plan("upgrade-b", 3, "sample.v3", "sample.v2", approved=False)]
    cluster = Cluster(object_request, "sample.v2", plans, replaces="sample.v1", current="sample.v3")
    cluster.program()
    state = cluster.observe()
    assert state["classification"] == "completed"
    assert state["olm"][0]["upgradePending"] is True and [link["csv"] for link in state["olm"][0]["chain"]] == ["sample.v1", "sample.v2"]
    assert plans[2]["spec"]["approved"] is False


def test_operator_managed_chain_reads_complete(object_request):
    cluster = Cluster(object_request, "sample.v3", two_links(), replaces="sample.v2")
    cluster.program(approval="operatorManaged")
    state = cluster.observe()
    assert state["classification"] == "completed"
    assert [link["plan"] for link in state["olm"][0]["chain"]] == ["initial", "upgrade-a", "upgrade-b"]
    assert [p["name"] for p in state["olm"][0]["plans"]] == ["initial", "upgrade-a", "upgrade-b"]


@pytest.mark.parametrize("name", [None, "outside", "collected"])
def test_dependent_chain(object_request, name):
    plans, replaces = broken(name) if name else (two_links(), "sample.v2")
    cluster = Cluster(object_request, "sample.v3", plans, replaces=replaces)
    cluster.program(dependent=True, check=False)
    state = cluster.observe()
    if name:
        assert state["classification"] == "unknown"
        assert state["dependents"][0].get("chainGap") == RETENTION.get(name)
        return
    assert state["classification"] == "completed"
    fact = state["dependents"][0]
    assert fact["installedCSV"] == "sample.v3" and fact["csvVersion"] == "3.0" and fact["uid"] == "uid-1"
    assert [link["csv"] for link in fact["chain"]] == ["sample.v1", "sample.v2", "sample.v3"]


def test_attempt_keeps_its_rules(object_request):
    cluster = Cluster(object_request, "sample.v3", two_links(), replaces="sample.v2")
    cluster.program()
    with pytest.raises(Refused, match="approval proof differs"):
        Engine(object_request, cluster.read, lambda *args: cluster.writes.append(args), clock=lambda: 0).execute()
    assert not cluster.writes


def test_operator_approved_multi_csv_plan_reads_complete(object_request):
    plans = two_links()
    upgrade = plan("upgrade-b", 3, "sample.v3", "sample.v2")
    upgrade["spec"]["clusterServiceVersionNames"] = ["sample.v3", "dependency.v2"]
    upgrade["status"]["bundleLookups"].append({"identifier": "dependency.v2", "replaces": "dependency.v1", "catalogSourceRef": dict(SOURCE)})
    plans[2] = upgrade
    cluster = Cluster(object_request, "sample.v3", plans, replaces="sample.v2")
    cluster.program(approval="operatorManaged")
    state = cluster.observe()
    assert state["classification"] == "completed"
    assert [link["plan"] for link in state["olm"][0]["chain"]] == ["initial", "upgrade-a", "upgrade-b"]


def shared(value):
    value["metadata"]["ownerReferences"] = [{"kind": "Subscription", "uid": "uid-1"}, {"kind": "Subscription", "uid": "uid-other"}]
    return value


def test_plans_owned_by_every_subscription_keep_the_chain(object_request):
    plans = [shared(p) for p in two_links()] + [shared(plan("other-upgrade", 4, "other.v2", "other.v1"))]
    cluster = Cluster(object_request, "sample.v3", plans, replaces="sample.v2")
    cluster.program()
    assert cluster.observe()["classification"] == "completed"


@pytest.mark.parametrize("replaces", ["other.v1", "sample.v1"])
def test_another_subscriptions_plan_never_splices(object_request, replaces):
    plans = [shared(p) for p in two_links()]
    del plans[1]
    plans.append(shared(plan("other-upgrade", 2, "other.v2", replaces)))
    cluster = Cluster(object_request, "sample.v3", plans, replaces="sample.v2")
    cluster.program()
    assert cluster.observe()["classification"] == "unknown"


def test_lookup_without_replaces_stays_unknown(object_request):
    plans = two_links()
    del plans[2]["status"]["bundleLookups"][0]["replaces"]
    cluster = Cluster(object_request, "sample.v3", plans, replaces="sample.v2")
    cluster.program()
    assert cluster.observe()["classification"] == "unknown"


def test_absent_head_csv_stays_unknown(object_request):
    cluster = Cluster(object_request, "sample.v3", two_links(), replaces="sample.v2")
    cluster.csv["metadata"]["name"] = "sample.v9"
    cluster.program()
    assert cluster.observe()["classification"] == "unknown"
