package material

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	cryptorand "crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/secrets"
	"golang.org/x/crypto/ssh"
)

var generationTime = time.Date(2032, time.March, 4, 5, 6, 7, 987654321, time.FixedZone("test", -3*60*60))

func TestGenerateTokenAndPasswordUseRequestedRawURLSafeEntropy(t *testing.T) {
	tokenService := New(nil, Options{Random: bytes.NewReader(bytes.Repeat([]byte{0xab}, 16)), Clock: func() time.Time { return generationTime }})
	token, err := tokenService.Generate(context.Background(), secrets.Declaration{
		Type: "token", Source: "generated", Generation: secrets.Generation{Bytes: 16},
	})
	if err != nil {
		t.Fatalf("Generate token: %v", err)
	}
	defer token.Clear()
	tokenBytes := requiredPart(t, token, secrets.ValuePart)
	decoded, err := base64.RawURLEncoding.DecodeString(string(tokenBytes))
	if err != nil || !bytes.Equal(decoded, bytes.Repeat([]byte{0xab}, 16)) || bytes.ContainsRune(tokenBytes, '=') {
		t.Fatalf("token is not unpadded base64url over the requested entropy: %v", err)
	}
	clear(tokenBytes)
	clear(decoded)

	passwordService := New(nil, Options{Random: bytes.NewReader(bytes.Repeat([]byte{0x5c}, 32)), Clock: func() time.Time { return generationTime }})
	password, err := passwordService.Generate(context.Background(), secrets.Declaration{
		Type: "usernamePassword", Source: "generated",
	})
	if err != nil {
		t.Fatalf("Generate password: %v", err)
	}
	defer password.Clear()
	if got := string(requiredPart(t, password, secrets.UsernamePart)); got != "admin" {
		t.Fatalf("default username = %q", got)
	}
	passwordBytes := requiredPart(t, password, secrets.PasswordPart)
	decoded, err = base64.RawURLEncoding.DecodeString(string(passwordBytes))
	if err != nil || len(decoded) != 32 || !bytes.Equal(decoded, bytes.Repeat([]byte{0x5c}, 32)) {
		t.Fatalf("password is not base64url over 32 entropy bytes: %v", err)
	}
	clear(passwordBytes)
	clear(decoded)
}

func TestGeneratedUsernameLimitPrecedesPasswordGeneration(t *testing.T) {
	over := secrets.Declaration{
		Type: "usernamePassword", Source: "generated",
		Generation: secrets.Generation{Username: strings.Repeat("u", secrets.MaxPartBytes+1)},
	}
	value, err := New(nil, Options{Random: panicReader{}}).Generate(context.Background(), over)
	value.Clear()
	assertFailureCode(t, err, "secret.store.limit")

	exact := over
	exact.Generation.Username = strings.Repeat("u", secrets.MaxPartBytes)
	value, err = New(nil, Options{Random: bytes.NewReader(bytes.Repeat([]byte{0x5c}, 32))}).Generate(context.Background(), exact)
	if err != nil {
		t.Fatalf("exact-limit generated username: %v", err)
	}
	defer value.Clear()
	username := requiredPart(t, value, secrets.UsernamePart)
	if len(username) != secrets.MaxPartBytes {
		t.Fatalf("generated username size = %d", len(username))
	}
	clear(username)
}

