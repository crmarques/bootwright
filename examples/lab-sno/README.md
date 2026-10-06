# Single-node OpenShift on emulated bare metal

This example is the shape the
[B61 delivery](../../specs/milestones/m3.md#b61)
targets: the same managed service set as [`lab-rhel`](../lab-rhel/README.md),
one libvirt Machine whose hardware the substrate realizes, and one
single-node OpenShift cluster the agent installer writes onto it. Bootwright
installs no operating system here: the node is *installer-provisioned*, so its
disk is written by the cluster's own installer rather than by a Kickstart.

| Object | Role |
| --- | --- |
| [`controller`](infra/machines/controller.yaml) | The provided local Machine selected by `Environment.spec.controller`; it declares `container-runtime` and `libvirt`. |
| [`lab-proxy`](infra/components/proxy.yaml) | Managed Squid container; it answers only the addresses the graph declares. |
| [`lab-dns`](infra/components/dns.yaml) | Managed dnsmasq container answering every retained Machine name and the three names the cluster answers at. |
| [`lab-ntp`](infra/components/ntp.yaml) | Managed chrony container serving time without disciplining the host clock. |
| [`lab-artifacts`](infra/components/artifact-server.yaml) | Managed artifact server; the cluster's agent image is published privately beneath its served root. |
| [`lab-libvirt`](infra/providers/lab-libvirt.yaml) | The libvirt provider hosted on the controller: the node profile, the managed guest network and the emulated BMC range starting at `8000`. |
| [`lab-guests`](infra/networkconfigs/lab-guests.yaml) | The guest network: `198.51.100.0/24` with its static interface, search domain, resolver selection and default route. |
| [`sno-01`](infra/machines/sno-01.yaml) | The cluster's one node. It declares `openshift-node`, `os.provided: false` and no install profile, which is what makes it installer-provisioned. |
| [`sno`](clusters/sno/cluster.yaml) | The cluster: an exact OpenShift release, the agent method, and three endpoint slots that resolve to the node's own address. |
| [`openshift-pull-secret`](secret-descriptors/openshift-pull-secret.yaml) | The one context secret you set by hand; everything else is generated. |
| [`sno-cluster-admin-ssh-key`](secret-descriptors/sno-cluster-admin-ssh-key.yaml) | The key authorized for the `core` account on every cluster node. |
| [`artifact-server-tls`](secret-descriptors/artifact-server-tls.yaml) | Generated serving certificate; its subject alternative names must cover every address an HTTPS endpoint serves. |
| [`lab-bmc-credentials`](secret-descriptors/lab-bmc-credentials.yaml) | Generated credentials the emulated BMC answers with. |

## What a single-node cluster does differently

A cluster with one node has no virtual address to answer at: `api`, `api-int`
and `ingress` all resolve to the node itself, which is what
`source.type: node` declares. The agent installer also refuses every platform
for one control-plane node and no compute nodes, so the rendered install
configuration carries `platform: none` whatever the graph derived. Neither is
authored here; both follow from the one node.

## Lab conventions

The conventions are [`lab-rhel`](../lab-rhel/README.md#lab-conventions)'s, with
one addition. `192.0.2.1` is the controller's own existing address and must
already exist on the host; `198.51.100.0/24` is the guest network Bootwright
defines on `virbr-lab` and must not exist yet. Six sockets on `192.0.2.1` must
be free: `3128`, `53`, `123`, `8443`, `8080` and `8000`.

The addition is name resolution for the controller itself. `openshift-install`
polls the cluster API from the controller, so the controller must resolve
`api.sno.lab.example.test`, `api-int.sno.lab.example.test` and names beneath
`apps.sno.lab.example.test`. The managed resolver answers all three, but
nothing points the controller's own resolver at it. Route the zone to the
managed resolver before applying, for example with a systemd-resolved drop-in:

```sh
sudo tee /etc/systemd/resolved.conf.d/bootwright-lab-sno.conf >/dev/null <<'EOF'
[Resolve]
DNS=192.0.2.1
Domains=~lab.example.test
EOF
sudo systemctl restart systemd-resolved
getent ahosts api.sno.lab.example.test
getent ahosts bootwright-sno.apps.sno.lab.example.test
```

Before it boots anything, the installation proves that each cluster name,
including a name under `*.apps`, answers with the address frozen for it and
nothing else, and fails naming the name, its answers and the expected address,
so a missing or wrong route costs a refusal rather than a failed install.

## The pull secret

The cluster's pull secret is the one secret you supply. Download it from the
Red Hat console and set it into the context store; it never enters the example.
A pull secret is a value file, so `secret set` reads it only when it is yours
and private to you, mode `0600` or `0400`; a browser download is usually
`0644`, which it refuses with `chmod 600` as the remedy.

```sh
chmod 600 ~/pull-secret.json
./bin/bootwright secret set --name openshift-pull-secret --value-file ~/pull-secret.json
./bin/bootwright secret generate
./bin/bootwright secret check
```

## Run it

```sh
make build
./bin/bootwright validate -f examples/lab-sno
sudo ./bin/bootwright context init --name lab-sno --input-dir "$PWD/examples/lab-sno"
chmod 600 ~/pull-secret.json
sudo ./bin/bootwright secret set --name openshift-pull-secret --value-file ~/pull-secret.json
sudo ./bin/bootwright secret generate
sudo ./bin/bootwright setup
sudo ./bin/bootwright apply --stage controller
sudo ./bin/bootwright plan
sudo ./bin/bootwright apply --yes
```

The `controller` stage installs `openshift-install` and the OpenShift clients
for the release the cluster declares. The release is the version pin: the agent
image embeds the payload compiled into that executable, so a release bump needs
that stage run again before the image is built, and the build refuses when the
executable's version is not the declared one.

The apply builds the image, boots the node from it, and then waits: a
single-node cluster takes the better part of an hour on a laptop. The waits are
observations, so interrupting the command stops nothing on the node, and
repeating the apply resumes watching where it left off. Follow the progress in
the log directory the operation names before its first effect.

A completed apply settles when repeated. A destroy releases the media the node
booted from and takes back the published image and the installer's work area;
the cluster itself leaves with the node's disks, which the Machine block
removes under `data-loss`:

```sh
sudo ./bin/bootwright apply --yes      # settles: nothing to do
sudo ./bin/bootwright machine stop --name sno-01
sudo ./bin/bootwright destroy --yes --authorize data-loss
```

Once the apply proves the installation complete, the context keeps a copy of
the administrator kubeconfig in its encrypted custody. Export it to a file only
you can read:

```sh
umask 077
sudo ./bin/bootwright cluster kubeconfig --name sno > sno.kubeconfig
```

The command writes exactly the kubeconfig's bytes and nothing else. The
installer's own copy and the initial administrator password stay in its
root-owned work area beneath `/var/lib/bootwright-clusters/`, which the destroy
removes. The destroy withdraws the custodied kubeconfig only once it completes,
so a destroy that stops part way keeps it.
