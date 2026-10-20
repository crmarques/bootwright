package material

import (
	"bytes"
	"strings"

	"github.com/crmarques/bootwright/internal/secrets"
	"golang.org/x/crypto/ssh"
)

func AuthorizedKey(material secrets.Material, reference string) (string, error) {
	value, ok := material.Part(secrets.PublicKeyPart)
	if !ok {
		value, ok = material.Part(secrets.ValuePart)
	}
	defer clear(value)
	line := bytes.TrimRight(value, "\n")
	_, _, options, rest, err := ssh.ParseAuthorizedKey(line)
	if !ok || len(line) == 0 || bytes.ContainsAny(line, "\r\n") || !secrets.SSHComment(string(line)) || err != nil || len(options) != 0 || len(bytes.TrimSpace(rest)) != 0 {
		return "", secrets.Refusal("part", "the selected public key must contain exactly one authorized-key line without options", "", reference, "set Secret/"+reference+" to an sshKeyPair public key or an opaque authorized-key line")
	}
	return strings.TrimSpace(string(line)), nil
}
