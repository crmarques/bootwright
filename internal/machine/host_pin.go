package machine

import (
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/trust"
)

func (p Placement) ProveHostPin(contextName string, material map[string]secrets.Material) error {
	if p.Connection != ConnectionSSH {
		return nil
	}
	_, err := BoundHostKey(contextName, p.Machine, p.Address, p.Port, p.KnownHostsRef, material)
	return err
}

func BoundHostKey(contextName, name, address string, port int, reference string, material map[string]secrets.Material) (trust.HostKey, error) {
	value, _ := material[reference].Part(secrets.ValuePart)
	defer clear(value)
	key, err := trust.ParseKnownHostsLine(string(value), address, port)
	if err == nil {
		err = key.Validate()
	}
	if err != nil {
		detail := diagnostics.Of(err)[0].Message
		return trust.HostKey{}, diagnostics.NewFailureWithRemediation("trust.identity",
			"Machine/"+name+" at "+trust.HostToken(address, port)+" requires a valid SSH host key in Secret/"+reference+" ("+detail+")", "",
			"verify the host key and repeat bootwright machine trust --context "+contextName+" --name "+name+" --referenced-secret; use --replace "+name+" for an existing invalid pin")
	}
	return key, nil
}
