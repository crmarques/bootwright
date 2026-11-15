"""Fail-closed traffic acceptance, run on the controller or the libvirt host."""

from __future__ import annotations

import argparse
import ipaddress
import json
import os
from pathlib import Path
import re
import shutil
import stat
import subprocess
import sys
import tempfile
import time


PROBE = r'''
import errno, json, socket, sys, urllib.error, urllib.request
request = urllib.request.Request(sys.argv[1])
if sys.argv[2]:
    request.add_header("Host", sys.argv[2])
class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, fp, code, message, headers, newurl):
        return None
opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
try:
    response = opener.open(request, timeout=4)
    print(json.dumps({"kind": "http", "status": response.status,
                      "body": response.read(65537).decode()}))
except urllib.error.HTTPError as error:
    print(json.dumps({"kind": "http", "status": error.code, "body": ""}))
except (urllib.error.URLError, OSError) as error:
    reason = getattr(error, "reason", error)
    blocked = isinstance(reason, (socket.timeout, TimeoutError)) or (
        isinstance(reason, OSError) and reason.errno in
        (errno.ECONNREFUSED, errno.ENETUNREACH, errno.EHOSTUNREACH,
         errno.ETIMEDOUT))
    print(json.dumps({"kind": "blocked" if blocked else "error",
                      "reason": str(reason)}))
'''


class Failure(Exception):
    """Missing evidence or a traffic assertion that did not hold."""


def run(argv):
    """Bound command duration and retained output; never invoke a shell."""
    try:
        with tempfile.TemporaryFile() as out, tempfile.TemporaryFile() as err:
            result = subprocess.run(argv, stdout=out, stderr=err, timeout=30,
                                    check=False)
            out.seek(0)
            err.seek(0)
            stdout, stderr = out.read(1048577), err.read(65537)
    except (OSError, subprocess.TimeoutExpired) as error:
        raise Failure(f"command failed: {argv[0]}: {error}") from error
    if result.returncode or len(stdout) > 1048576 or len(stderr) > 65536:
        raise Failure(f"command failed or output exceeded limit: {argv[:4]}")
    try:
        return json.loads(stdout)
    except (ValueError, UnicodeError) as error:
        raise Failure(f"command returned invalid JSON: {argv[:4]}") from error


def ipv4(value):
    try:
        return str(ipaddress.IPv4Address(value))
    except (ValueError, TypeError) as error:
        raise Failure(f"expected IPv4 address, got {value!r}") from error


def validate_config(config):
    if set(config["environments"]) != {"staging", "production"}:
        raise Failure("configuration must contain staging and production")
    management = ipaddress.IPv4Address(config["nodeAddress"])
    if not re.fullmatch(r"[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?", config["domain"]):
        raise Failure("domain must be a DNS name")
    if not re.fullmatch(r"[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?", config["nodeName"]):
        raise Failure("nodeName must be a DNS name")
    trunk = config["trunk"]
    names = [trunk["bridge"], trunk["interface"]]
    networks = []
    vlans = []
    for env in config["environments"].values():
        network = ipaddress.IPv4Network(env["cidr"])
        addresses = [ipaddress.IPv4Address(env[key]) for key in
                     ("nodeAddress", "appAddress", "egressAddress", "hostAddress")]
        if len(set(addresses)) != 4 or any(
                address not in network or address in
                (network.network_address, network.broadcast_address)
                for address in addresses):
            raise Failure("environment addresses must be distinct subnet hosts")
        pool = ipaddress.IPv4Network(env["pool"])
        if not pool.subnet_of(network) or any(address in pool for address in addresses):
            raise Failure("MetalLB pool must be inside its subnet and exclude reserved addresses")
        if network.network_address in pool or network.broadcast_address in pool:
            raise Failure("MetalLB pool includes a network or broadcast address")
        if type(env["prefix"]) is not int or env["prefix"] != network.prefixlen:
            raise Failure("environment prefix differs from its CIDR")
        if management in network or any(network.overlaps(ipaddress.IPv4Network(cidr))
                                        for cidr in ("10.128.0.0/14", "172.30.0.0/16")):
            raise Failure("environment overlaps management or the cluster/service networks")
        vlan = env["vlan"]
        if type(vlan) is not int or not 1 <= vlan <= 4094:
            raise Failure("VLAN must be an integer from 1 to 4094")
        vlans.append(vlan)
        names.extend((env["bridge"], f"{trunk['interface']}.{vlan}"))
        networks.append(network)
    if networks[0].overlaps(networks[1]):
        raise Failure("environment subnets overlap")
    if len(set(vlans)) != len(vlans) or len(set(names)) != len(names):
        raise Failure("VLANs and interface names must be distinct")
    if any(not re.fullmatch(r"[A-Za-z][A-Za-z0-9_.-]{0,14}", name) for name in names):
        raise Failure("interfaces must be safe Linux names of at most 15 characters")


def one(items, description):
    if len(items) != 1:
        raise Failure(f"expected exactly one {description}, got {len(items)}")
    return items[0]


