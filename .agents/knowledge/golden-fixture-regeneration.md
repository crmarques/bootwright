# Golden fixtures: regeneration commands and hermeticity

**Render goldens:** regenerate with

```
go test ./internal/render -run TestRenderGoldenFixtures -update
```

Failures read `differs from golden ... (run with -update if intended)` or
`render produced ... which has no golden`. The test renders each fixture twice
from independently loaded state into two different temp dirs and requires
byte-identical output — differing temp dirs also prove no rendered file embeds
its own absolute render path. The full produced tree is golden-pinned with no
exclusions: a new output file with no golden fails, and a golden with no
producer fails.

**Why goldens stay stable across machines:** three hermeticity tricks in
`internal/render/golden_test.go`. `goldenSecretsDir` is a fixed fictitious path
(`render.All` never reads it, only embeds it verbatim); `$HOME` is pinned via
`goldenHome` because fixture secrets declare `file: ~/.ssh/...` sources that
expand through `os.UserHomeDir()`; and the `lookupDate` fields that appear in
lock/vars output are static constants from
`internal/render/inventory/components.go`, not render dates.

**lookupDate constants are freshness stamps, not timestamps:** each records
when the matching pinned component version was last checked upstream. Bump the
constant whenever you update a pin; never derive it from the clock (that would
break golden determinism).

**Ansible UUID pin:** `TestAnsibleUUIDv5MatchesToUUIDFilter` in
`internal/render/inventory/uuid_test.go` pins the renderer's `ansibleUUIDv5`
to Ansible's `to_uuid` Jinja filter under Ansible's default namespace
`361E6D51-FAEC-444A-9079-341386DA8E2E`. substrate_libvirt and boot_redfish
derive libvirt domain UUIDs / Redfish System IDs from
`(cluster.name ~ '-' ~ machine.name) | to_uuid`; if the Go side drifted, the
projected Redfish System ID would no longer match the running libvirt domain
and InsertMedia would target the wrong `/Systems/<id>`. Regenerate an expected
value for a new case with:

```
python3 -c 'import uuid; print(uuid.uuid5(uuid.UUID("361E6D51-FAEC-444A-9079-341386DA8E2E"), "sno-libvirt-master-0"))'
```
