package material

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/secrets"
)

type testInput struct {
	reader io.Reader
	reads  int
}

func (i *testInput) Read(ctx context.Context, destination []byte) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	i.reads++
	return i.reader.Read(destination)
}

func TestAcquireStdinNormalizesTypedLinesAndPreservesOpaqueBytes(t *testing.T) {
	tests := []struct {
		name        string
		declaration secrets.Declaration
		input       secrets.Input
		bytes       []byte
		part        secrets.Part
		want        []byte
	}{
		{
			name:        "token",
			declaration: secrets.Declaration{Type: "token", Source: "contextStore"},
			input:       secrets.Input{ValueStdin: true}, bytes: []byte("token-value\n"),
			part: secrets.ValuePart, want: []byte("token-value"),
		},
		{
			name:        "password",
			declaration: secrets.Declaration{Type: "usernamePassword", Source: "contextStore"},
			input:       secrets.Input{Username: "operator", PasswordStdin: true}, bytes: []byte("password\n"),
			part: secrets.PasswordPart, want: []byte("password"),
		},
		{
			name:        "empty opaque",
			declaration: secrets.Declaration{Type: "opaque", Source: "contextStore"},
			input:       secrets.Input{ValueStdin: true}, bytes: []byte{},
			part: secrets.ValuePart, want: []byte{},
		},
		{
			name:        "binary opaque",
			declaration: secrets.Declaration{Type: "opaque", Source: "contextStore"},
			input:       secrets.Input{ValueStdin: true}, bytes: []byte{0, '\n', 0xff},
			part: secrets.ValuePart, want: []byte{0, '\n', 0xff},
		},
		{
			name:        "Docker JSON",
			declaration: secrets.Declaration{Type: "dockerConfigJson", Source: "contextStore"},
			input:       secrets.Input{ValueStdin: true}, bytes: []byte(`{"auths":{"registry.example":{"auth":"dTpw"}}}`),
			part: secrets.ValuePart, want: []byte(`{"auths":{"registry.example":{"auth":"dTpw"}}}`),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := &testInput{reader: bytes.NewReader(test.bytes)}
			value, err := New(input).Acquire(context.Background(), test.declaration, test.input)
			if err != nil {
				t.Fatalf("Acquire: %v (%+v)", err, diagnostics.Of(err))
			}
			defer value.Clear()
			got, exists := value.Part(test.part)
			if !exists || !bytes.Equal(got, test.want) {
				t.Fatalf("part %q presence or bytes differ", test.part)
			}
			clear(got)
			if input.reads == 0 {
				t.Fatal("stdin was not read")
			}
		})
	}
}

func TestAcquireStdinLineTransportBounds(t *testing.T) {
	tests := []struct {
		name        string
		declaration secrets.Declaration
		input       secrets.Input
		payload     func() []byte
		part        secrets.Part
		valid       bool
	}{
		{
			name: "token exact normalized limit", declaration: secrets.Declaration{Type: "token", Source: "contextStore"},
			input: secrets.Input{ValueStdin: true}, payload: func() []byte {
				return append(bytes.Repeat([]byte{'t'}, secrets.MaxPartBytes), '\n')
			}, part: secrets.ValuePart, valid: true,
		},
		{
			name: "password exact normalized limit", declaration: secrets.Declaration{Type: "usernamePassword", Source: "contextStore"},
			input: secrets.Input{Username: "operator", PasswordStdin: true}, payload: func() []byte {
				return append(bytes.Repeat([]byte{'p'}, secrets.MaxPartBytes), '\n')
			}, part: secrets.PasswordPart, valid: true,
		},
		{
			name: "token non-LF overflow", declaration: secrets.Declaration{Type: "token", Source: "contextStore"},
			input: secrets.Input{ValueStdin: true}, payload: func() []byte {
				return bytes.Repeat([]byte{'t'}, secrets.MaxPartBytes+1)
			},
		},
		{
			name: "password non-LF overflow", declaration: secrets.Declaration{Type: "usernamePassword", Source: "contextStore"},
			input: secrets.Input{Username: "operator", PasswordStdin: true}, payload: func() []byte {
				return bytes.Repeat([]byte{'p'}, secrets.MaxPartBytes+1)
			},
		},
		{
			name: "token two-byte overflow", declaration: secrets.Declaration{Type: "token", Source: "contextStore"},
			input: secrets.Input{ValueStdin: true}, payload: func() []byte {
				return append(bytes.Repeat([]byte{'t'}, secrets.MaxPartBytes), '\n', 'x')
			},
		},
		{
			name: "password two-byte overflow", declaration: secrets.Declaration{Type: "usernamePassword", Source: "contextStore"},
			input: secrets.Input{Username: "operator", PasswordStdin: true}, payload: func() []byte {
				return append(bytes.Repeat([]byte{'p'}, secrets.MaxPartBytes), '\n', 'x')
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			payload := test.payload()
			defer clear(payload)
			value, err := New(&testInput{reader: bytes.NewReader(payload)}).Acquire(context.Background(), test.declaration, test.input)
			defer value.Clear()
			if !test.valid {
				assertFailureCode(t, err, "secret.store.limit")
				return
			}
			if err != nil {
				t.Fatalf("Acquire: %v (%s)", err, diagnosticMessage(err))
			}
			got := requiredPart(t, value, test.part)
			defer clear(got)
			if len(got) != secrets.MaxPartBytes || got[len(got)-1] == '\n' {
				t.Fatal("line transport was not normalized to the exact decoded part limit")
			}
		})
	}
}