def ready_pod(pod):
    if pod["spec"].get("hostNetwork"):
        raise Failure("test workload uses host networking")
    annotations = pod["metadata"].get("annotations", {})
    if annotations.get("k8s.v1.cni.cncf.io/networks"):
        raise Failure("test workload has a secondary-network attachment")
    statuses = json.loads(annotations.get(
        "k8s.v1.cni.cncf.io/network-status", "[]"))
    if any(not status.get("default", False) for status in statuses):
        raise Failure("test workload reports a secondary-network interface")
    conditions = pod["status"].get("conditions", [])
    if not any(c["type"] == "Ready" and c["status"] == "True"
               for c in conditions):
        raise Failure("test workload is not Ready")
    return ipv4(pod["status"]["podIP"])


def discover(config, oc, runner=run):
    def get(*args):
        return runner([oc, "get", *args, "-o", "json"])

    node = get("node", config["nodeName"])
    if "k8s.ovn.org/egress-assignable" not in node["metadata"]["labels"]:
        raise Failure("SNO node is not labelled egress-assignable")
    observed = {}
    for name, env in config["environments"].items():
        egress = get("egressip", name)
        assignments = egress.get("status", {}).get("items", [])
        expected = {"node": config["nodeName"], "egressIP": env["egressAddress"]}
        if assignments != [expected]:
            raise Failure(f"{name}: EgressIP assignment is not {expected}")
        app = one(get("pod", "-n", name, "-l", "app=app")["items"],
                  f"{name} app pod")
        route = get("route", "app", "-n", name)
        admitted = {entry["routerName"] for entry in
                    route.get("status", {}).get("ingress", [])
                    if any(condition["type"] == "Admitted" and
                           condition["status"] == "True" for condition in
                           entry.get("conditions", []))}
        if admitted != {name}:
            raise Failure(f"{name}: route admitted by unexpected routers: {admitted}")
        probe = get("pod", "probe", "-n", name)
        ready_pod(probe)
        service = get("service", "app", "-n", name)
        ingress = get("service", "router-" + name, "-n", "openshift-ingress")
        address = one(ingress.get("status", {}).get("loadBalancer", {}).get(
            "ingress", []), f"{name} ingress address")
        router = one(get("pod", "-n", "openshift-ingress", "-l",
                         "ingresscontroller.operator.openshift.io/"
                         "deployment-ingresscontroller=" + name)["items"],
                     f"{name} router pod")
        http = one([p for p in ingress["spec"]["ports"] if p["name"] == "http"],
                   f"{name} HTTP service port")
        observed[name] = {
            "appPod": ready_pod(app),
            "appService": ipv4(service["spec"]["clusterIP"]),
            "routerLB": ipv4(address["ip"]),
            "routerService": ipv4(ingress["spec"]["clusterIP"]),
            "routerPod": ipv4(router["status"]["podIP"]),
            "routerNodePort": int(http["nodePort"]),
        }
        if ipaddress.ip_address(address["ip"]) not in ipaddress.ip_network(env["cidr"]):
            raise Failure(f"{name}: ingress address is outside its environment")
    return observed


def assert_response(result, allowed, environment=None, source=None):
    if not isinstance(result, dict):
        raise Failure("probe returned a non-object result")
    if not allowed:
        if result.get("kind") != "blocked":
            raise Failure(f"expected blocked connection, got {result}")
        return
    if result.get("kind") != "http" or result.get("status") != 200:
        raise Failure(f"expected HTTP 200, got {result}")
    try:
        body = json.loads(result["body"])
    except (ValueError, TypeError, KeyError) as error:
        raise Failure("application returned malformed JSON") from error
    if not isinstance(body, dict) or body.get("environment") != environment:
        raise Failure(f"wrong application identity: {body}")
    if source and body.get("source") != source:
        raise Failure(f"expected egress source {source}, got {body.get('source')}")


def check(prefix, url, allowed, environment=None, source=None, host="", runner=run):
    result = runner([*prefix, "python3", "-I", "-c", PROBE, url, host])
    assert_response(result, allowed, environment, source)
    print(f"PASS {'allow' if allowed else 'deny'} {prefix[-1]} -> {url}")


def route_host(config, name):
    return f"app.{name}.apps.sno.{config['domain']}"


def test_pods(config, observed, oc, runner=run):
    for name, env in config["environments"].items():
        other = "production" if name == "staging" else "staging"
        peer, foreign = config["environments"][other], observed[other]
        prefix = [oc, "exec", "-n", name, "probe", "--"]
        for phase in ("before", "after"):
            check(prefix, f"http://app.{name}.svc:8080/", True, name, runner=runner)
            check(prefix, f"http://{env['appAddress']}:8080/", True, name,
                  env["egressAddress"], runner=runner)
            check(prefix, f"http://{observed[name]['routerService']}/", True, name,
                  host=route_host(config, name), runner=runner)
            if phase == "after":
                break
            for address in (foreign["appPod"], foreign["appService"],
                            f"app.{other}.svc", peer["appAddress"]):
                check(prefix, f"http://{address}:8080/", False, runner=runner)
            for address in (foreign["routerLB"], foreign["routerService"],
                            foreign["routerPod"]):
                check(prefix, f"http://{address}/", False,
                      host=route_host(config, other), runner=runner)
            check(prefix, f"http://{config['nodeAddress']}:"
                  f"{foreign['routerNodePort']}/", False,
                  host=route_host(config, other), runner=runner)
            check(prefix, f"https://{config['nodeAddress']}:6443/", False,
                  runner=runner)