func TestGenerateCertificatesAreP256PKCS8SelfSignedAndTypeScoped(t *testing.T) {
	for _, secretType := range []string{"caBundle", "tlsCertificate"} {
		t.Run(secretType, func(t *testing.T) {
			service := New(nil, Options{Random: cryptorand.Reader, Clock: func() time.Time { return generationTime }})
			declaration := secrets.Declaration{
				Type: secretType, Source: "generated",
				Generation: secrets.Generation{
					CommonName: "service.example", DNSNames: []string{"service.example"},
					IPAddresses: []string{"192.0.2.10", "2001:db8::10"}, ValidityDays: 30,
				},
			}
			value, err := service.Generate(context.Background(), declaration)
			if err != nil {
				t.Fatalf("Generate: %v (%+v)", err, diagnosticMessage(err))
			}
			defer value.Clear()
			certificateBytes := requiredPart(t, value, secrets.CertificatePart)
			certificates, err := parseCertificates(certificateBytes)
			clear(certificateBytes)
			if err != nil || len(certificates) != 1 {
				t.Fatalf("generated certificate: %v", err)
			}
			certificate := certificates[0]
			if certificate.Subject.CommonName != "service.example" || !certificate.NotBefore.Equal(generationTime.UTC().Truncate(time.Second)) ||
				!certificate.NotAfter.Equal(generationTime.UTC().Truncate(time.Second).AddDate(0, 0, 30)) || certificate.SerialNumber.Sign() <= 0 || certificate.SerialNumber.BitLen() > 128 {
				t.Fatalf("certificate identity/time/serial = %+v %v %v %v", certificate.Subject, certificate.NotBefore, certificate.NotAfter, certificate.SerialNumber)
			}
			if len(certificate.DNSNames) != 1 || certificate.DNSNames[0] != "service.example" || len(certificate.IPAddresses) != 2 ||
				!certificate.IPAddresses[0].Equal(net.ParseIP("192.0.2.10")) || !certificate.IPAddresses[1].Equal(net.ParseIP("2001:db8::10")) {
				t.Fatalf("certificate SANs = %v %v", certificate.DNSNames, certificate.IPAddresses)
			}
			if err := certificate.CheckSignature(certificate.SignatureAlgorithm, certificate.RawTBSCertificate, certificate.Signature); err != nil {
				t.Fatalf("self signature: %v", err)
			}
			privateBytes := requiredPart(t, value, secrets.PrivateKeyPart)
			block, err := exactPrivatePEM(privateBytes)
			clear(privateBytes)
			if err != nil || block.Type != "PRIVATE KEY" {
				blockType := ""
				if block != nil {
					blockType = block.Type
				}
				t.Fatalf("private key block type = %q, error %v", blockType, err)
			}
			key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
			if err != nil {
				t.Fatalf("PKCS#8: %v", err)
			}
			if !matchingPublicKey(certificate.PublicKey, publicKey(key)) {
				t.Fatal("certificate and generated private key do not agree")
			}
			if secretType == "caBundle" {
				if !certificate.IsCA || certificate.KeyUsage&x509.KeyUsageCertSign == 0 {
					t.Fatal("generated CA lacks signing constraints")
				}
			} else if certificate.IsCA || !serverAuth(certificate.ExtKeyUsage) || certificate.KeyUsage&x509.KeyUsageDigitalSignature == 0 {
				t.Fatal("generated TLS certificate lacks server constraints")
			}
		})
	}
}

func TestValidateGeneratedCertificateAcceptsStillValidHistoricMaterial(t *testing.T) {
	declaration := secrets.Declaration{
		Type: "tlsCertificate", Source: "generated",
		Generation: secrets.Generation{
			CommonName: "service.example", DNSNames: []string{"service.example", "alias.example"},
			IPAddresses: []string{"192.0.2.10", "2001:db8::10"}, ValidityDays: 30,
		},
	}
	value, err := New(nil, Options{Random: cryptorand.Reader, Clock: func() time.Time { return generationTime }}).Generate(context.Background(), declaration)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	defer value.Clear()

	historicValidator := New(nil, Options{Clock: func() time.Time { return generationTime.AddDate(0, 0, 15) }})
	if err := historicValidator.Validate(context.Background(), declaration, value); err != nil {
		t.Fatalf("historical generated certificate: %v", err)
	}
	expiredValidator := New(nil, Options{Clock: func() time.Time { return generationTime.AddDate(0, 0, 31) }})
	assertFailureCode(t, expiredValidator.Validate(context.Background(), declaration, value), "secret.input")

	wrongDeclarations := []secrets.Declaration{
		{
			Type: "tlsCertificate", Source: "generated",
			Generation: secrets.Generation{CommonName: "other.example", DNSNames: slices.Clone(declaration.Generation.DNSNames), IPAddresses: slices.Clone(declaration.Generation.IPAddresses), ValidityDays: 30},
		},
		{
			Type: "tlsCertificate", Source: "generated",
			Generation: secrets.Generation{CommonName: "service.example", DNSNames: []string{"alias.example", "service.example"}, IPAddresses: slices.Clone(declaration.Generation.IPAddresses), ValidityDays: 30},
		},
		{
			Type: "tlsCertificate", Source: "generated",
			Generation: secrets.Generation{CommonName: "service.example", DNSNames: slices.Clone(declaration.Generation.DNSNames), IPAddresses: []string{"192.0.2.10"}, ValidityDays: 30},
		},
		{
			Type: "tlsCertificate", Source: "generated",
			Generation: secrets.Generation{CommonName: "service.example", DNSNames: slices.Clone(declaration.Generation.DNSNames), IPAddresses: slices.Clone(declaration.Generation.IPAddresses), ValidityDays: 31},
		},
		{
			Type: "caBundle", Source: "generated",
			Generation: declaration.Generation,
		},
	}
	for index, wrong := range wrongDeclarations {
		if err := historicValidator.Validate(context.Background(), wrong, value); err == nil {
			t.Fatalf("declaration drift case %d was accepted", index)
		} else {
			assertFailureCode(t, err, "secret.input")
		}
	}
}