func TestAcquireRejectsInvalidInputShapesBeforeReading(t *testing.T) {
	tests := []struct {
		name        string
		declaration secrets.Declaration
		input       secrets.Input
		code        string
	}{
		{"wrong source", secrets.Declaration{Type: "token", Source: "generated"}, secrets.Input{ValueStdin: true}, "secret.source"},
		{"neither", secrets.Declaration{Type: "token", Source: "contextStore"}, secrets.Input{}, "secret.input"},
		{"both", secrets.Declaration{Type: "token", Source: "contextStore"}, secrets.Input{ValueFile: "unused", ValueStdin: true}, "secret.input"},
		{"extra", secrets.Declaration{Type: "caBundle", Source: "contextStore"}, secrets.Input{CertificateFile: "unused", ValueStdin: true}, "secret.input"},
		{"missing username", secrets.Declaration{Type: "usernamePassword", Source: "contextStore"}, secrets.Input{PasswordStdin: true}, "secret.input"},
		{"invalid username", secrets.Declaration{Type: "usernamePassword", Source: "contextStore"}, secrets.Input{Username: "invalid name", PasswordStdin: true}, "secret.input"},
		{"oversized username", secrets.Declaration{Type: "usernamePassword", Source: "contextStore"}, secrets.Input{Username: strings.Repeat("u", secrets.MaxPartBytes+1), PasswordStdin: true}, "secret.store.limit"},
		{"unknown type", secrets.Declaration{Type: "future", Source: "contextStore"}, secrets.Input{}, "secret.declaration"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := &testInput{reader: strings.NewReader("must-not-be-read")}
			value, err := New(input).Acquire(context.Background(), test.declaration, test.input)
			value.Clear()
			assertFailureCode(t, err, test.code)
			if input.reads != 0 {
				t.Fatalf("invalid input performed %d stdin reads", input.reads)
			}
		})
	}
}