def container_pid(podman, name, runner):
    pid = runner([podman, "inspect", "--format", "{{json .State.Pid}}",
                  f"e2e-{name}-app"])
    if type(pid) is not int or pid <= 0:
        raise Failure(f"{name}: external container has no running PID")
    return pid


def prove_routes(config, observed, name, pid, nsenter, ip, runner):
    other = "production" if name == "staging" else "staging"
    env = config["environments"][name]
    targets = (config["environments"][other]["appAddress"],
               observed[other]["routerLB"], config["nodeAddress"])
    for target in targets:
        route = one(runner([nsenter, "--target", str(pid), "--net", ip,
                            "-j", "route", "get", ipv4(target)]), "selected route")
        if (route.get("gateway") != env["nodeAddress"] or
                route.get("prefsrc", route.get("src")) != env["appAddress"] or
                route.get("type", "unicast") != "unicast"):
            raise Failure(f"{name}: route to {target} is not through SNO with the peer source")


def test_external(config, observed, podman, runner=run, nsenter=None, ip=None):
    nsenter = nsenter or executable(None, "nsenter")
    ip = ip or executable(None, "ip")
    for name in config["environments"]:
        other = "production" if name == "staging" else "staging"
        pid = container_pid(podman, name, runner)
        prove_routes(config, observed, name, pid, nsenter, ip, runner)
        prefix = [podman, "exec", f"e2e-{name}-app"]
        for phase in ("before", "after"):
            check(prefix, f"http://{observed[name]['routerLB']}/", True, name,
                  host=route_host(config, name), runner=runner)
            if phase == "after":
                break
            check(prefix, f"http://{observed[other]['routerLB']}/", False,
                  host=route_host(config, other), runner=runner)
            check(prefix, "http://" + config["environments"][other]["appAddress"]
                  + ":8080/", False, runner=runner)
            check(prefix, f"http://{config['nodeAddress']}:"
                  f"{observed[other]['routerNodePort']}/", False,
                  host=route_host(config, other), runner=runner)
        if container_pid(podman, name, runner) != pid:
            raise Failure(f"{name}: peer restarted after route proof; rerun acceptance")


def executable(value, name):
    result = shutil.which(value or name)
    if not result:
        raise Failure(f"executable not found: {value or name}")
    return os.path.abspath(result)


def write_snapshot(path, config, observed):
    data = {"config": config, "observed": observed, "created": time.time()}
    descriptor, stage = tempfile.mkstemp(prefix=".snapshot-", dir=path.parent)
    try:
        with os.fdopen(descriptor, "w") as stream:
            json.dump(data, stream, indent=2)
            stream.write("\n")
        os.replace(stage, path)
    finally:
        if os.path.exists(stage):
            os.unlink(stage)


def invalidate_snapshot(path):
    try:
        info = path.lstat()
    except FileNotFoundError:
        return
    if (not stat.S_ISREG(info.st_mode) or info.st_uid != os.geteuid() or
            info.st_mode & 0o077 or info.st_nlink != 1):
        raise Failure("existing snapshot must be a private, singly linked regular file owned by this user")
    path.unlink()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--config", type=Path, required=True)
    parser.add_argument("--mode", choices=("config", "pods", "external"), required=True)
    parser.add_argument("--snapshot", type=Path)
    parser.add_argument("--oc")
    parser.add_argument("--podman")
    args = parser.parse_args()
    if args.mode != "config" and args.snapshot is None:
        parser.error("--snapshot is required for pods and external modes")
    try:
        if args.mode == "pods":
            if args.snapshot.absolute() == args.config.absolute():
                raise Failure("snapshot and configuration paths must differ")
            invalidate_snapshot(args.snapshot)
        config = json.loads(args.config.read_text())
        validate_config(config)
        if args.mode == "config":
            print("PASS: offline configuration validation")
            return 0
        elif args.mode == "pods":
            oc = executable(args.oc, "oc")
            observed = discover(config, oc)
            test_pods(config, observed, oc)
            write_snapshot(args.snapshot, config, observed)
        else:
            snapshot = json.loads(args.snapshot.read_text())
            age = time.time() - snapshot["created"]
            if snapshot["config"] != config or not 0 <= age <= 7200:
                raise Failure("snapshot differs from config or is over two hours old")
            test_external(config, snapshot["observed"], executable(args.podman, "podman"))
        print(f"PASS: all {args.mode} checks")
        return 0
    except (Failure, OSError, ValueError, KeyError, TypeError) as error:
        print(f"FAIL: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
