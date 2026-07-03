package secret

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

const (
	rsaSSHKeyBits = 4096

	sshPublicKeyTypeEd25519   = "ssh-ed25519"
	sshPublicKeyTypeRSA       = "ssh-rsa"
	sshPublicKeyTypeECDSAP256 = "ecdsa-sha2-nistp256"
	sshPublicKeyTypeECDSAP384 = "ecdsa-sha2-nistp384"
	sshPublicKeyTypeECDSAP521 = "ecdsa-sha2-nistp521"
)

func SSHKeyPairPEM(source v1alpha1.GeneratedSSHKeyPairSpec) (privateKeyPEM, publicKey []byte, err error) {
	keyType := source.Type
	if keyType == "" {
		keyType = v1alpha1.SSHKeyPairTypeEd25519
	}
	switch keyType {
	case v1alpha1.SSHKeyPairTypeEd25519:
		return ed25519SSHKeyPairPEM(source)
	case v1alpha1.SSHKeyPairTypeRSA:
		return rsaSSHKeyPairPEM(source)
	case v1alpha1.SSHKeyPairTypeECDSAP256:
		return ecdsaSSHKeyPairPEM(source, elliptic.P256(), sshPublicKeyTypeECDSAP256, "nistp256")
	case v1alpha1.SSHKeyPairTypeECDSAP384:
		return ecdsaSSHKeyPairPEM(source, elliptic.P384(), sshPublicKeyTypeECDSAP384, "nistp384")
	case v1alpha1.SSHKeyPairTypeECDSAP521:
		return ecdsaSSHKeyPairPEM(source, elliptic.P521(), sshPublicKeyTypeECDSAP521, "nistp521")
	default:
		return nil, nil, fmt.Errorf("unsupported SSH key pair type %q", keyType)
	}
}

func ed25519SSHKeyPairPEM(source v1alpha1.GeneratedSSHKeyPairSpec) (privateKeyPEM, publicKey []byte, err error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate SSH key pair: %w", err)
	}
	publicBlob := sshMarshalStrings([]byte(sshPublicKeyTypeEd25519), pub)
	publicLine := sshPublicKeyLine(sshPublicKeyTypeEd25519, publicBlob, source.Comment)
	privatePEM, err := opensshPrivateKeyPEM(pub, priv, source.Comment, publicBlob)
	if err != nil {
		return nil, nil, err
	}
	return privatePEM, publicLine, nil
}

func rsaSSHKeyPairPEM(source v1alpha1.GeneratedSSHKeyPairSpec) (privateKeyPEM, publicKey []byte, err error) {
	key, err := rsa.GenerateKey(rand.Reader, rsaSSHKeyBits)
	if err != nil {
		return nil, nil, fmt.Errorf("generate SSH key pair: %w", err)
	}
	if err := key.Validate(); err != nil {
		return nil, nil, fmt.Errorf("validate generated SSH key pair: %w", err)
	}
	publicBlob := sshMarshalString(nil, []byte(sshPublicKeyTypeRSA))
	publicBlob = sshMarshalMPInt(publicBlob, big.NewInt(int64(key.PublicKey.E)))
	publicBlob = sshMarshalMPInt(publicBlob, key.PublicKey.N)
	privateDER := x509.MarshalPKCS1PrivateKey(key)
	var out bytes.Buffer
	if err := pem.Encode(&out, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: privateDER}); err != nil {
		return nil, nil, fmt.Errorf("encode SSH private key: %w", err)
	}
	return out.Bytes(), sshPublicKeyLine(sshPublicKeyTypeRSA, publicBlob, source.Comment), nil
}

func ecdsaSSHKeyPairPEM(source v1alpha1.GeneratedSSHKeyPairSpec, curve elliptic.Curve, publicType, curveName string) (privateKeyPEM, publicKey []byte, err error) {
	key, err := ecdsa.GenerateKey(curve, rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate SSH key pair: %w", err)
	}
	ecdhPub, err := key.PublicKey.ECDH()
	if err != nil {
		return nil, nil, fmt.Errorf("derive SSH public key point: %w", err)
	}
	// ecdh.PublicKey.Bytes returns the SEC1 uncompressed encoding
	// (0x04 || X || Y) — identical to the deprecated elliptic.Marshal.
	point := ecdhPub.Bytes()
	publicBlob := sshMarshalStrings([]byte(publicType), []byte(curveName), point)
	privateDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, fmt.Errorf("encode SSH private key: %w", err)
	}
	var out bytes.Buffer
	if err := pem.Encode(&out, &pem.Block{Type: "EC PRIVATE KEY", Bytes: privateDER}); err != nil {
		return nil, nil, fmt.Errorf("encode SSH private key: %w", err)
	}
	return out.Bytes(), sshPublicKeyLine(publicType, publicBlob, source.Comment), nil
}

func VerifySSHKeyPairPublicBytesMatchRequest(data []byte, source v1alpha1.GeneratedSSHKeyPairSpec) error {
	fields := strings.Fields(strings.TrimSpace(string(data)))
	if len(fields) < 2 {
		return errors.New("SSH public key must use authorized_keys format")
	}
	wantType, err := sshPublicKeyType(source.Type)
	if err != nil {
		return err
	}
	if fields[0] != wantType {
		return fmt.Errorf("SSH key type drift: got %q, want %q", fields[0], wantType)
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

func sshPublicKeyLine(publicType string, publicBlob []byte, comment string) []byte {
	publicLine := publicType + " " + base64.StdEncoding.EncodeToString(publicBlob)
	if comment != "" {
		publicLine += " " + comment
	}
	return []byte(publicLine + "\n")
}

func sshPublicKeyType(keyType string) (string, error) {
	if keyType == "" {
		keyType = v1alpha1.SSHKeyPairTypeEd25519
	}
	switch keyType {
	case v1alpha1.SSHKeyPairTypeEd25519:
		return sshPublicKeyTypeEd25519, nil
	case v1alpha1.SSHKeyPairTypeRSA:
		return sshPublicKeyTypeRSA, nil
	case v1alpha1.SSHKeyPairTypeECDSAP256:
		return sshPublicKeyTypeECDSAP256, nil
	case v1alpha1.SSHKeyPairTypeECDSAP384:
		return sshPublicKeyTypeECDSAP384, nil
	case v1alpha1.SSHKeyPairTypeECDSAP521:
		return sshPublicKeyTypeECDSAP521, nil
	default:
		return "", fmt.Errorf("unsupported SSH key pair type %q", keyType)
	}
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

func sshMarshalMPInt(out []byte, value *big.Int) []byte {
	if value.Sign() == 0 {
		return sshMarshalString(out, nil)
	}
	bytes := value.Bytes()
	if bytes[0]&0x80 != 0 {
		bytes = append([]byte{0}, bytes...)
	}
	return sshMarshalString(out, bytes)
}

func sshMarshalUint32(out []byte, value uint32) []byte {
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], value)
	return append(out, buf[:]...)
}