func TestValidateGeneratedCertificateRejectsSignatureCurveAndPrivateKeyFormatDrift(t *testing.T) {
	declaration := secrets.Declaration{
		Type: "tlsCertificate", Source: "generated",
		Generation: secrets.Generation{CommonName: "service.example", DNSNames: []string{"service.example"}, ValidityDays: 30},
	}
	service := New(nil, Options{Random: cryptorand.Reader, Clock: func() time.Time { return generationTime }})
	value, err := service.Generate(context.Background(), declaration)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	defer value.Clear()
	certificate := requiredPart(t, value, secrets.CertificatePart)
	privateKey := requiredPart(t, value, secrets.PrivateKeyPart)
	defer clear(certificate)
	defer clear(privateKey)

	certificateBlock, rest := pem.Decode(certificate)
	if certificateBlock == nil || len(rest) != 0 || len(certificateBlock.Bytes) == 0 {
		t.Fatal("generated certificate did not decode")
	}
	corruptDER := slices.Clone(certificateBlock.Bytes)
	corruptDER[len(corruptDER)-1] ^= 1
	corruptCertificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: corruptDER})
	clear(corruptDER)
	corruptMaterial := secrets.NewMaterial(map[secrets.Part][]byte{
		secrets.CertificatePart: corruptCertificate, secrets.PrivateKeyPart: privateKey,
	})
	clear(corruptCertificate)
	err = service.Validate(context.Background(), declaration, corruptMaterial)
	corruptMaterial.Clear()
	assertFailureCode(t, err, "secret.input")

	privateBlock, err := exactPrivatePEM(privateKey)
	if err != nil {
		t.Fatalf("generated private key: %v", err)
	}
	parsed, err := x509.ParsePKCS8PrivateKey(privateBlock.Bytes)
	clear(privateBlock.Bytes)
	if err != nil {
		t.Fatalf("parse generated private key: %v", err)
	}
	defer clearPrivateKey(parsed)
	ecdsaKey, ok := parsed.(*ecdsa.PrivateKey)
	if !ok {
		t.Fatal("generated certificate key is not ECDSA")
	}
	sec1DER, err := x509.MarshalECPrivateKey(ecdsaKey)
	if err != nil {
		t.Fatalf("marshal SEC1 private key: %v", err)
	}
	sec1PEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: sec1DER})
	clear(sec1DER)
	sec1Material := secrets.NewMaterial(map[secrets.Part][]byte{
		secrets.CertificatePart: certificate, secrets.PrivateKeyPart: sec1PEM,
	})
	clear(sec1PEM)
	err = service.Validate(context.Background(), declaration, sec1Material)
	sec1Material.Clear()
	assertFailureCode(t, err, "secret.input")

	p384Material := generatedLikeTLSMaterial(t, declaration, elliptic.P384())
	err = service.Validate(context.Background(), declaration, p384Material)
	p384Material.Clear()
	assertFailureCode(t, err, "secret.input")
}

