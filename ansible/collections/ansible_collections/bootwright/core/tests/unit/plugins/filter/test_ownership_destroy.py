from __future__ import annotations

import importlib.util
import json
import pathlib
import unittest


_FILTER_DIR = pathlib.Path(__file__).resolve().parents[4] / "plugins" / "filter"
_spec = importlib.util.spec_from_file_location(
    "_bootwright_ownership_destroy_filter",
    _FILTER_DIR / "ownership_destroy.py",
)
assert _spec and _spec.loader
_module = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(_module)


def _vmedia_unit(extra=""):
    lines = [
        "[Unit]",
        "Description=Bootwright sushy vmedia HTTP (provider-a)",
        "After=network-online.target",
        "Wants=network-online.target",
        "",
        "[Service]",
        "Type=simple",
        "Environment=BOOTWRIGHT_CONTEXT=lab",
        "Environment=BOOTWRIGHT_PROVIDER=provider-a",
        "Environment=BOOTWRIGHT_HOST=bastion",
        "ExecStart=/usr/bin/python3 -m http.server 8001 --bind 127.0.0.1 --directory /var/lib/bootwright/vmedia",
        "Restart=on-failure",
        "RestartSec=5",
        "StandardOutput=append:/var/lib/bootwright/bmc/provider-a/vmedia.log",
        "StandardError=append:/var/lib/bootwright/bmc/provider-a/vmedia.log",
    ]
    if extra:
        lines.append(extra)
    lines.extend(["", "[Install]", "WantedBy=multi-user.target"])
    return "\n".join(lines)


def _sushy_unit(extra=""):
    lines = [
        "[Unit]",
        "Description=Bootwright sushy-emulator (provider-a)",
        "After=libvirtd.service network-online.target",
        "Wants=libvirtd.service network-online.target",
        "",
        "[Service]",
        "Type=simple",
        "Environment=BOOTWRIGHT_CONTEXT=lab",
        "Environment=BOOTWRIGHT_PROVIDER=provider-a",
        "Environment=BOOTWRIGHT_HOST=bastion",
        "Environment=TMPDIR=/var/lib/bootwright/bmc/provider-a/tmp",
        "Environment=TMP=/var/lib/bootwright/bmc/provider-a/tmp",
        "Environment=TEMP=/var/lib/bootwright/bmc/provider-a/tmp",
        "ExecStart=/var/lib/bootwright/bmc/provider-a/venv/bin/sushy-emulator --config /var/lib/bootwright/bmc/provider-a/sushy.conf",
        "Restart=on-failure",
        "RestartSec=5",
        "StandardOutput=append:/var/lib/bootwright/bmc/provider-a/sushy.log",
        "StandardError=append:/var/lib/bootwright/bmc/provider-a/sushy.log",
    ]
    if extra:
        lines.append(extra)
    lines.extend(["", "[Install]", "WantedBy=multi-user.target"])
    return "\n".join(lines)


def _bmc_claim():
    provider = "provider-a"
    context = "lab"
    state_root = "/srv/bootwright/provider-state/bmc/provider-a"
    vmedia_root = "/var/lib/libvirt/images/bootwright/lab/bmc/provider-a/vmedia"
    claim_path = "/var/lib/bootwright/shared-services/bmc-emulator/provider-a"
    return {
        "apiVersion": "bootwright.io/bmc-service-claim/v1alpha1",
        "kind": "bmc-emulator",
        "name": provider,
        "owner": "bootwright",
        "context": context,
        "host": "bastion",
        "provider": provider,
        "paths": [
            state_root,
            vmedia_root,
            "/etc/systemd/system/bootwright-sushy-provider-a.service",
            "/etc/systemd/system/bootwright-vmedia-provider-a.service",
            claim_path,
        ],
        "attributes": {
            "redfishUnit": "bootwright-sushy-provider-a.service",
            "vMediaUnit": "bootwright-vmedia-provider-a.service",
            "redfishPort": "8000",
            "vMediaPort": "8001",
            "libvirtURI": "qemu:///system",
            "bindAddress": "127.0.0.1",
            "authPath": state_root + "/htpasswd",
            "pool": "bootwright-provider-a-vmedia",
            "claimPath": claim_path,
            "stateRoot": state_root,
            "vMediaRoot": vmedia_root,
            "firewallManaged": "true",
        },
    }


