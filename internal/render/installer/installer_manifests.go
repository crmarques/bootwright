package installer

import (
	"encoding/base64"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

type InstallerManifest struct {
	FileName string
	Object   map[string]any
}

func InstallerManifests(ocp v1alpha1.ContainerCluster, secrets InstallerSecrets) []InstallerManifest {
	manifests := DiskEncryptionManifests(ocp)
	serving := ocp.Spec.Install.ServingCertificates
	if serving == nil {
		return manifests
	}
	if api := serving.APIServer; api != nil && len(api.NamedCertificates) > 0 {
		for _, ref := range uniqueAPICertificateRefs(api.NamedCertificates) {
			if pair, ok := secrets.TLSPairs[ref.Name]; ok {
				manifests = append(manifests, InstallerManifest{
					FileName: "50-bootwright-api-serving-cert-" + ref.Name + ".yaml",
					Object:   tlsSecretManifest("openshift-config", ref.Name, pair),
				})
			}
		}
		manifests = append(manifests, InstallerManifest{
			FileName: "51-bootwright-api-server.yaml",
			Object:   apiServerManifest(api.NamedCertificates),
		})
	}
	if ingress := serving.Ingress; ingress != nil && ingress.DefaultCertificateRef.Name != "" {
		ref := ingress.DefaultCertificateRef
		if pair, ok := secrets.TLSPairs[ref.Name]; ok {
			manifests = append(manifests, InstallerManifest{
				FileName: "60-bootwright-ingress-default-cert-" + ref.Name + ".yaml",
				Object:   tlsSecretManifest("openshift-ingress", ref.Name, pair),
			})
		}
		manifests = append(manifests, InstallerManifest{
			FileName: "61-bootwright-ingress-controller-default.yaml",
			Object:   ingressControllerManifest(ref.Name),
		})
	}
	sort.SliceStable(manifests, func(i, j int) bool {
		return manifests[i].FileName < manifests[j].FileName
	})
	return manifests
}

func uniqueAPICertificateRefs(certs []v1alpha1.APIServerNamedCertificateSpec) []v1alpha1.SecretRef {
	seen := map[string]bool{}
	var refs []v1alpha1.SecretRef
	for _, cert := range certs {
		name := cert.SecretRef.Name
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		refs = append(refs, cert.SecretRef)
	}
	return refs
}

func tlsSecretManifest(namespace, name string, pair InstallerTLSSecret) map[string]any {
	if isPlaceholderTLSSecret(pair) {
		return map[string]any{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]any{
				"name":      name,
				"namespace": namespace,
			},
			"type": "kubernetes.io/tls",
			"stringData": map[string]any{
				"tls.crt": pair.Cert,
				"tls.key": pair.Key,
			},
		}
	}
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata": map[string]any{
			"name":      name,
			"namespace": namespace,
		},
		"type": "kubernetes.io/tls",
		"data": map[string]any{
			"tls.crt": base64.StdEncoding.EncodeToString([]byte(pair.Cert)),
			"tls.key": base64.StdEncoding.EncodeToString([]byte(pair.Key)),
		},
	}
}

func isPlaceholderTLSSecret(pair InstallerTLSSecret) bool {
	return pair.Cert != "" && pair.Key != "" &&
		containsSecretRef(pair.Cert) && containsSecretRef(pair.Key)
}

func containsSecretRef(value string) bool {
	return strings.HasPrefix(value, "<bootwright-") || strings.HasPrefix(value, "{{ secret ")
}

func apiServerManifest(certs []v1alpha1.APIServerNamedCertificateSpec) map[string]any {
	named := make([]any, 0, len(certs))
	for _, cert := range certs {
		names := make([]any, 0, len(cert.Names))
		for _, name := range cert.Names {
			names = append(names, name)
		}
		named = append(named, map[string]any{
			"names": names,
			"servingCertificate": map[string]any{
				"name": cert.SecretRef.Name,
			},
		})
	}
	return map[string]any{
		"apiVersion": "config.openshift.io/v1",
		"kind":       "APIServer",
		"metadata": map[string]any{
			"name": "cluster",
		},
		"spec": map[string]any{
			"servingCerts": map[string]any{
				"namedCertificates": named,
			},
		},
	}
}

func ingressControllerManifest(secretName string) map[string]any {
	return map[string]any{
		"apiVersion": "operator.openshift.io/v1",
		"kind":       "IngressController",
		"metadata": map[string]any{
			"name":      "default",
			"namespace": "openshift-ingress-operator",
		},
		"spec": map[string]any{
			"defaultCertificate": map[string]any{
				"name": secretName,
			},
		},
	}
}