func generatedLikeTLSMaterial(t *testing.T, declaration secrets.Declaration, curve elliptic.Curve) secrets.Material {
	t.Helper()
	key, err := ecdsa.GenerateKey(curve, cryptorand.Reader)
	if err != nil {
		t.Fatalf("generate alternate key: %v", err)
	}
	defer clearPrivateKey(key)
	notBefore := generationTime.UTC().Truncate(time.Second)
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: declaration.Generation.CommonName},
		NotBefore:             notBefore,
		NotAfter:              notBefore.AddDate(0, 0, declaration.Generation.ValidityDays),
		DNSNames:              slices.Clone(declaration.Generation.DNSNames),
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	certificateDER, err := x509.CreateCertificate(cryptorand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create alternate certificate: %v", err)
	}
	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificateDER})
	clear(certificateDER)
	privateDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		clear(certificate)
		t.Fatalf("marshal alternate private key: %v", err)
	}
	privateKey := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER})
	clear(privateDER)
	value := secrets.NewMaterial(map[secrets.Part][]byte{
		secrets.CertificatePart: certificate, secrets.PrivateKeyPart: privateKey,
	})
	clear(certificate)
	clear(privateKey)
	return value
}

func TestGenerateSSHAlgorithmsAndComment(t *testing.T) {
	tests := []struct {
		keyType string
		prefix  string
	}{
		{"", "ssh-ed25519 "},
		{"ed25519", "ssh-ed25519 "},
		{"rsa", "ssh-rsa "},
		{"ecdsa-p256", "ecdsa-sha2-nistp256 "},
		{"ecdsa-p384", "ecdsa-sha2-nistp384 "},
		{"ecdsa-p521", "ecdsa-sha2-nistp521 "},
	}
	for _, test := range tests {
		name := test.keyType
		if name == "" {
			name = "default"
		}
		t.Run(name, func(t *testing.T) {
			service := New(nil, Options{Random: cryptorand.Reader, Clock: func() time.Time { return generationTime }})
			value, err := service.Generate(context.Background(), secrets.Declaration{
				Type: "sshKeyPair", Source: "generated", Generation: secrets.Generation{KeyType: test.keyType, Comment: "generated-key"},
			})
			if err != nil {
				t.Fatalf("Generate: %v (%s)", err, diagnosticMessage(err))
			}
			defer value.Clear()
			privateKey := requiredPart(t, value, secrets.PrivateKeyPart)
			publicKey := requiredPart(t, value, secrets.PublicKeyPart)
			if !strings.HasPrefix(string(publicKey), test.prefix) || !strings.HasSuffix(string(publicKey), " generated-key\n") {
				t.Fatal("public key format or comment differs")
			}
			if err := validateSSH(privateKey, publicKey); err != nil {
				t.Fatalf("generated pair: %v", err)
			}
			clear(privateKey)
			clear(publicKey)
		})
	}
}

func TestGeneratedSSHCommentAndPublicPartLimitsArePreflighted(t *testing.T) {
	declaration := secrets.Declaration{
		Type: "sshKeyPair", Source: "generated",
		Generation: secrets.Generation{Comment: strings.Repeat("c", secrets.MaxPartBytes+1)},
	}
	value, err := New(nil, Options{Random: panicReader{}}).Generate(context.Background(), declaration)
	value.Clear()
	assertFailureCode(t, err, "secret.store.limit")

	seed := bytes.Repeat([]byte{0x42}, ed25519.SeedSize)
	privateKey := ed25519.NewKeyFromSeed(seed)
	clear(seed)
	defer clearPrivateKey(privateKey)
	public, err := ssh.NewPublicKey(privateKey.Public())
	if err != nil {
		t.Fatal(err)
	}
	base := bytes.TrimSuffix(ssh.MarshalAuthorizedKey(public), []byte{'\n'})
	exactComment := strings.Repeat("c", secrets.MaxPartBytes-len(base)-2)
	encoded, err := marshalGeneratedSSHPublic(public, exactComment)
	if err != nil || len(encoded) != secrets.MaxPartBytes {
		t.Fatalf("exact-limit SSH public part: size=%d error=%v", len(encoded), err)
	}
	clear(encoded)
	encoded, err = marshalGeneratedSSHPublic(public, exactComment+"c")
	if encoded != nil {
		clear(encoded)
		t.Fatalf("over-limit SSH public part returned %d bytes", len(encoded))
	}
	assertFailureCode(t, err, "secret.store.limit")
}