def _infra_record(kind):
    provider = "provider-a"
    name = {
        "artifacts": "artifacts-a",
        "load-balancer": "lb-a",
        "nameResolution": "dns-a",
        "ntp": "ntp-a",
        "proxy": "proxy-a",
        "registry": "registry-a",
    }[kind]
    root = "/srv/bootwright/services/" + name
    attrs = {
        "artifacts": {
            "componentKind": kind,
            "container": f"bootwright-artifacts-{provider}-{name}",
            "listenerPorts": "8080/tcp,8443/tcp",
        },
        "load-balancer": {
            "componentKind": kind,
            "container": f"bootwright-haproxy-{provider}-{name}",
            "frontendPorts": "80/tcp,443/tcp",
        },
        "nameResolution": {
            "componentKind": kind,
            "container": f"bootwright-dnsmasq-{provider}-{name}",
            "port": "53/tcp",
            "udpPort": "53/udp",
        },
        "ntp": {
            "componentKind": kind,
            "service": "chronyd",
            "port": "123/udp",
        },
        "proxy": {
            "componentKind": kind,
            "container": f"bootwright-squid-{provider}-{name}",
            "port": "3128/tcp",
        },
        "registry": {
            "componentKind": kind,
            "container": f"bootwright-mirror-registry-{provider}-{name}",
            "port": "5000/tcp",
            "trustAnchor": f"/etc/pki/ca-trust/source/anchors/bootwright-mirror-{provider}-{name}.crt",
            "trustBundleSHA256": "a" * 64,
        },
    }[kind]
    paths = [root]
    if kind == "load-balancer":
        paths.append("/etc/sysctl.d/99-bootwright-haproxy.conf")
    if kind == "ntp":
        paths.append(f"/etc/chrony.d/bootwright-{provider}-{name}.conf")
    return {
        "apiVersion": "bootwright.io/ownership/v1alpha1",
        "kind": "infra-component",
        "name": f"{provider}-{name}",
        "owner": "bootwright",
        "context": "lab",
        "host": "bastion",
        "hostFacts": {
            "bootwright_host_name": "bastion",
            "ansible_host": "192.0.2.10",
            "ansible_user": "root",
            "ansible_connection": "ssh",
            "ansible_ssh_common_args": "",
        },
        "provider": provider,
        "paths": paths,
        "labels": {
            "bootwright.kind": kind,
            "bootwright.provider": provider,
            "bootwright.name": name,
        },
        "attributes": attrs,
        "updatedAt": "2026-08-11T12:00:00Z",
    }


class ExplicitLibvirtAbsence(unittest.TestCase):
    def test_accepts_only_positive_not_found_diagnostics(self):
        self.assertTrue(
            _module.bootwright_libvirt_explicit_absence(
                "error: Domain not found: no domain with matching name 'vm'",
                "libvirt-domain",
            )
        )
        self.assertTrue(
            _module.bootwright_libvirt_explicit_absence(
                "error: Network not found: no network with matching name 'net'",
                "libvirt-network",
            )
        )
        self.assertTrue(
            _module.bootwright_libvirt_explicit_absence(
                "error: Storage pool not found: no storage pool with matching name 'vmedia'",
                "libvirt-pool",
            )
        )
        for value in [
            "error: failed to get domain 'vm'",
            "permission denied while opening domain",
            "failed to connect to the hypervisor",
            "",
        ]:
            with self.subTest(value=value):
                self.assertFalse(
                    _module.bootwright_libvirt_explicit_absence(
                        value, "libvirt-domain"
                    )
                )
        for value in [
            "error: failed to get pool 'vmedia'",
            "permission denied while opening storage pool",
            "failed to connect to the hypervisor",
        ]:
            with self.subTest(value=value):
                self.assertFalse(
                    _module.bootwright_libvirt_explicit_absence(
                        value, "libvirt-pool"
                    )
                )


class PodmanContainerIdentity(unittest.TestCase):
    def test_requires_one_exact_labelled_container(self):
        expected = {
            "bootwright.context": "lab",
            "bootwright.kind": "proxy",
            "bootwright.provider": "provider-a",
            "bootwright.name": "proxy-a",
        }
        value = json.dumps([{"Config": {"Labels": dict(expected)}}])
        self.assertTrue(
            _module.bootwright_podman_container_identity(value, expected)
        )
        cases = [
            "",
            "not-json",
            "{}",
            "[]",
            "[{}, {}]",
            json.dumps([{"Config": {"Labels": {}}}]),
            json.dumps(
                [
                    {
                        "Config": {
                            "Labels": {
                                **expected,
                                "bootwright.context": "foreign",
                            }
                        }
                    }
                ]
            ),
        ]
        for value in cases:
            with self.subTest(value=value):
                self.assertFalse(
                    _module.bootwright_podman_container_identity(value, expected)
                )

    def test_accepts_only_explicit_podman_absence(self):
        for value in [
            "Error: no such container: proxy-a",
            "Error: no such container proxy-a",
            "no container with name or ID proxy-a found",
        ]:
            with self.subTest(value=value):
                self.assertTrue(_module.bootwright_podman_explicit_absence(value))
        for value in [
            "permission denied",
            "cannot connect to Podman",
            "failed to inspect container",
            "",
        ]:
            with self.subTest(value=value):
                self.assertFalse(_module.bootwright_podman_explicit_absence(value))