func TestValidateGeneratedTokenAndUsernamePasswordConformance(t *testing.T) {
	service := New(nil)
	urlValue := func(count int) []byte {
		raw := bytes.Repeat([]byte{0xa5}, count)
		defer clear(raw)
		encoded := make([]byte, base64.RawURLEncoding.EncodedLen(count))
		base64.RawURLEncoding.Encode(encoded, raw)
		return encoded
	}
	tests := []struct {
		name        string
		declaration secrets.Declaration
		parts       map[secrets.Part][]byte
		valid       bool
	}{
		{
			name: "canonical token", declaration: secrets.Declaration{Type: "token", Source: "generated", Generation: secrets.Generation{Bytes: 16}},
			parts: map[secrets.Part][]byte{secrets.ValuePart: urlValue(16)}, valid: true,
		},
		{
			name: "token entropy length", declaration: secrets.Declaration{Type: "token", Source: "generated", Generation: secrets.Generation{Bytes: 16}},
			parts: map[secrets.Part][]byte{secrets.ValuePart: urlValue(17)},
		},
		{
			name: "token noncanonical trailing bits", declaration: secrets.Declaration{Type: "token", Source: "generated", Generation: secrets.Generation{Bytes: 16}},
			parts: func() map[secrets.Part][]byte {
				value := urlValue(16)
				value[len(value)-1] = 'B'
				return map[secrets.Part][]byte{secrets.ValuePart: value}
			}(),
		},
		{
			name: "declared username", declaration: secrets.Declaration{Type: "usernamePassword", Source: "generated", Generation: secrets.Generation{Username: "operator"}},
			parts: map[secrets.Part][]byte{secrets.UsernamePart: []byte("operator"), secrets.PasswordPart: urlValue(32)}, valid: true,
		},
		{
			name: "default username mismatch", declaration: secrets.Declaration{Type: "usernamePassword", Source: "generated"},
			parts: map[secrets.Part][]byte{secrets.UsernamePart: []byte("operator"), secrets.PasswordPart: urlValue(32)},
		},
		{
			name: "password entropy length", declaration: secrets.Declaration{Type: "usernamePassword", Source: "generated", Generation: secrets.Generation{Username: "operator"}},
			parts: map[secrets.Part][]byte{secrets.UsernamePart: []byte("operator"), secrets.PasswordPart: urlValue(31)},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := secrets.NewMaterial(test.parts)
			defer value.Clear()
			err := service.Validate(context.Background(), test.declaration, value)
			if test.valid {
				if err != nil {
					t.Fatalf("Validate: %v", err)
				}
				return
			}
			assertFailureCode(t, err, "secret.input")
		})
	}
}

func TestAcquireRejectsExplicitEmptyOrFalseInapplicableFlagsBeforeReading(t *testing.T) {
	tests := []struct {
		name        string
		declaration secrets.Declaration
		input       secrets.Input
	}{
		{
			name: "empty username on token", declaration: secrets.Declaration{Type: "token", Source: "contextStore"},
			input: secrets.Input{Provided: secrets.ValueStdinInput | secrets.UsernameInput, ValueStdin: true},
		},
		{
			name: "false value stdin on usernamePassword", declaration: secrets.Declaration{Type: "usernamePassword", Source: "contextStore"},
			input: secrets.Input{Provided: secrets.UsernameInput | secrets.PasswordStdinInput | secrets.ValueStdinInput, Username: "operator", PasswordStdin: true},
		},
		{
			name: "empty public key on TLS", declaration: secrets.Declaration{Type: "tlsCertificate", Source: "contextStore"},
			input: secrets.Input{Provided: secrets.CertificateFileInput | secrets.PrivateKeyFileInput | secrets.PublicKeyFileInput, CertificateFile: "unused", PrivateKeyFile: "unused"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := &testInput{reader: strings.NewReader("must-not-be-read")}
			value, err := New(input).Acquire(context.Background(), test.declaration, test.input)
			value.Clear()
			assertFailureCode(t, err, "secret.input")
			if input.reads != 0 {
				t.Fatalf("invalid explicit flag performed %d stdin reads", input.reads)
			}
		})
	}
}

func TestAcquireRejectsInvalidLineAndBoundedInput(t *testing.T) {
	for _, value := range [][]byte{nil, []byte("line\r"), []byte("two\nlines"), []byte{'a', 0}, []byte{0xff}} {
		input := &testInput{reader: bytes.NewReader(value)}
		got, err := New(input).Acquire(context.Background(), secrets.Declaration{Type: "token", Source: "contextStore"}, secrets.Input{ValueStdin: true})
		got.Clear()
		assertFailureCode(t, err, "secret.input")
	}
	input := &testInput{reader: io.LimitReader(zeroReader{}, secrets.MaxPartBytes+1)}
	got, err := New(input).Acquire(context.Background(), secrets.Declaration{Type: "opaque", Source: "contextStore"}, secrets.Input{ValueStdin: true})
	got.Clear()
	assertFailureCode(t, err, "secret.store.limit")
}

