package agentinstall

import (
	"bytes"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"strings"

	"go.yaml.in/yaml/v3"
)

type kubeconfigIdentities struct {
	Clusters []struct {
		Cluster struct {
			Server    string `yaml:"server"`
			Authority string `yaml:"certificate-authority-data"`
		} `yaml:"cluster"`
	} `yaml:"clusters"`
	Users []struct {
		User struct {
			Certificate string `yaml:"client-certificate-data"`
		} `yaml:"user"`
	} `yaml:"users"`
}

// kubeconfigIdentity is the cluster's server, the digest of its decoded
// authority and the client certificate's subject, never the credential, so a
// recapture that rotates only the client key keeps it.
func kubeconfigIdentity(value []byte) (string, error) {
	var config kubeconfigIdentities
	unnamed := refusal("lifecycle.state", "a custodied administrator kubeconfig names no single cluster authority and principal", "")
	if err := yaml.Unmarshal(value, &config); err != nil || len(config.Clusters) != 1 || len(config.Users) != 1 || config.Clusters[0].Cluster.Server == "" {
		return "", unnamed
	}
	authority, err := base64.StdEncoding.DecodeString(config.Clusters[0].Cluster.Authority)
	defer clear(authority)
	if err != nil || len(bytes.TrimSpace(authority)) == 0 {
		return "", unnamed
	}
	principal, ok := certificateSubject(config.Users[0].User.Certificate)
	if !ok {
		return "", unnamed
	}
	digest := sha256.Sum256(authority)
	return config.Clusters[0].Cluster.Server + " under authority sha256:" + hex.EncodeToString(digest[:]) + " as " + principal, nil
}

func certificateSubject(data string) (string, bool) {
	encoded, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return "", false
	}
	block, _ := pem.Decode(encoded)
	if block == nil || block.Type != "CERTIFICATE" {
		return "", false
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil || certificate.Subject.String() == "" || strings.ContainsAny(certificate.Subject.String(), "\n\r") {
		return "", false
	}
	return certificate.Subject.String(), true
}