class BMCIdentities(unittest.TestCase):
    def test_reads_exact_pool_and_unit_identity(self):
        pool = """<pool><metadata><bw:bmcService xmlns:bw="https://bootwright.io/libvirt/metadata/1.0"><bw:context>lab</bw:context><bw:provider>libvirt-a</bw:provider><bw:host>bastion</bw:host></bw:bmcService></metadata></pool>"""
        self.assertEqual(
            _module.bootwright_bmc_pool_identity(pool),
            {},
        )
        pool = """<pool type="dir"><name>bootwright-libvirt-a-vmedia</name><uuid>11111111-2222-3333-4444-555555555555</uuid><capacity unit="bytes">1</capacity><metadata><bw:bmcService xmlns:bw="https://bootwright.io/libvirt/metadata/1.0"><bw:context>lab</bw:context><bw:provider>libvirt-a</bw:provider><bw:host>bastion</bw:host></bw:bmcService></metadata><target><path>/var/lib/libvirt/images/bootwright/lab/bmc/libvirt-a/vmedia</path><permissions><mode>0755</mode></permissions></target></pool>"""
        self.assertEqual(
            _module.bootwright_bmc_pool_identity(pool),
            {
                "context": "lab",
                "provider": "libvirt-a",
                "host": "bastion",
                "type": "dir",
                "name": "bootwright-libvirt-a-vmedia",
                "targetPath": "/var/lib/libvirt/images/bootwright/lab/bmc/libvirt-a/vmedia",
            },
        )
        self.assertEqual(
            _module.bootwright_bmc_pool_uuid(pool),
            "11111111-2222-3333-4444-555555555555",
        )
        unit = _vmedia_unit().replace("provider-a", "libvirt-a")
        self.assertEqual(
            _module.bootwright_systemd_unit_identity(unit),
            {"context": "lab", "provider": "libvirt-a", "host": "bastion"},
        )

    def test_rejects_missing_duplicate_unknown_or_malformed_evidence(self):
        pools = [
            "",
            "<pool>",
            "<pool/>",
            "<pool><metadata><bmcService/></metadata></pool>",
            """<pool><metadata><bw:bmcService xmlns:bw="https://bootwright.io/libvirt/metadata/1.0"><bw:context>lab</bw:context><bw:provider>libvirt-a</bw:provider></bw:bmcService></metadata></pool>""",
            """<pool><metadata><bw:bmcService xmlns:bw="https://bootwright.io/libvirt/metadata/1.0"><bw:context>lab</bw:context><bw:context>other</bw:context><bw:provider>libvirt-a</bw:provider><bw:host>bastion</bw:host></bw:bmcService></metadata></pool>""",
            """<pool type="dir"><name>decoy</name><name>other</name><metadata><bw:bmcService xmlns:bw="https://bootwright.io/libvirt/metadata/1.0"><bw:context>lab</bw:context><bw:provider>libvirt-a</bw:provider><bw:host>bastion</bw:host></bw:bmcService></metadata><target><path>/tmp/decoy</path></target></pool>""",
            """<pool type="dir"><name>decoy</name><source><name>nested</name></source><metadata><bw:bmcService xmlns:bw="https://bootwright.io/libvirt/metadata/1.0"><bw:context>lab</bw:context><bw:provider>libvirt-a</bw:provider><bw:host>bastion</bw:host></bw:bmcService></metadata><target><path>/tmp/decoy</path></target></pool>""",
            """<pool type="dir"><name>decoy</name><uuid>not-a-uuid</uuid><metadata><bw:bmcService xmlns:bw="https://bootwright.io/libvirt/metadata/1.0"><bw:context>lab</bw:context><bw:provider>libvirt-a</bw:provider><bw:host>bastion</bw:host></bw:bmcService></metadata><target><path>/tmp/decoy</path></target></pool>""",
        ]
        for value in pools:
            with self.subTest(value=value):
                self.assertEqual(_module.bootwright_bmc_pool_identity(value), {})
                self.assertEqual(_module.bootwright_bmc_pool_uuid(value), "")
        for value in [
            "",
            "Environment=BOOTWRIGHT_CONTEXT=lab",
            _vmedia_unit("Environment=BOOTWRIGHT_CONTEXT=other"),
            _vmedia_unit("EnvironmentFile=/tmp/foreign"),
            _vmedia_unit("ExecStop = /usr/bin/foreign"),
            _vmedia_unit("Environment=LD_PRELOAD=/tmp/foreign.so"),
            _vmedia_unit("Environment=PYTHONPATH=/tmp/foreign"),
            _vmedia_unit("User=foreign"),
            _vmedia_unit("RootDirectory=/tmp/foreign"),
            _vmedia_unit("StandardOutput=journal"),
        ]:
            with self.subTest(value=value):
                self.assertEqual(
                    _module.bootwright_systemd_unit_identity(value), {}
                )

    def test_reads_exact_bmc_runtime_composite(self):
        config = "\n".join(
            [
                "SUSHY_EMULATOR_LIBVIRT_URI = u'qemu:///system'",
                "SUSHY_EMULATOR_FEATURE_SET = u'vmedia'",
                "SUSHY_EMULATOR_STATE_DIR = u'/var/lib/bootwright/bmc/provider-a/state'",
                "SUSHY_EMULATOR_STORAGE_POOL = u'bootwright-provider-a-vmedia'",
                "SUSHY_EMULATOR_LISTEN_IP = u'127.0.0.1'",
                "SUSHY_EMULATOR_LISTEN_PORT = 8000",
                "SUSHY_EMULATOR_AUTH_FILE = u'/var/lib/bootwright/bmc/provider-a/htpasswd'",
            ]
        )
        self.assertEqual(
            _module.bootwright_bmc_sushy_config_identity(config),
            {
                "libvirtURI": "qemu:///system",
                "stateRoot": "/var/lib/bootwright/bmc/provider-a",
                "pool": "bootwright-provider-a-vmedia",
                "bindAddress": "127.0.0.1",
                "redfishPort": "8000",
                "authPath": "/var/lib/bootwright/bmc/provider-a/htpasswd",
            },
        )
        unit = _vmedia_unit()
        self.assertEqual(
            _module.bootwright_bmc_vmedia_unit_runtime(unit),
            {
                "context": "lab",
                "provider": "provider-a",
                "host": "bastion",
                "vMediaPort": "8001",
                "vMediaRoot": "/var/lib/bootwright/vmedia",
            },
        )
        sushy = _sushy_unit()
        self.assertEqual(
            _module.bootwright_bmc_sushy_unit_runtime(sushy),
            {
                "context": "lab",
                "provider": "provider-a",
                "host": "bastion",
                "stateRoot": "/var/lib/bootwright/bmc/provider-a",
            },
        )
        self.assertEqual(
            _module.bootwright_bmc_sushy_unit_runtime(
                _sushy_unit("ExecStart=/usr/bin/foreign")
            ),
            {},
        )
        for value in [
            _sushy_unit("Environment=LD_PRELOAD=/tmp/foreign.so"),
            _sushy_unit("ExecStartPre=/usr/bin/foreign"),
            _sushy_unit("ExecStop = /usr/bin/foreign"),
            _sushy_unit("User=root"),
            _sushy_unit().replace("RestartSec=5", "RestartSec=6"),
            _sushy_unit().replace(
                "StandardError=append:/var/lib/bootwright/bmc/provider-a/sushy.log",
                "StandardError=journal",
            ),
            _sushy_unit().replace(
                "Environment=TEMP=/var/lib/bootwright/bmc/provider-a/tmp",
                "Environment=TEMP=/tmp/foreign",
            ),
        ]:
            with self.subTest(value=value):
                self.assertEqual(_module.bootwright_bmc_sushy_unit_runtime(value), {})
        for value in [
            "",
            config.replace("8000", "bad"),
            config + "\n" + config,
            config + "\nprint('foreign')",
            config + "\nFOREIGN_SETTING = u'unsafe'",
            config.replace("u'vmedia'", "u'full'"),
            config.replace("u'127.0.0.1'", "'127.0.0.1'"),
            config.replace(
                "u'/var/lib/bootwright/bmc/provider-a/htpasswd'",
                "u'/tmp/foreign'",
            ),
            config.replace("8000", "65536"),
        ]:
            with self.subTest(value=value):
                self.assertEqual(
                    _module.bootwright_bmc_sushy_config_identity(value), {}
                )
        self.assertEqual(
            _module.bootwright_bmc_sushy_config_identity(
                config + '\nSUSHY_EMULATOR_LISTEN_PORT = "9000"'
            ),
            {},
        )
        self.assertEqual(
            _module.bootwright_bmc_sushy_config_identity(
                config.replace(
                    "SUSHY_EMULATOR_AUTH_FILE = u'/var/lib/bootwright/bmc/provider-a/htpasswd'",
                    "",
                )
            ),
            {
                "libvirtURI": "qemu:///system",
                "stateRoot": "/var/lib/bootwright/bmc/provider-a",
                "pool": "bootwright-provider-a-vmedia",
                "bindAddress": "127.0.0.1",
                "redfishPort": "8000",
                "authPath": "",
            },
        )


