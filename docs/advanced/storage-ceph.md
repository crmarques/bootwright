---
title: Ceph Storage Clusters
description: Accessing a managed Ceph storage cluster and recovering its dashboard credentials.
---

# Ceph storage clusters

A `StorageCluster` of `type: ceph` with `management: managed` is bootstrapped by
Bootwright with `cephadm`. Ceph keeps no kubeconfig-style admin file on the
controller — the admin keyring and `ceph.conf` live on the seed node — so
day-to-day access is by SSH to the seed node plus `cephadm shell`.

## Access details

`bootwright cluster access-info` prints everything needed to reach a managed Ceph
cluster, derived entirely from desired state:

```text
$ bootwright cluster access-info --cluster ceph-libvirt

Storage cluster ceph-libvirt
  Type: ceph (managed)
  Seed node: ceph-0
  SSH: ssh root@192.168.134.20
  Monitors: ceph-0=192.168.134.20:6789, ceph-1=192.168.134.21:6789, ceph-2=192.168.134.22:6789
  Health check: ssh root@192.168.134.20 sudo cephadm shell -- ceph -s
  Cluster shell: ssh root@192.168.134.20 sudo cephadm shell
  Dashboard: https://192.168.134.20:8443
  Dashboard user: admin
  Dashboard password file: /var/lib/bootwright/contexts/<ctx>/clusters/ceph-libvirt/secrets/dashboard-password
  Show dashboard password: sudo cat /var/lib/bootwright/contexts/<ctx>/clusters/ceph-libvirt/secrets/dashboard-password
  [OK] dashboard password: OK /var/lib/bootwright/contexts/<ctx>/clusters/ceph-libvirt/secrets/dashboard-password
  [INFO] health: run the health check to confirm Ceph reports HEALTH_OK
```

Run the **Health check** line; `HEALTH_OK` from `ceph -s` confirms the cluster is
reachable and healthy.

## Dashboard credentials

`cephadm bootstrap` enables the Ceph dashboard and generates a one-time random
`admin` password, printed once during bootstrap. Bootwright captures that
password **during the install** and saves it on the controller exactly like the
kubeadmin password for OpenShift clusters:

```text
clusters/<storage-cluster>/secrets/dashboard-password   # mode 0600
```

View it without printing it anywhere else, then log in at the **Dashboard** URL as
`admin`:

```bash
sudo cat /var/lib/bootwright/contexts/<ctx>/clusters/ceph-libvirt/secrets/dashboard-password
```

!!! note "Captured at install only"
    The password is captured solely from the one-time `cephadm bootstrap`. It is
    never re-read or re-synced on later applies, and — like every secret —
    `cluster access-info` only ever shows its file path and a `sudo cat` command,
    never the bytes. As with the kubeadmin password, the file persists after the
    cluster is destroyed; delete the cluster's `secrets/` directory by hand if you
    want the credential gone.

## Recovering the dashboard password

If the `dashboard-password` file is lost, or the in-cluster password was changed
and no longer matches the stored copy, reset it directly on the cluster. The
`ceph` CLI is on the seed node's PATH after bootstrap, so no `cephadm shell` is
needed:

```bash
# SSH to the seed node (the SSH line from cluster access-info)
ssh root@192.168.134.20

# Set a new admin password. Modern Ceph requires the password to be supplied
# from a file via -i (a positional password argument is rejected), and enforces
# a policy: at least 8 characters and not a common word.
umask 077
printf 'NewStr0ngPassw0rd' > /tmp/dash-pass
sudo ceph dashboard ac-user-set-password admin -i /tmp/dash-pass
rm -f /tmp/dash-pass

# confirm the dashboard URL the active mgr is serving
sudo ceph mgr services
```

To keep `bootwright cluster access-info` accurate, write the same value back to
the stored file on the controller:

```bash
P=/var/lib/bootwright/contexts/<ctx>/clusters/ceph-libvirt/secrets/dashboard-password
printf 'NewStr0ngPassw0rd' | sudo tee "$P" >/dev/null
sudo chmod 0600 "$P"
```

A clean reinstall (`bootwright apply ... --override`, which clears `/etc/ceph` and
re-bootstraps) re-captures a fresh dashboard password into the stored file
automatically.
