# Native procedure documentation

A named slice revives this design when the owner promotes
[B1034](../milestones/m7.md#b1034): a command that turns one operation plan
into a document an operator can follow by hand. Until then nothing below
exists in code. [State reconciliation](../state-reconciliation.md#plan-and-execution)
owns the operation plan, which the lifecycle already decides for every apply,
destroy and plan preview and which only its executor consumes. This design
adds a second consumer of that value; it changes neither the plan nor the
executor.

The purpose is twofold. The document lets an operator audit, step by step,
what Bootwright would do. It is also a replacement for Bootwright when an
operator cannot run it: every step is a command of a native tool (the
OpenShift installer, `cephadm`, `ceph`, `oc`, `kubectl`) or of a tool a Red Hat
installation ships, with the native input files beside it.

## The consumer

A `render`-class command, such as `bootwright render procedure --output <dir>`
or a verb of its own (the owner's call), reads the same inputs as `plan`. It
registers no operation, allocates no run id and writes only its declared
artifacts, as the [private operation logs rule](../cli/output.md#private-operation-logs) and the
[extension model](../project.md#implementation-and-extension-model) say of
generated outputs. It takes the verb, the selection and the context exactly as
`plan` does, so the document describes the invocation the operator would have
run.

## The entry point

The lifecycle's private operation plan is exported as a method-free
`OperationPlan`, which needs no
[`applicationValues`](../architecture.md#adding-a-lifecycle-capability) entry, and
its durable basis stays unexported. One read-only `Service` method returns it
for a `PlanRequest` without the plan inspection a preview performs. An
application package under `internal/nativeartifacts/` consumes it; that
package imports `lifecycle`, as every capability does. A directory writer
adapter sits beside `internal/nativeartifacts/renderlocal` and carries its
own effect grant, role, dependency row, comment ceiling and package-table row,
and the unwired-package rule applies until the command wires it.

## The port

Only a capability may decode its own frozen request, so the engine cannot
write the steps. An optional capability interface sits beside `Projector` and
`ToolLocator` in `lifecycle/contracts.go`:

```
Procedure(ctx, verb, block, facts) (Procedure, error)
```

It returns a `Procedure` made of:

- `Host`: where the steps run, and `Tools`: the commands they need;
- `Inputs`: native input files, each with `Path`, `MediaType`, `Bytes`,
  `Sensitive` and `Placeholders`;
- `Steps`: each with its `Group`, `Description`, `Host`, `Argv`, `Stdin`,
  `Expect` and `ReplaySafe`;
- `Verify`: the check that proves the block's effect.

The generator discovers the port through `Resolve` and a type assertion. A
block whose implementation lacks it fails the generator, naming the
implementation, so a document is never silently incomplete. The port takes the
frozen `Block`; the capability decodes its own request and nothing else does.

## Facts the engine supplies

The engine passes facts it already holds through existing ports, so the port
need not rediscover them: the hosts of a block (`HostBound.HostMachines`), its
secret references (`SecretReferencer`), the inputs it consumes and the outputs
it provides (`ProducedInputConsumer`, `ProducedOutputProvider`), the stage
tails it consumes (`StageTailConsumer`) and `Removal.ReplaySafe`. The plan adds
the groups, dependencies, exclusive resources, waves, the removal phases and
the authorizations. The procedure follows the plan's order: waves for an
apply, the removal phases for a destroy.

## Output layout

The directory holds:

- `README.md`: the context, verb, mode, input revision, plan digest,
  selection, a wave table, the DAG, the destroy phases and a callout at each
  step that needs an authorization.
- `manifest.json`: the [rendered-file manifest](../milestones/m7.md#b51)
  contract every native file shares: each file, its sha256, its placeholders,
  its block and its chapter.
- `chapters/00-prerequisites.md`: the secrets to supply, the tools and their
  versions, the hosts and the reservations.
- One chapter per block, ordered by plan index and grouped by stage, with a
  section per `Group`.
- `inputs/<block-slug>/<file>`: the native input files. A block slug is the
  block identifier followed by the first 8 hex digits of its request digest.

## Where native inputs come from

Only files Go builds can be reproduced from the plan, and the three origins
differ.

- **Frozen in Go requests.** The installer inputs (install-config,
  agent-config and manifests), the kickstart, NMState and dracut arguments,
  the cephadm service specs and the pool, rule and client parameters, and the
  cluster-object programs. These need argv builders only.
- **Jinja templates in roles.** The libvirt network and domain XML, the sushy
  configuration, every Quadlet unit, and the dnsmasq, squid, chrony and nginx
  configurations. These move into Go builders.
- **Python.** The KubeVirt VM body, the cephadm bootstrap argument vector and
  `ceph.conf`, the Redfish payloads and erase. These move into Go builders.

The move order, each step its own item when B1034 is promoted: the manifest
contract ([B51](../milestones/m7.md#b51)); the installer inputs
([B52](../milestones/m7.md#b52), already Go-frozen); the kickstart, NMState and
dracut procedure; the cephadm bootstrap and `ceph.conf`
([B55](../milestones/m7.md#b55)); the pool script
([B58](../milestones/m7.md#b58), then [B57](../milestones/m7.md#b57)); the
libvirt XML and sushy configuration; the Quadlet units and service
configurations; the KubeVirt VM body; the cluster-object step builders;
Redfish and erase last.

Groups are the only sub-step vocabulary a frozen block carries, and they are
absent for the Ceph capabilities, the package tree, erase, cluster network and
add-on instances. Adding them changes `plan.json`, so each owning item makes
that a deliberate golden update.

## Non-goals

- No secret value is ever written; a placeholder stands where one belongs.
- The generated procedure creates no operation, lease or ownership.
- Drift is not adopted: the document describes the plan, not what the hosts
  hold now.
