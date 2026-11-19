package ceph

import (
	"slices"

	"github.com/crmarques/bootwright/internal/secrets"
)

func singleValue(read secrets.Material) ([]byte, bool) {
	value, ok := read.Part(secrets.ValuePart)
	if !ok || !slices.Equal(read.Parts(), []secrets.Part{secrets.ValuePart}) {
		clear(value)
		return nil, false
	}
	return value, true
}

func ObjectUserCredentialsOf(read secrets.Material, user string) (secrets.Material, bool) {
	value, ok := singleValue(read)
	if !ok {
		return secrets.Material{}, false
	}
	defer clear(value)
	return ObjectUserCredentials(value, user)
}

func ClientKeyringOf(read secrets.Material, client string) (secrets.Material, bool) {
	value, ok := singleValue(read)
	if !ok {
		return secrets.Material{}, false
	}
	defer clear(value)
	keyring, ok := ClientKeyring(value, client)
	defer clear(keyring)
	if !ok {
		return secrets.Material{}, false
	}
	return secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: keyring}), true
}

func MonitoringCredentialsOf(read secrets.Material, service string) (secrets.Material, bool) {
	value, ok := singleValue(read)
	if !ok {
		return secrets.Material{}, false
	}
	defer clear(value)
	return MonitoringCredentials(value, service)
}
