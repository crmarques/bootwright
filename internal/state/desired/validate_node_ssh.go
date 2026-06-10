package desiredstate

import "github.com/crmarques/bootwright/api/v1alpha1"

func validateNodeSSHSpec(owner string, spec v1alpha1.NodeSSHSpec, required bool) []string {
	if spec.IsZero() {
		if required {
			return []string{owner + " requires keyPairRef or publicKeyRef"}
		}
		return nil
	}
	var errs []string
	if spec.KeyPairRef.Name != "" && (spec.PublicKeyRef.Name != "" || spec.PrivateKeyRef.Name != "") {
		errs = append(errs, owner+" must use either keyPairRef or publicKeyRef/privateKeyRef, not both")
	}
	if spec.KeyPairRef.Name == "" && spec.PublicKeyRef.Name == "" {
		errs = append(errs, owner+" publicKeyRef is required when keyPairRef is empty")
	}
	return errs
}
