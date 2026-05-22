# Libvirt VIP not reachable: bootstrap API never initializes

**Symptom:** The OpenShift install loops on `Bootstrap Kube API never initialized` even though the SNO node is booting and bootkube appears healthy. API or ingress VIP traffic never reaches HAProxy.

**Root cause:** `ip_nonlocal_bind` lets HAProxy listen on a VIP, but neither cluster nodes nor the provider host can reach the listener unless some interface answers ARP for that address. The libvirt bridge must own the managed VIP addresses.

**Fix:** The `cluster_network_vips` role pairs load-balancer endpoint binds with the cluster's libvirt machine networks and assigns the VIPs to the matching bridge before DNS or external validation runs.