func TestValidateDockerJSONIsClosedDuplicateFreeAndHasAuths(t *testing.T) {
	declaration := secrets.Declaration{Type: "dockerConfigJson", Source: "contextStore"}
	valid := []byte(`{"auths":{"one":{}},"credsStore":"helper"}`)
	if err := New(nil).Validate(context.Background(), declaration, secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: valid})); err != nil {
		t.Fatalf("valid Docker JSON: %v", err)
	}
	invalid := [][]byte{
		[]byte(`{"auths":{}}`),
		[]byte(`{"auths":null}`),
		[]byte(`{"notAuths":{"one":{}}}`),
		[]byte(`{"auths":{"one":{}},"auths":{"two":{}}}`),
		[]byte(`{"auths":{"one":{"auth":"a","auth":"b"}}}`),
		[]byte(`{"auths":{"one":{}}} true`),
		{0xff},
	}
	for _, data := range invalid {
		value := secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: data})
		err := New(nil).Validate(context.Background(), declaration, value)
		value.Clear()
		assertFailureCode(t, err, "secret.input")
	}
}

func TestValidatePartsLimitsCancellationAndNonDisclosure(t *testing.T) {
	service := New(nil)
	err := service.Validate(context.Background(), secrets.Declaration{Type: "token", Source: "contextStore"}, secrets.NewMaterial(map[secrets.Part][]byte{
		secrets.PasswordPart: []byte("sensitive-sentinel"),
	}))
	assertFailureCode(t, err, "secret.input")
	if strings.Contains(err.Error(), "sensitive-sentinel") || strings.Contains(diagnosticMessage(err), "sensitive-sentinel") {
		t.Fatal("validation failure exposed material")
	}

	over := bytes.Repeat([]byte{'x'}, secrets.MaxPartBytes+1)
	err = service.Validate(context.Background(), secrets.Declaration{Type: "opaque", Source: "contextStore"}, secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: over}))
	clear(over)
	assertFailureCode(t, err, "secret.store.limit")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = service.Validate(ctx, secrets.Declaration{Type: "opaque", Source: "contextStore"}, secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("value")}))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation = %v", err)
	}
}

func TestJSONDepthIsBounded(t *testing.T) {
	data := []byte(strings.Repeat("[", maxJSONDepth+2) + "0" + strings.Repeat("]", maxJSONDepth+2))
	assertFailureCode(t, validateUniqueJSON(data), "secret.store.limit")
}

func TestJSONTrailingCompositeIsRejectedWithoutScanningIt(t *testing.T) {
	depth := maxJSONDepth * 64
	data := []byte(`{} ` + strings.Repeat("[", depth) + "0" + strings.Repeat("]", depth))
	assertFailureCode(t, validateUniqueJSON(data), "secret.input")
	decoder := json.NewDecoder(bytes.NewReader(data))
	tokens := 0
	if err := scanJSONValue(decoder, 0, &tokens); err != nil {
		t.Fatal(err)
	}
	assertFailureCode(t, requireJSONEOF(decoder), "secret.input")
	if decoder.InputOffset() != int64(len(`{} [`)) {
		t.Fatal("trailing composite was decoded beyond its opening token")
	}
}

func FuzzSecretJSONParsers(f *testing.F) {
	for _, seed := range [][]byte{
		[]byte(`{"auths":{"one":{}}}`),
		[]byte(`{"username":"name","password":"value"}`),
		[]byte(`{"duplicate":1,"duplicate":2}`),
		{0xff, 0, '{'},
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > secrets.MaxPartBytes {
			t.Skip()
		}
		_ = validateUniqueJSON(data)
		_ = dockerConfig(data)
	})
}

func assertFailureCode(t *testing.T, err error, want string) {
	t.Helper()
	diagnostics := diagnostics.Of(err)
	if len(diagnostics) != 1 || diagnostics[0].Code != want {
		t.Fatalf("failure = %v (%+v), want %s", err, diagnostics, want)
	}
}

func diagnosticMessage(err error) string {
	diagnostics := diagnostics.Of(err)
	if len(diagnostics) == 0 {
		return ""
	}
	return diagnostics[0].Message
}

type zeroReader struct{}

func (zeroReader) Read(destination []byte) (int, error) {
	clear(destination)
	return len(destination), nil
}
