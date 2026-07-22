package desiredstate

import (
	"fmt"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func validateStorageNFSExports(items []v1alpha1.StorageNFSExport, clusters map[string]v1alpha1.StorageCluster, filesystems map[string]v1alpha1.StorageFilesystem) []string {
	var errs []string
	for _, nfs := range items {
		if e := validateName(v1alpha1.KindStorageNFSExport, nfs.Metadata.Name); e != "" {
			errs = append(errs, e)
			continue
		}
		prefix := fmt.Sprintf("StorageNFSExport/%s spec", nfs.Metadata.Name)
		cluster, ok := clusters[nfs.Spec.StorageClusterRef.Name]
		if nfs.Spec.StorageClusterRef.Name == "" {
			errs = append(errs, prefix+".storageClusterRef is required")
		} else if !ok {
			errs = append(errs, fmt.Sprintf("%s.storageClusterRef %q does not match any StorageCluster", prefix, nfs.Spec.StorageClusterRef.Name))
		} else if storageClusterExternal(cluster) {
			errs = append(errs, fmt.Sprintf("%s.storageClusterRef %q references an external StorageCluster; Bootwright-managed NFS services are not declared for imported Ceph", prefix, nfs.Spec.StorageClusterRef.Name))
		}
		if nfs.Spec.Ceph.ServiceID == "" {
			errs = append(errs, prefix+".ceph.serviceID is required")
		}
		if len(nfs.Spec.Ceph.Placement.Hosts) == 0 && len(nfs.Spec.Ceph.Placement.Sites) == 0 {
			errs = append(errs, prefix+".ceph.placement requires hosts or sites")
		}
		errs = append(errs, validateStoragePlacementHosts(prefix+".ceph.placement", nfs.Spec.Ceph.Placement, cluster, ok, "")...)
		ingressNames := map[string]bool{}
		for i, ingress := range nfs.Spec.Ceph.Ingresses {
			owner := fmt.Sprintf("%s.ceph.ingresses[%d]", prefix, i)
			if ingress.Name == "" {
				errs = append(errs, owner+".name is required")
			} else if ingressNames[ingress.Name] {
				errs = append(errs, fmt.Sprintf("%s.name %q is duplicated", owner, ingress.Name))
			}
			ingressNames[ingress.Name] = true
			if ingress.Address == "" {
				errs = append(errs, owner+".address is required")
			}
			if ingress.PrefixLength == 0 {
				errs = append(errs, owner+".prefixLength is required")
			}
			errs = append(errs, validateStorageIngressVRRPID(owner, ingress.FirstVirtualRouterID)...)
			errs = append(errs, validateStoragePlacementHosts(owner+".placement", ingress.Placement, cluster, ok, v1alpha1.StorageCephRoleIngress)...)
		}
		errs = append(errs, validateStorageNFSExportEntries(prefix, nfs, filesystems)...)
	}
	return errs
}

func validateStorageNFSExportEntries(prefix string, nfs v1alpha1.StorageNFSExport, filesystems map[string]v1alpha1.StorageFilesystem) []string {
	var errs []string
	seen := map[string]bool{}
	for i, export := range nfs.Spec.Exports {
		owner := fmt.Sprintf("%s.exports[%d]", prefix, i)
		if export.Pseudo == "" {
			errs = append(errs, owner+".pseudo is required")
		} else if seen[export.Pseudo] {
			errs = append(errs, fmt.Sprintf("%s.pseudo %q is duplicated", owner, export.Pseudo))
		}
		seen[export.Pseudo] = true
		hasFS := export.FilesystemRef.Name != ""
		hasBucket := export.Bucket != ""
		switch {
		case hasFS && hasBucket:
			errs = append(errs, owner+" must set exactly one of filesystemRef (CephFS) or bucket (RGW), not both")
		case !hasFS && !hasBucket:
			errs = append(errs, owner+" must set filesystemRef (CephFS) or bucket (RGW)")
		case hasFS:
			fs, fsOK := filesystems[export.FilesystemRef.Name]
			if !fsOK {
				errs = append(errs, fmt.Sprintf("%s.filesystemRef %q does not match any StorageFilesystem", owner, export.FilesystemRef.Name))
			} else if fs.Spec.StorageClusterRef.Name != nfs.Spec.StorageClusterRef.Name {
				errs = append(errs, fmt.Sprintf("%s.filesystemRef %q belongs to StorageCluster/%s, want StorageCluster/%s", owner, export.FilesystemRef.Name, fs.Spec.StorageClusterRef.Name, nfs.Spec.StorageClusterRef.Name))
			}
		}
		switch export.AccessType {
		case "", v1alpha1.StorageNFSAccessReadWrite, v1alpha1.StorageNFSAccessReadOnly, v1alpha1.StorageNFSAccessNone:
		default:
			errs = append(errs, fmt.Sprintf("%s.accessType %q must be one of {RW, RO, NONE}", owner, export.AccessType))
		}
	}
	return errs
}