class BMCClaimIdentity(unittest.TestCase):
    def classify(self, claim, state_dir="/srv/bootwright/provider-state"):
        return _module.bootwright_bmc_claim_identity(
            json.dumps(claim), "lab", "provider-a", "bastion", state_dir
        )

    def test_accepts_only_exact_full_provider_composite(self):
        claim = _bmc_claim()
        self.assertEqual(self.classify(claim), claim)
        claim["attributes"]["authPath"] = ""
        claim["attributes"]["firewallManaged"] = "false"
        self.assertEqual(self.classify(claim), claim)

    def test_rejects_authority_shape_path_and_attribute_mutations(self):
        cases = []
        for field, value in [
            ("apiVersion", "foreign/v1"),
            ("kind", "foreign"),
            ("name", "provider-b"),
            ("owner", "foreign"),
            ("context", "other"),
            ("host", "other"),
            ("provider", "provider-b"),
        ]:
            claim = _bmc_claim()
            claim[field] = value
            cases.append(claim)
        for field, value in [
            ("redfishUnit", "foreign.service"),
            ("vMediaUnit", "foreign.service"),
            ("redfishPort", "0"),
            ("redfishPort", "65536"),
            ("redfishPort", 8000),
            ("vMediaPort", "8000"),
            ("libvirtURI", " qemu:///system"),
            ("bindAddress", ""),
            ("authPath", "/tmp/foreign"),
            ("pool", "foreign"),
            ("claimPath", "/tmp/foreign"),
            ("stateRoot", "/tmp/foreign"),
            ("vMediaRoot", "/tmp/foreign"),
            ("firewallManaged", True),
            ("firewallManaged", "yes"),
        ]:
            claim = _bmc_claim()
            claim["attributes"][field] = value
            cases.append(claim)
        claim = _bmc_claim()
        claim["paths"][0] = "/tmp/foreign"
        cases.append(claim)
        claim = _bmc_claim()
        claim["extra"] = "unsafe"
        cases.append(claim)
        claim = _bmc_claim()
        claim["attributes"]["extra"] = "unsafe"
        cases.append(claim)
        claim = _bmc_claim()
        del claim["attributes"]["pool"]
        cases.append(claim)
        for claim in cases:
            with self.subTest(claim=claim):
                self.assertEqual(self.classify(claim), {})
        for value in ["", "not-json", "[]", "{}"]:
            with self.subTest(value=value):
                self.assertEqual(
                    _module.bootwright_bmc_claim_identity(
                        value,
                        "lab",
                        "provider-a",
                        "bastion",
                        "/srv/bootwright/provider-state",
                    ),
                    {},
                )
        self.assertEqual(self.classify(_bmc_claim(), "/srv/../foreign"), {})