func TestValidateGeneratedSSHRequiresDeclaredAlgorithmCommentAndCanonicalPublicBytes(t *testing.T) {
	service := New(nil, Options{Random: cryptorand.Reader})
	declaration := secrets.Declaration{
		Type: "sshKeyPair", Source: "generated", Generation: secrets.Generation{KeyType: "ed25519", Comment: "generated-key"},
	}
	value, err := service.Generate(context.Background(), declaration)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	defer value.Clear()
	privateKey := requiredPart(t, value, secrets.PrivateKeyPart)
	publicKey := requiredPart(t, value, secrets.PublicKeyPart)
	defer clear(privateKey)
	defer clear(publicKey)

	wrongAlgorithm := declaration
	wrongAlgorithm.Generation.KeyType = "ecdsa-p256"
	wrongComment := declaration
	wrongComment.Generation.Comment = "other-comment"
	noncanonicalPublic := bytes.TrimSuffix(slices.Clone(publicKey), []byte{'\n'})
	defer clear(noncanonicalPublic)
	if err := validateSSH(privateKey, noncanonicalPublic); err != nil {
		t.Fatalf("generic SSH validation rejected equivalent public bytes: %v", err)
	}

	tests := []struct {
		name        string
		declaration secrets.Declaration
		publicKey   []byte
	}{
		{name: "algorithm", declaration: wrongAlgorithm, publicKey: publicKey},
		{name: "comment", declaration: wrongComment, publicKey: publicKey},
		{name: "public serialization", declaration: declaration, publicKey: noncanonicalPublic},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := secrets.NewMaterial(map[secrets.Part][]byte{
				secrets.PrivateKeyPart: privateKey,
				secrets.PublicKeyPart:  test.publicKey,
			})
			defer candidate.Clear()
			assertFailureCode(t, service.Validate(context.Background(), test.declaration, candidate), "secret.input")
		})
	}
}

func TestClearPrivateKeyOverwritesBigIntegerBackingStorage(t *testing.T) {
	secretInteger := func(seed byte) *big.Int {
		return new(big.Int).SetBytes(bytes.Repeat([]byte{seed}, 64))
	}
	integers := []*big.Int{
		secretInteger(1), secretInteger(2), secretInteger(3), secretInteger(4), secretInteger(5),
		secretInteger(6), secretInteger(7), secretInteger(8), secretInteger(9),
	}
	backings := make([][]big.Word, len(integers))
	for index, integer := range integers {
		words := integer.Bits()
		backings[index] = words[:cap(words)]
	}
	key := &rsa.PrivateKey{
		D:      integers[0],
		Primes: []*big.Int{integers[1], integers[2]},
		Precomputed: rsa.PrecomputedValues{
			Dp: integers[3], Dq: integers[4], Qinv: integers[5],
			CRTValues: []rsa.CRTValue{{Exp: integers[6], Coeff: integers[7], R: integers[8]}},
		},
	}
	clearPrivateKey(key)
	for index, integer := range integers {
		if integer.Sign() != 0 {
			t.Fatalf("RSA private integer %d was not reset", index)
		}
		for _, word := range backings[index] {
			if word != 0 {
				t.Fatalf("RSA private integer %d retained a backing limb", index)
			}
		}
	}
	if key.D != nil || len(key.Primes) != 0 || key.Precomputed.Dp != nil || key.Precomputed.Dq != nil ||
		key.Precomputed.Qinv != nil || len(key.Precomputed.CRTValues) != 0 {
		t.Fatal("RSA private key retained cleared secret references")
	}

	ecdsaInteger := secretInteger(10)
	ecdsaWords := ecdsaInteger.Bits()
	ecdsaBacking := ecdsaWords[:cap(ecdsaWords)]
	ecdsaKey := &ecdsa.PrivateKey{D: ecdsaInteger}
	clearPrivateKey(ecdsaKey)
	if ecdsaInteger.Sign() != 0 || ecdsaKey.D != nil {
		t.Fatal("ECDSA private integer was not reset")
	}
	for _, word := range ecdsaBacking {
		if word != 0 {
			t.Fatal("ECDSA private integer retained a backing limb")
		}
	}
}

