# Six-host Ceph services profile

Profile [P2](../../../specs/ceph-coverage.md#profile-catalog): six RHEL storage
guests and one independent client guest running an IBM Storage Ceph cluster
(`ceph-lab`) that exercises CephFS, S3, NFS and SMB. CephFS `files` has a
replicated and an EC 2+2 data pool, two subvolume groups and an active MDS
with a standby-replay follower. RGW `objects` (hosts 3 and 6) sits behind a
TLS ingress VIP, with a user and bucket. NFS (two daemons, ingress VIP) exports
a CephFS subvolume at `/docs` and the RGW user at `/objects`. SMB runs in user
mode with clustering over two daemons and two shares on distinct subvolumes.

This is a synthetic profile: every operator run is unrun, a passing offline
plan is not live qualification, and [M2c](../../../specs/milestones/m2.md#m2c)
stays open until its owner accepts the applicable ledger rows.

## Worth noting

- Topology: `rhel-01` to `rhel-06` with 4 vCPU, 60 GiB root and one 60 GiB SSD
  each; RAM 16/16/11/12/12/12 GiB. The `client` has 2 vCPU, 4 GiB, 40 GiB
  root. Total 7 guests, 26 vCPUs, 83 GiB RAM; reserve 4 GiB controller
  memory. OSD memory targets are 4 GiB. Never cut replication or RAM to fit.
- VIPs in the public subnet: RGW ingress `198.51.100.50`, NFS ingress
  `198.51.100.51`; allocate your own VIPs, addresses and DNS in the copy.
- The `client` Machine is a node of no cluster and is installed beside the
  six hosts; remove it to use an operator-owned client. Adapt the NFS
  allowed-client addresses to the client you use.
- The four auxiliary images (`haproxy`, `keepalived`, `samba`, `samba_metrics`)
  are `qualification-placeholder` coordinates under `cp.icr.io/cp/synthetic/`;
  replace them with authenticated images of your release. Clustered SMB and
  the gateways need qualifying against those exact images.
- Required inputs: RHEL 9.8 DVD media, the IBM entitlement key for Secret
  `ibm-registry`, and the SMB user password for `files-smb-user`. The
  Entitlement `license.accept: true` is a placeholder; review IBM's terms.
- Client tools to install: `ceph-common`, `nfs-utils`, `cifs-utils`,
  `samba-client`, `curl` and an S3 client.