class SystemdAndMountEvidence(unittest.TestCase):
    def test_systemd_show_requires_exact_loaded_fragment_or_positive_absence(self):
        path = "/etc/systemd/system/bootwright-sushy-provider-a.service"
        self.assertEqual(
            _module.bootwright_systemd_show_identity(
                "LoadState=loaded\nFragmentPath=" + path, path
            ),
            {"present": True, "loadState": "loaded", "fragmentPath": path},
        )
        self.assertEqual(
            _module.bootwright_systemd_show_identity(
                "LoadState=not-found\nFragmentPath=", path
            ),
            {"present": False, "loadState": "not-found", "fragmentPath": ""},
        )
        for value in [
            "",
            "LoadState=loaded",
            "LoadState=loaded\nFragmentPath=/tmp/foreign",
            "LoadState=masked\nFragmentPath=" + path,
            "LoadState=not-found\nFragmentPath=" + path,
            "LoadState=loaded\nLoadState=loaded\nFragmentPath=" + path,
            "LoadState=loaded\nFragmentPath=" + path + "\nUnitFileState=enabled",
        ]:
            with self.subTest(value=value):
                self.assertEqual(
                    _module.bootwright_systemd_show_identity(value, path), {}
                )

    def test_mount_targets_require_exact_normalized_targets_below_roots(self):
        roots = [
            "/srv/bootwright/provider-state/bmc/provider-a",
            "/var/lib/libvirt/images/bootwright/lab/bmc/provider-a/vmedia",
        ]
        self.assertEqual(
            _module.bootwright_mount_targets("", roots),
            {"valid": True, "targets": []},
        )
        self.assertEqual(
            _module.bootwright_mount_targets(
                roots[1] + "/nested\n" + roots[0], roots
            ),
            {"valid": True, "targets": [roots[0], roots[1] + "/nested"]},
        )
        for value in [
            "/tmp/foreign",
            roots[0] + "/../foreign",
            roots[0] + "\n" + roots[0],
            roots[0] + " ",
            "relative/path",
        ]:
            with self.subTest(value=value):
                self.assertEqual(_module.bootwright_mount_targets(value, roots), {})
        for bad_roots in [[], ["/"], ["relative"], [roots[0], roots[0]]]:
            with self.subTest(roots=bad_roots):
                self.assertEqual(
                    _module.bootwright_mount_targets(roots[0], bad_roots), {}
                )


class DeclaredManagedOSPaths(unittest.TestCase):
    def test_accepts_only_desired_root_and_exact_stage_template(self):
        root = "/state/os-install/ceph-a/node-a"
        stage = "/services/artifacts/public/__BOOTWRIGHT_AGENT_ISO_PUBLISH_TOKEN__/os-node-a.iso"
        self.assertTrue(
            _module.bootwright_declared_managed_os_paths(
                [root, "/services/artifacts/public/deadbeef/os-node-a.iso"],
                root,
                stage,
            )
        )
        for value in [
            [],
            [root, root],
            [root, "/services/artifacts/public/../foreign.iso"],
            [root, "/var/lib/foreign"],
        ]:
            with self.subTest(value=value):
                self.assertFalse(
                    _module.bootwright_declared_managed_os_paths(
                        value, root, stage
                    )
                )


class InfraComponentRecordComposite(unittest.TestCase):
    managed = "/srv/bootwright/services"

    def classify(self, value):
        return _module.bootwright_infra_component_record_composite(
            value, self.managed
        )

    def test_accepts_only_the_six_exact_writer_composites(self):
        expected = {
            "artifacts": (
                "bootwright.core.infra_component_artifact_server_http",
                ["8080/tcp", "8443/tcp"],
            ),
            "load-balancer": (
                "bootwright.core.infra_component_load_balancer_haproxy",
                ["80/tcp", "443/tcp"],
            ),
            "nameResolution": (
                "bootwright.core.infra_component_name_resolution_dnsmasq",
                ["53/tcp", "53/udp"],
            ),
            "ntp": (
                "bootwright.core.infra_component_ntp_chrony",
                ["123/udp"],
            ),
            "proxy": (
                "bootwright.core.infra_component_proxy_squid",
                ["3128/tcp"],
            ),
            "registry": (
                "bootwright.core.infra_component_registry_mirror",
                ["5000/tcp"],
            ),
        }
        for kind, (role, ports) in expected.items():
            with self.subTest(kind=kind):
                record = _infra_record(kind)
                result = self.classify(record)
                self.assertEqual(result["role"], role)
                self.assertEqual(result["kind"], kind)
                self.assertEqual(result["ports"], ports)
                self.assertEqual(result["record"], record)
                self.assertFalse(result["phaseComplete"])
                record["attributes"]["destroyPhase"] = "external-cleanup-complete"
                result = self.classify(record)
                self.assertTrue(result["phaseComplete"])
                self.assertEqual(result["record"], record)

    def test_rejects_unknown_missing_noncanonical_or_contradictory_fields(self):
        cases = []
        for kind in [
            "artifacts",
            "load-balancer",
            "nameResolution",
            "ntp",
            "proxy",
            "registry",
        ]:
            record = _infra_record(kind)
            record["extra"] = "unsafe"
            cases.append(record)
            record = _infra_record(kind)
            record["labels"]["extra"] = "unsafe"
            cases.append(record)
            record = _infra_record(kind)
            record["attributes"]["extra"] = "unsafe"
            cases.append(record)
            record = _infra_record(kind)
            record["attributes"]["destroyPhase"] = "unknown"
            cases.append(record)
            record = _infra_record(kind)
            record["paths"][0] = "/tmp/foreign"
            cases.append(record)
        for kind, field, value in [
            ("artifacts", "listenerPorts", "8443/tcp,8080/tcp"),
            ("artifacts", "listenerPorts", "8080/tcp,8080/tcp"),
            ("load-balancer", "frontendPorts", "0/tcp"),
            ("load-balancer", "frontendPorts", "65536/tcp"),
            ("nameResolution", "udpPort", "53/tcp"),
            ("ntp", "service", "foreign"),
            ("ntp", "port", 123),
            ("proxy", "port", "3128/udp"),
            ("registry", "trustAnchor", "/tmp/foreign"),
            ("registry", "trustBundleSHA256", "bad"),
        ]:
            record = _infra_record(kind)
            record["attributes"][field] = value
            cases.append(record)
        for record in cases:
            with self.subTest(record=record):
                self.assertEqual(self.classify(record), {})

    def test_is_nonthrowing_for_malformed_inputs(self):
        values = [None, "", "record", [], [1], 1, True, {}, {"kind": []}]
        for value in values:
            with self.subTest(value=value):
                self.assertEqual(self.classify(value), {})
        record = _infra_record("proxy")
        for managed in [None, "", "/", "relative", "/srv/../tmp"]:
            with self.subTest(managed=managed):
                self.assertEqual(
                    _module.bootwright_infra_component_record_composite(
                        record, managed
                    ),
                    {},
                )

    def test_endpoint_conflicts_name_exact_selected_and_surviving_owners(self):
        selected = self.classify(_infra_record("artifacts"))
        survivor = self.classify(_infra_record("proxy"))
        survivor["ports"] = ["8080/tcp"]
        result = _module.bootwright_infra_component_endpoint_conflicts(
            [selected, survivor], [selected["name"]]
        )
        self.assertEqual(
            result,
            {
                "valid": True,
                "conflicts": [
                    {
                        "port": "8080/tcp",
                        "selected": selected["name"],
                        "survivor": survivor["name"],
                    }
                ],
            },
        )
        self.assertEqual(
            _module.bootwright_infra_component_endpoint_conflicts(
                [selected, survivor], [selected["name"], survivor["name"]]
            ),
            {"valid": True, "conflicts": []},
        )
        for composites, names in [
            (None, []),
            ([{}], []),
            ([selected, selected], [selected["name"]]),
            ([selected], ["missing"]),
            ([selected], [selected["name"], selected["name"]]),
        ]:
            with self.subTest(composites=composites, names=names):
                self.assertEqual(
                    _module.bootwright_infra_component_endpoint_conflicts(
                        composites, names
                    ),
                    {},
                )


