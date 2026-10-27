# Single-node OpenShift with Kubernetes NMState

This self-contained input extends [lab-sno](../../openshift-clusters/lab-sno/README.md)
with one binding selecting `nmstate`. Read that runbook's controller, network,
Secret and capacity prerequisites, and make the same local substitutions.
All addresses and identities here are synthetic. Operator acceptance remains
pending; this example supplies the input for the M4 rehearsal.

Copy this example outside Git before customizing it:

```sh
addon_input=$(mktemp -d /tmp/bootwright-addons.XXXXXX)
cp -R examples/add-ons/lab-sno-nmstate/. "$addon_input/"
```

Replace `example-channel` and `kubernetes-nmstate-operator.v0.0.0` in `add-ons/binding.yaml` with
an operator-selected channel and its exact CSV. These are illustrative values,
not defaults or a qualified release pairing. On a cluster whose selected
catalog serves the package, inspect its channels with:

```sh
oc get packagemanifest kubernetes-nmstate-operator -n openshift-marketplace -o json
```

Read `status.channels[].name` and `status.channels[].currentCSV`. Set the
channel and CSV together; Bootwright approves a Manual InstallPlan only when
it resolves exactly the declared CSV from the declared catalog.

Validate and import the customized copy, then follow lab-sno's Secret setup
and controller setup instructions for the new context:

```sh
bootwright validate --file "$addon_input"
bootwright context init --name addons-nmstate --input-dir "$addon_input"
bootwright secret generate --context addons-nmstate
bootwright setup
bootwright plan --context addons-nmstate
bootwright apply --context addons-nmstate
bootwright diff --context addons-nmstate --observe
```

The initial apply installs the operator and its operand after the cluster.
Repeat an interrupted apply with the same input. A settled apply contacts no
cluster. Destroy retires the add-on with the cluster, without deleting its
Kubernetes objects through the cluster API:

```sh
bootwright destroy --context addons-nmstate
```