func TestGenerationCryptographyFailuresAreInjectableAndNonDisclosing(t *testing.T) {
	privateFailure := errors.New("private-cryptography-detail")
	tests := []struct {
		name         string
		declaration  secrets.Declaration
		cryptography Cryptography
	}{
		{
			name: "RSA generation",
			declaration: secrets.Declaration{
				Type: "sshKeyPair", Source: "generated", Generation: secrets.Generation{KeyType: "rsa"},
			},
			cryptography: testCryptography{generateRSA: func(int) (*rsa.PrivateKey, error) { return nil, privateFailure }},
		},
		{
			name: "ECDSA generation",
			declaration: secrets.Declaration{
				Type: "sshKeyPair", Source: "generated", Generation: secrets.Generation{KeyType: "ecdsa-p256"},
			},
			cryptography: testCryptography{generateECDSA: func(elliptic.Curve) (*ecdsa.PrivateKey, error) { return nil, privateFailure }},
		},
		{
			name: "certificate creation",
			declaration: secrets.Declaration{
				Type: "tlsCertificate", Source: "generated", Generation: secrets.Generation{CommonName: "service.example", ValidityDays: 30},
			},
			cryptography: testCryptography{createCertificate: func(*x509.Certificate, *x509.Certificate, crypto.PublicKey, crypto.Signer) ([]byte, error) {
				return nil, privateFailure
			}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := New(nil, Options{Cryptography: test.cryptography, Clock: func() time.Time { return generationTime }})
			value, err := service.Generate(context.Background(), test.declaration)
			value.Clear()
			assertFailureCode(t, err, "secret.store.crypto")
			if strings.Contains(err.Error(), privateFailure.Error()) || strings.Contains(diagnosticMessage(err), privateFailure.Error()) {
				t.Fatal("cryptography failure exposed its cause")
			}
		})
	}
}

func TestGenerationObservesCancellationAfterInjectedKeyGeneration(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	secretInteger := new(big.Int).SetBytes(bytes.Repeat([]byte{0xa7}, 64))
	words := secretInteger.Bits()
	backing := words[:cap(words)]
	cryptography := testCryptography{generateRSA: func(int) (*rsa.PrivateKey, error) {
		cancel()
		return &rsa.PrivateKey{D: secretInteger}, nil
	}}
	value, err := New(nil, Options{Cryptography: cryptography}).Generate(ctx, secrets.Declaration{
		Type: "sshKeyPair", Source: "generated", Generation: secrets.Generation{KeyType: "rsa"},
	})
	value.Clear()
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation = %v", err)
	}
	if secretInteger.Sign() != 0 {
		t.Fatal("canceled key generation retained its private integer")
	}
	for _, word := range backing {
		if word != 0 {
			t.Fatal("canceled key generation retained a private backing limb")
		}
	}
}

func TestGenerationObservesCancellationAfterInjectedCertificateCreation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	returned := []byte("private-certificate-buffer")
	cryptography := testCryptography{createCertificate: func(*x509.Certificate, *x509.Certificate, crypto.PublicKey, crypto.Signer) ([]byte, error) {
		cancel()
		return returned, nil
	}}
	value, err := New(nil, Options{Cryptography: cryptography, Clock: func() time.Time { return generationTime }}).Generate(ctx, secrets.Declaration{
		Type: "tlsCertificate", Source: "generated", Generation: secrets.Generation{CommonName: "service.example", ValidityDays: 30},
	})
	value.Clear()
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation = %v", err)
	}
	if !bytes.Equal(returned, make([]byte, len(returned))) {
		t.Fatal("canceled certificate creation retained returned bytes")
	}
}

