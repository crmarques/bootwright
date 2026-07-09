package desiredstate

import (
	"fmt"

	"github.com/crmarques/bootwright/api/v1alpha1"
	addoninputs "github.com/crmarques/bootwright/internal/addons/inputs"
)

func validateStorageExports(state v1alpha1.State, clusters map[string]v1alpha1.StorageCluster, pools map[string]v1alpha1.StoragePool, filesystems map[string]v1alpha1.StorageFilesystem, gateways map[string]v1alpha1.StorageObjectGateway, machines map[string]v1alpha1.Machine) []string {
	var errs []string
	for _, export := range state.StorageExports {
		if e := validateName(v1alpha1.KindStorageExport, export.Metadata.Name); e != "" {
			errs = append(errs, e)
			continue
		}
		prefix := fmt.Sprintf("StorageExport/%s spec", export.Metadata.Name)
		cluster, clusterOK := clusters[export.Spec.StorageClusterRef.Name]
		if export.Spec.StorageClusterRef.Name == "" {
			errs = append(errs, prefix+".storageClusterRef is required")
		} else if !clusterOK {
			errs = append(errs, fmt.Sprintf("%s.storageClusterRef %q does not match any StorageCluster", prefix, export.Spec.StorageClusterRef.Name))
		}
		switch export.Spec.Type {
		case v1alpha1.StorageExportTypeDataFoundation:
		case "":
			errs = append(errs, prefix+".type is required")
			continue
		default:
			errs = append(errs, fmt.Sprintf("%s.type %q must be %q", prefix, export.Spec.Type, v1alpha1.StorageExportTypeDataFoundation))
			continue
		}
		df := export.Spec.DataFoundation
		if clusterOK {
			errs = append(errs, validateStorageExportExternalDetails(export, cluster)...)
		}
		if clusterOK && storageClusterExternal(cluster) {
			if df != nil {
				errs = append(errs, fmt.Sprintf("%s.dataFoundation must be empty when storageClusterRef points to StorageCluster/%s with spec.management=external", prefix, cluster.Metadata.Name))
			}
			continue
		}
		if df == nil {
			errs = append(errs, prefix+".dataFoundation is required when storageClusterRef points to managed Ceph")
			continue
		}
		if df.RBDPoolRef.Name == "" {
			errs = append(errs, prefix+".dataFoundation.rbdPoolRef is required")
		} else if pool, ok := pools[df.RBDPoolRef.Name]; !ok {
			errs = append(errs, fmt.Sprintf("%s.dataFoundation.rbdPoolRef %q does not match any StoragePool", prefix, df.RBDPoolRef.Name))
		} else if pool.Spec.StorageClusterRef.Name != export.Spec.StorageClusterRef.Name {
			errs = append(errs, fmt.Sprintf("%s.dataFoundation.rbdPoolRef %q belongs to StorageCluster/%s, want StorageCluster/%s", prefix, pool.Metadata.Name, pool.Spec.StorageClusterRef.Name, export.Spec.StorageClusterRef.Name))
		}
		if df.FilesystemRef.Name == "" {
			errs = append(errs, prefix+".dataFoundation.filesystemRef is required")
		} else if fs, ok := filesystems[df.FilesystemRef.Name]; !ok {
			errs = append(errs, fmt.Sprintf("%s.dataFoundation.filesystemRef %q does not match any StorageFilesystem", prefix, df.FilesystemRef.Name))
		} else if fs.Spec.StorageClusterRef.Name != export.Spec.StorageClusterRef.Name {
			errs = append(errs, fmt.Sprintf("%s.dataFoundation.filesystemRef %q belongs to StorageCluster/%s, want StorageCluster/%s", prefix, fs.Metadata.Name, fs.Spec.StorageClusterRef.Name, export.Spec.StorageClusterRef.Name))
		}
		if df.ObjectGatewayRef.Name != "" {
			if gw, ok := gateways[df.ObjectGatewayRef.Name]; !ok {
				errs = append(errs, fmt.Sprintf("%s.dataFoundation.objectGatewayRef %q does not match any StorageObjectGateway", prefix, df.ObjectGatewayRef.Name))
			} else if gw.Spec.StorageClusterRef.Name != export.Spec.StorageClusterRef.Name {
				errs = append(errs, fmt.Sprintf("%s.dataFoundation.objectGatewayRef %q belongs to StorageCluster/%s, want StorageCluster/%s", prefix, gw.Metadata.Name, gw.Spec.StorageClusterRef.Name, export.Spec.StorageClusterRef.Name))
			}
		}
	}
	return errs
}

// validateStorageExportExternalDetails validates the operator-supplied
// details arm. A managed-Ceph export may omit externalDetails entirely — the
// consuming add-on then produces the details itself (a hook running the Rook
// exporter on a Ceph node). External Ceph has no nodes Bootwright can reach,
// so operator-supplied details are the only source.
func validateStorageExportExternalDetails(export v1alpha1.StorageExport, cluster v1alpha1.StorageCluster) []string {
	prefix := fmt.Sprintf("StorageExport/%s spec.externalDetails", export.Metadata.Name)
	details := export.Spec.ExternalDetails
	if details == nil {
		if storageClusterExternal(cluster) {
			return []string{prefix + " is required when storageClusterRef points to external Ceph"}
		}
		return nil
	}
	if details.FromSecretRef.Name == "" {
		return []string{prefix + ".fromSecretRef is required when externalDetails is set"}
	}
	return nil
}

func validateStorageExportAttachmentEffects(state v1alpha1.State, exports map[string]v1alpha1.StorageExport) []string {
	var errs []string
	for _, effect := range addoninputs.EffectBindings(state, v1alpha1.ClusterAddonInputEffectStorageExportAttachment, v1alpha1.ClusterAddonProvidesDataFoundation) {
		prefix := fmt.Sprintf("ClusterAddonBinding/%s ClusterAddon/%s input[%s]", effect.Binding.Metadata.Name, effect.Addon.AddonRef.Name, effect.Input.Name)
		if !addonProvides(effect.Extension, v1alpha1.ClusterAddonProvidesDataFoundation) {
			errs = append(errs, fmt.Sprintf("%s effect %q with provider %q requires ClusterAddon/%s to provide %q", prefix, effect.Effect.Type, effect.Effect.Provider, effect.Addon.AddonRef.Name, v1alpha1.ClusterAddonProvidesDataFoundation))
		}
		exportRef := addoninputs.LocalObjectReferenceValue(effect.Input.Values, "exportRef")
		if exportRef.Name == "" {
			continue
		}
		export, exportOK := exports[exportRef.Name]
		if !exportOK {
			errs = append(errs, fmt.Sprintf("%s.values.exportRef %q does not match any StorageExport", prefix, exportRef.Name))
			continue
		}
		if export.Spec.Type != v1alpha1.StorageExportTypeDataFoundation {
			errs = append(errs, fmt.Sprintf("%s.values.exportRef %q must reference a %s StorageExport", prefix, exportRef.Name, v1alpha1.StorageExportTypeDataFoundation))
			continue
		}
	}
	return errs
}

func addonProvides(extension v1alpha1.ClusterAddon, capability string) bool {
	for _, item := range extension.Spec.Provides {
		if item == capability {
			return true
		}
	}
	return false
}
