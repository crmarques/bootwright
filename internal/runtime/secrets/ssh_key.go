package secret

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func SSHKeyPairPEM(source v1alpha1.GeneratedSSHKeyPairSpec) (privateKeyPEM, publicKey []byte, err error) {
	keyType := source.Type
	if keyType == "" {
		keyType = v1alpha1.SSHKeyPairTypeEd25519
	}
	if keyType != v1alpha1.SSHKeyPairTypeEd25519 {
		return nil, nil, fmt.Errorf("unsupported SSH key pair type %q", keyType)
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate SSH key pair: %w", err)
	}
	publicBlob := sshMarshalStrings([]byte("ssh-ed25519"), pub)
	publicLine := "ssh-ed25519 " + base64.StdEncoding.EncodeToString(publicBlob)
	if source.Comment != "" {
		publicLine += " " + source.Comment
	}
	publicLine += "\n"
	privatePEM, err := opensshPrivateKeyPEM(pub, priv, source.Comment, publicBlob)
	if err != nil {
		return nil, nil, err
	}
	return privatePEM, []byte(publicLine), nil
}

func VerifySSHKeyPairPublicMatchesRequest(publicPath string, source v1alpha1.GeneratedSSHKeyPairSpec) error {
	data, err := os.ReadFile(publicPath)
	if err != nil {
		return fmt.Errorf("read SSH public key: %w", err)
	}
	fields := strings.Fields(strings.TrimSpace(string(data)))
	if len(fields) < 2 {
		return errors.New("SSH public key must use authorized_keys format")
	}
	if fields[0] != "ssh-ed25519" {
		return fmt.Errorf("SSH key type drift: got %q, want %q", fields[0], "ssh-ed25519")
	}
	if source.Comment != "" {
		if len(fields) < 3 {
			return fmt.Errorf("SSH public key comment drift: got empty, want %q", source.Comment)
		}
		comment := strings.Join(fields[2:], " ")
		if comment != source.Comment {
			return fmt.Errorf("SSH public key comment drift: got %q, want %q", comment, source.Comment)
		}
	}
	return nil
}

func opensshPrivateKeyPEM(pub ed25519.PublicKey, priv ed25519.PrivateKey, comment string, publicBlob []byte) ([]byte, error) {
	var check [4]byte
	if _, err := rand.Read(check[:]); err != nil {
		return nil, fmt.Errorf("generate SSH private key check bytes: %w", err)
	}
	checkValue := binary.BigEndian.Uint32(check[:])
	privateBlock := sshMarshalUint32(nil, checkValue)
	privateBlock = sshMarshalUint32(privateBlock, checkValue)
	privateBlock = sshMarshalString(privateBlock, []byte("ssh-ed25519"))
	privateBlock = sshMarshalString(privateBlock, pub)
	privateBlock = sshMarshalString(privateBlock, priv)
	privateBlock = sshMarshalString(privateBlock, []byte(comment))
	for i := 1; len(privateBlock)%8 != 0; i++ {
		privateBlock = append(privateBlock, byte(i))
	}
	payload := append([]byte("openssh-key-v1\x00"), sshMarshalString(nil, []byte("none"))...)
	payload = sshMarshalString(payload, []byte("none"))
	payload = sshMarshalString(payload, nil)
	payload = sshMarshalUint32(payload, 1)
	payload = sshMarshalString(payload, publicBlob)
	payload = sshMarshalString(payload, privateBlock)

	var out bytes.Buffer
	if err := pem.Encode(&out, &pem.Block{Type: "OPENSSH PRIVATE KEY", Bytes: payload}); err != nil {
		return nil, fmt.Errorf("encode SSH private key: %w", err)
	}
	return out.Bytes(), nil
}

func sshMarshalStrings(values ...[]byte) []byte {
	var out []byte
	for _, value := range values {
		out = sshMarshalString(out, value)
	}
	return out
}

func sshMarshalString(out, value []byte) []byte {
	out = sshMarshalUint32(out, uint32(len(value)))
	return append(out, value...)
}

func sshMarshalUint32(out []byte, value uint32) []byte {
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], value)
	return append(out, buf[:]...)
}