func TestGenerationRejectsWrongParametersAndNormalizesDependencyFailures(t *testing.T) {
	service := New(nil, Options{Random: errorReader{}, Clock: func() time.Time { return generationTime }})
	value, err := service.Generate(context.Background(), secrets.Declaration{Type: "token", Source: "generated"})
	value.Clear()
	assertFailureCode(t, err, "secret.store.crypto")
	if strings.Contains(diagnosticMessage(err), "private-reader-detail") {
		t.Fatal("randomness failure exposed its cause")
	}

	invalid := []secrets.Declaration{
		{Type: "opaque", Source: "generated"},
		{Type: "token", Source: "generated", Generation: secrets.Generation{Comment: "wrong"}},
		{Type: "usernamePassword", Source: "generated", Generation: secrets.Generation{Username: "bad name"}},
		{Type: "caBundle", Source: "generated", Generation: secrets.Generation{CommonName: "ca", DNSNames: []string{"UPPER.example"}}},
		{Type: "tlsCertificate", Source: "generated", Generation: secrets.Generation{CommonName: "", ValidityDays: 30}},
		{Type: "sshKeyPair", Source: "generated", Generation: secrets.Generation{KeyType: "dsa"}},
		{Type: "sshKeyPair", Source: "generated", Generation: secrets.Generation{Comment: " trailing "}},
	}
	for _, declaration := range invalid {
		value, err := New(nil, Options{Random: cryptorand.Reader, Clock: func() time.Time { return generationTime }}).Generate(context.Background(), declaration)
		value.Clear()
		assertFailureCode(t, err, "secret.source")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	value, err = New(nil, Options{Random: panicReader{}}).Generate(ctx, secrets.Declaration{Type: "token", Source: "generated"})
	value.Clear()
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled generation = %v", err)
	}

	serialService := New(nil, Options{Random: zeroReader{}})
	if serial, err := serialService.randomSerial(context.Background()); serial != nil || err == nil {
		t.Fatal("all-zero serial entropy did not fail closed")
	}
}

func requiredPart(t *testing.T, value secrets.Material, part secrets.Part) []byte {
	t.Helper()
	data, exists := value.Part(part)
	if !exists {
		t.Fatalf("material lacks %q; has %v", part, value.Parts())
	}
	return data
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errors.New("private-reader-detail") }

type panicReader struct{}

func (panicReader) Read([]byte) (int, error) { panic("reader must not be called") }

var _ io.Reader = errorReader{}

type testCryptography struct {
	generateECDSA     func(elliptic.Curve) (*ecdsa.PrivateKey, error)
	generateRSA       func(int) (*rsa.PrivateKey, error)
	createCertificate func(*x509.Certificate, *x509.Certificate, crypto.PublicKey, crypto.Signer) ([]byte, error)
}

func (c testCryptography) GenerateECDSA(curve elliptic.Curve) (*ecdsa.PrivateKey, error) {
	if c.generateECDSA != nil {
		return c.generateECDSA(curve)
	}
	return standardCryptography{}.GenerateECDSA(curve)
}

func (c testCryptography) GenerateRSA(bits int) (*rsa.PrivateKey, error) {
	if c.generateRSA != nil {
		return c.generateRSA(bits)
	}
	return standardCryptography{}.GenerateRSA(bits)
}

func (c testCryptography) CreateCertificate(template, parent *x509.Certificate, publicKey crypto.PublicKey, privateKey crypto.Signer) ([]byte, error) {
	if c.createCertificate != nil {
		return c.createCertificate(template, parent, publicKey, privateKey)
	}
	return standardCryptography{}.CreateCertificate(template, parent, publicKey, privateKey)
}

var _ Cryptography = testCryptography{}