class HostSharedServiceAuthority(unittest.TestCase):
    def lease(self, command="apply"):
        return {
            "apiVersion": "bootwright.io/host-mutation-lease/v1alpha1",
            "kind": "host-mutation-lease",
            "token": "sha256:" + "a" * 64,
            "runId": command + "-run",
            "command": command,
            "controller": "controller-a",
            "pid": 123,
            "processStart": "456",
            "startedAt": "2026-08-11T12:00:00.123456789Z",
        }

    def selection(self, command="apply"):
        return {
            "apiVersion": "bootwright.io/host-shared-service-selection/v1alpha1",
            "kind": "host-shared-service-selection",
            "context": "lab",
            "command": command,
            "host": "bastion",
            "consequences": [
                {
                    "kind": "bmc-emulator",
                    "name": "provider-a",
                    "selectionDigests": ["sha256:" + "a" * 64],
                    "claimDigests": ["sha256:" + "b" * 64],
                },
                {
                    "kind": "infra-component",
                    "name": "provider-a-proxy-a",
                    "selectionDigests": ["sha256:" + "c" * 64],
                },
            ],
        }

    def test_operation_guard_binds_unique_lease_and_full_sorted_host_selection(self):
        guard = _module.bootwright_host_operation_guard(
            self.lease(), self.selection(), "bootwright apply lab"
        )
        self.assertEqual(
            _module.bootwright_host_operation_guard_any(json.dumps(guard)), guard
        )
        self.assertEqual(guard["selection"], self.selection())
        self.assertEqual(
            guard["selectionDigest"],
            _module.bootwright_bmc_claim_digest(self.selection()),
        )
        changed = json.loads(json.dumps(guard))
        changed["lease"]["token"] = "sha256:" + "b" * 64
        self.assertNotEqual(
            _module.bootwright_host_operation_guard_any(json.dumps(changed)), guard
        )

    def test_operation_guard_rejects_future_or_contradictory_shapes(self):
        cases = []
        selection = self.selection()
        selection["consequences"].reverse()
        cases.append(
            _module.bootwright_host_operation_guard(
                self.lease(), selection, "bootwright apply lab"
            )
        )
        selection = self.selection()
        selection["command"] = "destroy"
        cases.append(
            _module.bootwright_host_operation_guard(
                self.lease(), selection, "bootwright destroy lab"
            )
        )
        guard = _module.bootwright_host_operation_guard(
            self.lease(), self.selection(), "bootwright apply lab"
        )
        guard["future"] = True
        cases.append(guard)
        for value in cases:
            with self.subTest(value=value):
                self.assertEqual(
                    _module.bootwright_host_operation_guard_any(value), {}
                )

    def test_endpoint_claims_are_global_across_logical_families(self):
        digest = "sha256:" + "c" * 64
        bmc = _module.bootwright_host_endpoint_claim(
            "tcp", 8000, "bmc-emulator", "provider-a", "lab", "bastion", digest
        )
        infra = _module.bootwright_host_endpoint_claim(
            "tcp",
            8000,
            "infra-component",
            "provider-b-proxy",
            "lab",
            "bastion",
            digest,
        )
        self.assertTrue(bmc)
        self.assertTrue(infra)
        self.assertNotEqual(bmc["owner"], infra["owner"])
        self.assertEqual(
            _module.bootwright_host_endpoint_claim_any(json.dumps(bmc)), bmc
        )
        self.assertEqual(
            _module.bootwright_host_endpoint_claim_set([bmc, infra]), {}
        )
        grouped = _module.bootwright_host_endpoint_claim_groups([bmc, infra])
        self.assertTrue(grouped["valid"])
        self.assertEqual(len(grouped["groups"]), 1)
        self.assertEqual(grouped["groups"][0]["claims"], [bmc, infra])

        first = _module.bootwright_host_endpoint_registry_next(
            {}, [bmc], [digest]
        )
        self.assertTrue(first["valid"])
        self.assertEqual(first["conflicts"], [])
        conflict = _module.bootwright_host_endpoint_registry_next(
            first["registry"], [infra], [digest]
        )
        self.assertTrue(conflict["valid"])
        self.assertEqual(
            conflict["conflicts"],
            [{"protocol": "tcp", "port": 8000, "owner": bmc["owner"]}],
        )
        removed = _module.bootwright_host_endpoint_registry_remove(
            first["registry"], [bmc]
        )
        self.assertEqual(removed, {"valid": True, "registry": {}})

    def test_endpoint_registry_atomically_publishes_complete_multiport_set(self):
        claim = _bmc_claim()
        transition = _module.bootwright_bmc_endpoint_transition(claim, claim)
        result = _module.bootwright_host_endpoint_registry_next(
            {}, transition["retainedClaims"], transition["allowedDigests"]
        )
        self.assertTrue(result["valid"])
        self.assertEqual(
            [item["port"] for item in result["registry"]["claims"]],
            [8000, 8001],
        )
        self.assertEqual(
            _module.bootwright_host_endpoint_registry_any(
                json.dumps(result["registry"])
            ),
            result["registry"],
        )

    def test_endpoint_registry_transition_retires_only_exact_old_slots(self):
        active = _bmc_claim()
        pending = json.loads(json.dumps(active))
        pending["attributes"]["redfishPort"] = "9000"
        pending["attributes"]["vMediaPort"] = "9001"
        transition = _module.bootwright_bmc_endpoint_transition(active, pending)
        reserved = _module.bootwright_host_endpoint_registry_next(
            {}, transition["retainedClaims"], transition["allowedDigests"]
        )
        self.assertEqual(
            [item["port"] for item in reserved["registry"]["claims"]],
            [8000, 8001, 9000, 9001],
        )
        retired = _module.bootwright_host_endpoint_registry_remove(
            reserved["registry"], transition["oldOnlyCandidates"]
        )
        self.assertTrue(retired["valid"])
        self.assertEqual(
            [item["port"] for item in retired["registry"]["claims"]],
            [9000, 9001],
        )

        mismatched = json.loads(json.dumps(reserved["registry"]))
        mismatched["claims"][0]["claimDigest"] = "sha256:" + "f" * 64
        self.assertEqual(
            _module.bootwright_host_endpoint_registry_remove(
                mismatched, transition["oldOnlyCandidates"]
            ),
            {},
        )

    def test_bmc_endpoint_transition_retains_union_and_handles_port_swap(self):
        active = _bmc_claim()
        pending = json.loads(json.dumps(active))
        pending["attributes"]["redfishPort"] = "8001"
        pending["attributes"]["vMediaPort"] = "8000"
        transition = _module.bootwright_bmc_endpoint_transition(active, pending)
        self.assertTrue(transition["valid"])
        self.assertEqual(
            [item["port"] for item in transition["desiredClaims"]], [8000, 8001]
        )
        self.assertEqual(
            [item["port"] for item in transition["retainedClaims"]], [8000, 8001]
        )
        self.assertEqual(transition["oldOnlyCandidates"], [])
        self.assertEqual(len(transition["allCandidates"]), 4)

    def test_podman_context_classifies_missing_as_valid_unclaimed(self):
        payload = json.dumps([{"Config": {"Labels": {}}}])
        self.assertEqual(
            _module.bootwright_podman_container_context(payload),
            {"valid": True, "context": ""},
        )
        payload = json.dumps(
            [{"Config": {"Labels": {"bootwright.context": "lab"}}}]
        )
        self.assertEqual(
            _module.bootwright_podman_container_context(payload),
            {"valid": True, "context": "lab"},
        )

    def test_infra_record_digests_strip_only_completed_destroy_phase(self):
        for kind in [
            "artifacts",
            "load-balancer",
            "nameResolution",
            "ntp",
            "proxy",
            "registry",
        ]:
            with self.subTest(kind=kind):
                record = _infra_record(kind)
                initial = _module.bootwright_infra_component_record_digests(
                    record, "/srv/bootwright/services"
                )
                self.assertTrue(initial["valid"])
                self.assertEqual(initial["selectionDigest"], initial["claimDigest"])
                record["attributes"]["destroyPhase"] = "external-cleanup-complete"
                completed = _module.bootwright_infra_component_record_digests(
                    record, "/srv/bootwright/services"
                )
                self.assertEqual(completed, initial)

    def test_infra_selection_digest_matches_cross_language_canonical_json(self):
        component = {
            "kind": "proxy",
            "providerName": "provider-a",
            "name": "proxy-a",
            "machineRef": "bastion",
            "applyRole": "bootwright.core.infra_component_proxy_squid",
            "destroyRole": "bootwright.core.infra_component_proxy_squid",
            "proxyURL": "http://proxy/?a=1&b=2",
        }
        self.assertEqual(
            _module.bootwright_infra_component_selection_digest(
                component, "bastion"
            ),
            "sha256:cefc3a3be08585d585698752ef8b3207e7ebfb7706fb34a1b1cc2445129a8069",
        )

    def test_cross_family_prepublication_refuses_claim_only_endpoint_conflicts(self):
        infra_record = _infra_record("proxy")
        infra_side = _module.bootwright_infra_component_record_digests(
            infra_record, "/srv/bootwright/services"
        )["side"]
        infra_claim = {
            "apiVersion": "bootwright.io/infra-component-service-claim/v1alpha1",
            "kind": "infra-component",
            "name": infra_record["name"],
            "owner": "bootwright",
            "context": "lab",
            "host": "bastion",
            "state": "steady",
            "active": infra_side,
        }
        bmc_claim = _bmc_claim()
        bmc_claim["attributes"]["redfishPort"] = "3128"

        bmc_candidate = _module.bootwright_host_shared_service_prepublication_conflicts(
            "bmc-emulator", bmc_claim, [], [infra_claim], {}
        )
        self.assertTrue(bmc_candidate["valid"])
        self.assertEqual(
            bmc_candidate["conflicts"][0]["resources"], ["endpoint:3128/tcp"]
        )

        artifact_record = _infra_record("artifacts")
        artifact_record["attributes"]["listenerPorts"] = "8000/tcp,8443/tcp"
        artifact_side = _module.bootwright_infra_component_record_digests(
            artifact_record, "/srv/bootwright/services"
        )["side"]
        artifact_claim = {
            "apiVersion": "bootwright.io/infra-component-service-claim/v1alpha1",
            "kind": "infra-component",
            "name": artifact_record["name"],
            "owner": "bootwright",
            "context": "lab",
            "host": "bastion",
            "state": "steady",
            "active": artifact_side,
        }
        infra_candidate = _module.bootwright_host_shared_service_prepublication_conflicts(
            "infra-component", artifact_claim, [_bmc_claim()], [], {}
        )
        self.assertTrue(infra_candidate["valid"])
        self.assertEqual(
            infra_candidate["conflicts"][0]["resources"], ["endpoint:8000/tcp"]
        )

    def test_cross_family_prepublication_is_nonthrowing_and_registry_aware(self):
        record = _infra_record("proxy")
        side = _module.bootwright_infra_component_record_digests(
            record, "/srv/bootwright/services"
        )["side"]
        candidate = {
            "apiVersion": "bootwright.io/infra-component-service-claim/v1alpha1",
            "kind": "infra-component",
            "name": record["name"],
            "owner": "bootwright",
            "context": "lab",
            "host": "bastion",
            "state": "steady",
            "active": side,
        }
        endpoint = _module.bootwright_host_endpoint_claim(
            "tcp",
            3128,
            "bmc-emulator",
            "provider-b",
            "other",
            "bastion",
            "sha256:" + "a" * 64,
        )
        registry = _module.bootwright_host_endpoint_registry_next(
            {}, [endpoint], ["sha256:" + "a" * 64]
        )["registry"]
        decision = _module.bootwright_host_shared_service_prepublication_conflicts(
            "infra-component", candidate, [], [], registry
        )
        self.assertTrue(decision["valid"])
        self.assertEqual(decision["conflicts"][0]["source"], "endpoint-registry")
        for value in [None, {}, [], "bad"]:
            with self.subTest(value=value):
                self.assertEqual(
                    _module.bootwright_host_shared_service_prepublication_conflicts(
                        "infra-component", value, [], [], {}
                    ),
                    {},
                )

    def test_shared_service_scan_paths_requires_closed_root_owned_topology(self):
        missing = {"exists": False}
        transition_missing = {
            "exists": False,
            "path": "/srv/bootwright/ownership/transitions/infra-component",
        }
        self.assertEqual(
            _module.bootwright_host_shared_service_scan_paths(
                missing, [], missing, [], transition_missing, []
            ),
            {"valid": True, "bmcDocumentPaths": [], "infraDocumentPaths": []},
        )
        root = {
            "exists": True,
            "isdir": True,
            "islnk": False,
            "pw_name": "root",
            "gr_name": "root",
            "mode": "0700",
        }
        provider_path = "/var/lib/bootwright/shared-services/bmc-emulator/provider-a"
        provider = {
            **root,
            "path": provider_path,
        }
        claim_path = provider_path + "/claim.json"
        claim = {
            "path": claim_path,
            "isreg": True,
            "islnk": False,
            "pw_name": "root",
            "gr_name": "root",
            "mode": "0600",
        }
        result = _module.bootwright_host_shared_service_scan_paths(
            root, [claim, provider], missing, [], transition_missing, []
        )
        self.assertEqual(result["bmcDocumentPaths"], [claim_path])
        linked = dict(claim)
        linked["islnk"] = True
        self.assertEqual(
            _module.bootwright_host_shared_service_scan_paths(
                root, [provider, linked], missing, [], transition_missing, []
            ),
            {},
        )
        unknown = dict(claim)
        unknown["path"] = provider_path + "/future.json"
        self.assertEqual(
            _module.bootwright_host_shared_service_scan_paths(
                root, [provider, unknown], missing, [], transition_missing, []
            ),
            {},
        )


if __name__ == "__main__":
    unittest.main()
