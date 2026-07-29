package desiredstate

import (
	"fmt"
	"net"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func validateStorageIngressVIP(owner, address string, prefixLength int) []string {
	var errs []string
	ip := net.ParseIP(address)
	switch {
	case address == "":
		errs = append(errs, owner+".address is required")
	case ip == nil:
		errs = append(errs, fmt.Sprintf("%s.address %q is not a valid IP", owner, address))
	}
	maxPrefix := 128
	if ip != nil && ip.To4() != nil {
		maxPrefix = 32
	}
	switch {
	case prefixLength == 0:
		errs = append(errs, owner+".prefixLength is required")
	case prefixLength < 1 || prefixLength > maxPrefix:
		errs = append(errs, fmt.Sprintf("%s.prefixLength %d out of range (1-%d)", owner, prefixLength, maxPrefix))
	}
	return errs
}

func validateStorageIngressVRRPID(owner string, firstVirtualRouterID int) []string {
	if firstVirtualRouterID == 0 {
		return nil
	}
	if firstVirtualRouterID < 1 || firstVirtualRouterID > 255 {
		return []string{fmt.Sprintf("%s.firstVirtualRouterID %d out of range (1-255)", owner, firstVirtualRouterID)}
	}
	return nil
}

type storageIngressVRRPDeclaration struct {
	cluster  string
	owner    string
	vrrpID   int
	networks []string
}

func validateStorageIngressVRRPCollisions(state v1alpha1.State) []string {
	var declarations []storageIngressVRRPDeclaration
	for _, gw := range state.StorageObjectGateways {
		for i, ingress := range gw.Spec.Ceph.Ingresses {
			declarations = append(declarations, storageIngressVRRPDeclaration{
				cluster:  gw.Spec.StorageClusterRef.Name,
				owner:    fmt.Sprintf("StorageObjectGateway/%s spec.ceph.ingresses[%d]", gw.Metadata.Name, i),
				vrrpID:   ingress.FirstVirtualRouterID,
				networks: ingress.VirtualInterfaceNetworks,
			})
		}
	}
	for _, nfs := range state.StorageNFSExports {
		for i, ingress := range nfs.Spec.Ceph.Ingresses {
			declarations = append(declarations, storageIngressVRRPDeclaration{
				cluster:  nfs.Spec.StorageClusterRef.Name,
				owner:    fmt.Sprintf("StorageNFSExport/%s spec.ceph.ingresses[%d]", nfs.Metadata.Name, i),
				vrrpID:   ingress.FirstVirtualRouterID,
				networks: ingress.VirtualInterfaceNetworks,
			})
		}
	}
	for _, cluster := range state.StorageClusters {
		if cluster.Spec.Ceph == nil || cluster.Spec.Ceph.MgmtGateway == nil {
			continue
		}
		ingress := cluster.Spec.Ceph.MgmtGateway.Ingress
		declarations = append(declarations, storageIngressVRRPDeclaration{
			cluster:  cluster.Metadata.Name,
			owner:    fmt.Sprintf("StorageCluster/%s spec.ceph.management.ingress", cluster.Metadata.Name),
			vrrpID:   ingress.FirstVirtualRouterID,
			networks: ingress.VirtualInterfaceNetworks,
		})
	}
	var errs []string
	for i := 0; i < len(declarations); i++ {
		a := declarations[i]
		if a.vrrpID == 0 || len(a.networks) == 0 {
			continue
		}
		for j := i + 1; j < len(declarations); j++ {
			b := declarations[j]
			if a.cluster != b.cluster || b.vrrpID != a.vrrpID || len(b.networks) == 0 {
				continue
			}
			if net := storageOverlappingNetwork(a.networks, b.networks); net != "" {
				errs = append(errs, fmt.Sprintf("%s and %s both set firstVirtualRouterID %d on overlapping virtualInterfaceNetworks %s; cephadm keepalived VRRP instances with the same router ID on the same L2 network conflict — give one of them a distinct firstVirtualRouterID", a.owner, b.owner, a.vrrpID, net))
			}
		}
	}
	return errs
}

func validateStorageGatewayIngressTLS(state v1alpha1.State) []string {
	var errs []string
	for _, gw := range state.StorageObjectGateways {
		for i, ingress := range gw.Spec.Ceph.Ingresses {
			if ingress.TLS == nil {
				continue
			}
			owner := fmt.Sprintf("StorageObjectGateway/%s spec.ceph.ingresses[%d].tls", gw.Metadata.Name, i)
			errs = append(errs, validateStorageTLSSecretRef(owner+".certificateRef", ingress.TLS.CertificateRef.Name, state)...)
			errs = append(errs, validateStorageTLSSecretRef(owner+".keyRef", ingress.TLS.KeyRef.Name, state)...)
		}
	}
	return errs
}

func storageOverlappingNetwork(a, b []string) string {
	canonicalA := storageCanonicalNetworks(a)
	for _, network := range b {
		if _, ipNet, err := net.ParseCIDR(network); err == nil && canonicalA[ipNet.String()] {
			return ipNet.String()
		}
	}
	return ""
}
