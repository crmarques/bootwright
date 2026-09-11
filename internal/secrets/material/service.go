package material

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/custody"
)

type Options struct {
	Random       io.Reader
	Clock        func() time.Time
	Cryptography Cryptography
	Operator     Operator
}

type FileIdentity struct {
	UID  int
	Home string
}

type Service struct {
	input        InputReader
	random       io.Reader
	clock        func() time.Time
	cryptography Cryptography
	operator     Operator
}

var _ custody.Materializer = (*Service)(nil)

func New(input InputReader, options ...Options) *Service {
	service := &Service{input: input, random: cryptorand.Reader, clock: time.Now, cryptography: standardCryptography{}}
	if len(options) > 0 {
		service.operator = options[0].Operator
		if options[0].Random != nil {
			service.random = options[0].Random
		}
		if options[0].Clock != nil {
			service.clock = options[0].Clock
		}
		if options[0].Cryptography != nil {
			service.cryptography = options[0].Cryptography
		}
	}
	return service
}

func (s *Service) Acquire(ctx context.Context, declaration secrets.Declaration, input secrets.Input) (secrets.Material, error) {
	if err := ctx.Err(); err != nil {
		return secrets.Material{}, err
	}
	if declaration.Source != "contextStore" {
		return secrets.Material{}, failure("source", "explicit material can be stored only for a contextStore declaration", "")
	}
	requests, literal, stdinRequest, err := acquisition(declaration, input)
	if err != nil {
		return secrets.Material{}, err
	}
	defer clearParts(literal)
	parts, err := s.readFileParts(ctx, requests, "", false)
	if err != nil {
		return secrets.Material{}, err
	}
	defer clearParts(parts)
	for part, value := range literal {
		parts[part] = slices.Clone(value)
	}
	if stdinRequest.Part != "" {
		value, err := s.readInput(ctx, stdinRequest.Encoding)
		if err != nil {
			return secrets.Material{}, err
		}
		parts[stdinRequest.Part] = value
	}
	if declaration.Type == "sshKeyPair" && input.PublicKeyFile == "" {
		public, err := deriveSSHPublic(parts[secrets.PrivateKeyPart])
		if err != nil {
			return secrets.Material{}, err
		}
		parts[secrets.PublicKeyPart] = public
	}
	return s.finish(ctx, declaration, parts)
}

func (s *Service) File(ctx context.Context, declaration secrets.Declaration) (secrets.Material, error) {
	if err := ctx.Err(); err != nil {
		return secrets.Material{}, err
	}
	if declaration.Source != "file" {
		return secrets.Material{}, failure("source", "material does not use a file source", "")
	}
	requests, structured, derivePublic, err := declarationFiles(declaration)
	if err != nil {
		return secrets.Material{}, err
	}
	parts, err := s.readFileParts(ctx, requests, declaration.Origin, true)
	if err != nil {
		return secrets.Material{}, err
	}
	defer clearParts(parts)
	if structured {
		value := parts[secrets.ValuePart]
		delete(parts, secrets.ValuePart)
		username, password, err := usernamePassword(value)
		clear(value)
		if err != nil {
			return secrets.Material{}, err
		}
		parts[secrets.UsernamePart] = username
		parts[secrets.PasswordPart] = password
	}
	if derivePublic {
		public, err := deriveSSHPublic(parts[secrets.PrivateKeyPart])
		if err != nil {
			return secrets.Material{}, err
		}
		parts[secrets.PublicKeyPart] = public
	}
	return s.finish(ctx, declaration, parts)
}

func (s *Service) Validate(ctx context.Context, declaration secrets.Declaration, value secrets.Material) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if value.Size() > secrets.MaxVersionBytes {
		return failure("store.limit", "secret material exceeds the version byte limit", "")
	}
	want := declaration.Parts()
	got := value.Parts()
	if len(want) == 0 || !slices.Equal(want, got) {
		return failure("input", "secret material does not contain exactly the parts required by its declaration", "")
	}
	parts := make(map[secrets.Part][]byte, len(got))
	defer clearParts(parts)
	for _, part := range got {
		data, _ := value.Part(part)
		if len(data) > secrets.MaxPartBytes {
			clear(data)
			return failure("store.limit", "a secret material part exceeds the byte limit", "")
		}
		parts[part] = data
	}

	var err error
	switch declaration.Type {
	case "opaque":
		if declaration.Source == "generated" {
			err = failure("source", "declared secret type does not permit generation", "")
		}
	case "token":
		if declaration.Source == "generated" {
			err = validateGeneratedToken(declaration, parts[secrets.ValuePart])
		} else {
			err = line(parts[secrets.ValuePart], "token")
		}
	case "usernamePassword":
		if declaration.Source == "generated" {
			err = validateGeneratedUsernamePassword(declaration, parts[secrets.UsernamePart], parts[secrets.PasswordPart])
		} else if err = username(parts[secrets.UsernamePart]); err == nil {
			err = line(parts[secrets.PasswordPart], "password")
		}
	case "dockerConfigJson":
		if declaration.Source == "generated" {
			err = failure("source", "declared secret type does not permit generation", "")
		} else {
			err = dockerConfig(parts[secrets.ValuePart])
		}
	case "caBundle":
		if declaration.Source == "generated" {
			err = s.validateGeneratedCertificate(declaration, parts[secrets.CertificatePart], parts[secrets.PrivateKeyPart])
		} else {
			err = validateCA(parts[secrets.CertificatePart], parts[secrets.PrivateKeyPart], s.now())
		}
	case "tlsCertificate":
		if declaration.Source == "generated" {
			err = s.validateGeneratedCertificate(declaration, parts[secrets.CertificatePart], parts[secrets.PrivateKeyPart])
		} else {
			err = validateTLS(parts[secrets.CertificatePart], parts[secrets.PrivateKeyPart], s.now())
		}
	case "sshKeyPair":
		if declaration.Source == "generated" {
			err = validateGeneratedSSH(declaration, parts[secrets.PrivateKeyPart], parts[secrets.PublicKeyPart])
		} else {
			err = validateSSH(parts[secrets.PrivateKeyPart], parts[secrets.PublicKeyPart])
		}
	default:
		return failure("declaration", "secret declaration has an unsupported type", "")
	}
	if err != nil {
		return err
	}
	return ctx.Err()
}

func (s *Service) finish(ctx context.Context, declaration secrets.Declaration, parts map[secrets.Part][]byte) (secrets.Material, error) {
	normalizeLines(declaration, parts)
	total := 0
	for _, data := range parts {
		if len(data) > secrets.MaxPartBytes {
			return secrets.Material{}, failure("store.limit", "a secret material part exceeds the byte limit", "")
		}
		if len(data) > secrets.MaxVersionBytes-total {
			return secrets.Material{}, failure("store.limit", "secret material exceeds the version byte limit", "")
		}
		total += len(data)
	}
	value := secrets.NewMaterial(parts)
	if err := s.Validate(ctx, declaration, value); err != nil {
		value.Clear()
		return secrets.Material{}, err
	}
	return value, nil
}

func (s *Service) readInput(ctx context.Context, encoding transportEncoding) ([]byte, error) {
	if s == nil || s.input == nil {
		return nil, failure("input", "secret standard input is not configured", "")
	}
	maximum, valid := encoding.maximumBytes()
	if !valid {
		return nil, failure("input", "secret standard input uses an invalid transport encoding", "")
	}
	buffer := make([]byte, maximum+1)
	offset := 0
	for offset < len(buffer) {
		if err := ctx.Err(); err != nil {
			clear(buffer)
			return nil, err
		}
		n, err := s.input.Read(ctx, buffer[offset:])
		if n < 0 || n > len(buffer)-offset {
			clear(buffer)
			return nil, failure("input", "secret standard input returned an invalid byte count", "")
		}
		offset += n
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil || n == 0 {
			clear(buffer)
			if canceled := ctx.Err(); canceled != nil {
				return nil, canceled
			}
			return nil, failure("input", "secret standard input could not be read safely", "")
		}
	}
	if offset > maximum {
		clear(buffer)
		return nil, failure("store.limit", "secret standard input exceeds its transport byte limit", "")
	}
	value := slices.Clone(buffer[:offset])
	clear(buffer)
	return value, nil
}

// Bound both decoded parts and an optional password LF, with maximal JSON
// escaping of strings/member names, delimiters and a final document LF.
const maxUsernamePasswordJSONBytes = 6*(secrets.MaxVersionBytes+1+len("username")+len("password")) + 13 + 1

type transportEncoding uint8

const (
	transportExact transportEncoding = iota
	transportOptionalFinalLF
	transportUsernamePasswordJSON
)

func (encoding transportEncoding) maximumBytes() (int, bool) {
	switch encoding {
	case transportExact:
		return secrets.MaxPartBytes, true
	case transportOptionalFinalLF:
		return secrets.MaxPartBytes + 1, true
	case transportUsernamePasswordJSON:
		return maxUsernamePasswordJSONBytes, true
	default:
		return 0, false
	}
}

type partRequest struct {
	Part     secrets.Part
	Encoding transportEncoding
}

type fileRequest struct {
	Part     secrets.Part
	Path     string
	Encoding transportEncoding
}

func acquisition(declaration secrets.Declaration, input secrets.Input) ([]fileRequest, map[secrets.Part][]byte, partRequest, error) {
	if input.Provided&^secrets.AllowedInputFields(declaration.Type) != 0 {
		return nil, nil, partRequest{}, failure("input", "secret input contains a flag that is not accepted by its declared type", "")
	}
	extra := func(allowed ...string) bool {
		set := make(map[string]bool, len(allowed))
		for _, name := range allowed {
			set[name] = true
		}
		return input.ValueFile != "" && !set["value-file"] || input.ValueStdin && !set["value-stdin"] ||
			input.Username != "" && !set["username"] || input.PasswordFile != "" && !set["password-file"] ||
			input.PasswordStdin && !set["password-stdin"] || input.CertificateFile != "" && !set["certificate-file"] ||
			input.PrivateKeyFile != "" && !set["private-key-file"] || input.PublicKeyFile != "" && !set["public-key-file"]
	}
	requests := []fileRequest{}
	literal := map[secrets.Part][]byte{}
	stdinRequest := partRequest{}
	requireOne := func(file string, stdin bool, part secrets.Part, encoding transportEncoding) error {
		if (file == "") == !stdin {
			return failure("input", "secret input must select exactly one file or standard-input source", "")
		}
		if file != "" {
			requests = append(requests, fileRequest{Part: part, Path: file, Encoding: encoding})
		} else {
			stdinRequest = partRequest{Part: part, Encoding: encoding}
		}
		return nil
	}

	switch declaration.Type {
	case "opaque", "token", "dockerConfigJson":
		if extra("value-file", "value-stdin") {
			return nil, nil, partRequest{}, failure("input", "secret input contains a flag that is not accepted by its declared type", "")
		}
		encoding := transportExact
		if declaration.Type == "token" {
			encoding = transportOptionalFinalLF
		}
		if err := requireOne(input.ValueFile, input.ValueStdin, secrets.ValuePart, encoding); err != nil {
			return nil, nil, partRequest{}, err
		}
	case "usernamePassword":
		if extra("username", "password-file", "password-stdin") || input.Username == "" {
			return nil, nil, partRequest{}, failure("input", "usernamePassword input requires a username and one password source", "")
		}
		if len(input.Username) > secrets.MaxPartBytes {
			return nil, nil, partRequest{}, failure("store.limit", "secret username exceeds the part byte limit", "")
		}
		if err := username([]byte(input.Username)); err != nil {
			return nil, nil, partRequest{}, err
		}
		if err := requireOne(input.PasswordFile, input.PasswordStdin, secrets.PasswordPart, transportOptionalFinalLF); err != nil {
			return nil, nil, partRequest{}, err
		}
		literal[secrets.UsernamePart] = []byte(input.Username)
	case "caBundle":
		if extra("certificate-file") || input.CertificateFile == "" {
			return nil, nil, partRequest{}, failure("input", "caBundle input requires exactly one certificate file", "")
		}
		requests = append(requests, fileRequest{Part: secrets.CertificatePart, Path: input.CertificateFile})
	case "tlsCertificate":
		if extra("certificate-file", "private-key-file") || input.CertificateFile == "" || input.PrivateKeyFile == "" {
			return nil, nil, partRequest{}, failure("input", "tlsCertificate input requires certificate and private-key files", "")
		}
		requests = append(requests, fileRequest{Part: secrets.CertificatePart, Path: input.CertificateFile}, fileRequest{Part: secrets.PrivateKeyPart, Path: input.PrivateKeyFile})
	case "sshKeyPair":
		if extra("private-key-file", "public-key-file") || input.PrivateKeyFile == "" {
			return nil, nil, partRequest{}, failure("input", "sshKeyPair input requires a private-key file and accepts one public-key file", "")
		}
		requests = append(requests, fileRequest{Part: secrets.PrivateKeyPart, Path: input.PrivateKeyFile})
		if input.PublicKeyFile != "" {
			requests = append(requests, fileRequest{Part: secrets.PublicKeyPart, Path: input.PublicKeyFile})
		}
	default:
		return nil, nil, partRequest{}, failure("declaration", "secret declaration has an unsupported type", "")
	}
	return requests, literal, stdinRequest, nil
}

func declarationFiles(declaration secrets.Declaration) ([]fileRequest, bool, bool, error) {
	files := declaration.Files
	requests := []fileRequest{}
	structured, derivePublic := false, false
	switch declaration.Type {
	case "opaque", "token", "dockerConfigJson", "caBundle", "usernamePassword":
		if files.Path == "" || files.Certificate != "" || files.PrivateKey != "" || files.PublicKey != "" {
			return nil, false, false, failure("source", "file declaration does not provide exactly the paths required by its type", "")
		}
		part := secrets.ValuePart
		if declaration.Type == "caBundle" {
			part = secrets.CertificatePart
		}
		structured = declaration.Type == "usernamePassword"
		encoding := transportExact
		if declaration.Type == "token" {
			encoding = transportOptionalFinalLF
		}
		if structured {
			encoding = transportUsernamePasswordJSON
		}
		requests = append(requests, fileRequest{Part: part, Path: files.Path, Encoding: encoding})
	case "tlsCertificate":
		if files.Path != "" || files.Certificate == "" || files.PrivateKey == "" || files.PublicKey != "" {
			return nil, false, false, failure("source", "TLS file declaration requires exactly certificate and private-key paths", "")
		}
		requests = append(requests, fileRequest{Part: secrets.CertificatePart, Path: files.Certificate}, fileRequest{Part: secrets.PrivateKeyPart, Path: files.PrivateKey})
	case "sshKeyPair":
		if files.Path != "" || files.Certificate != "" || files.PrivateKey == "" {
			return nil, false, false, failure("source", "SSH file declaration requires a private-key path and accepts one public-key path", "")
		}
		requests = append(requests, fileRequest{Part: secrets.PrivateKeyPart, Path: files.PrivateKey})
		if files.PublicKey == "" {
			derivePublic = true
		} else {
			requests = append(requests, fileRequest{Part: secrets.PublicKeyPart, Path: files.PublicKey})
		}
	default:
		return nil, false, false, failure("declaration", "secret declaration has an unsupported type", "")
	}
	return requests, structured, derivePublic, nil
}

func usernamePassword(data []byte) ([]byte, []byte, error) {
	if !utf8.Valid(data) {
		return nil, nil, failure("input", "usernamePassword file is not valid UTF-8 JSON", "")
	}
	if err := validateUniqueJSON(data); err != nil {
		return nil, nil, err
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(data, &document); err != nil || len(document) != 2 {
		return nil, nil, failure("input", "usernamePassword file must be a closed object with username and password strings", "")
	}
	usernameJSON, usernameExists := document["username"]
	passwordJSON, passwordExists := document["password"]
	if !usernameExists || !passwordExists {
		return nil, nil, failure("input", "usernamePassword file must be a closed object with username and password strings", "")
	}
	var usernameRaw, passwordRaw any
	if json.Unmarshal(usernameJSON, &usernameRaw) != nil || json.Unmarshal(passwordJSON, &passwordRaw) != nil {
		return nil, nil, failure("input", "usernamePassword file must be a closed object with username and password strings", "")
	}
	usernameValue, usernameIsString := usernameRaw.(string)
	passwordValue, passwordIsString := passwordRaw.(string)
	if !usernameIsString || !passwordIsString {
		return nil, nil, failure("input", "usernamePassword file must be a closed object with username and password strings", "")
	}
	if len(usernameValue) > secrets.MaxPartBytes {
		return nil, nil, failure("store.limit", "usernamePassword file username exceeds the part byte limit", "")
	}
	if len(passwordValue) > secrets.MaxPartBytes &&
		(len(passwordValue) != secrets.MaxPartBytes+1 || passwordValue[len(passwordValue)-1] != '\n') {
		return nil, nil, failure("store.limit", "usernamePassword file password exceeds the normalized part byte limit", "")
	}
	return []byte(usernameValue), []byte(passwordValue), nil
}

func normalizeLines(declaration secrets.Declaration, parts map[secrets.Part][]byte) {
	if declaration.Type == "token" {
		parts[secrets.ValuePart] = stripFinalLF(parts[secrets.ValuePart])
	}
	if declaration.Type == "usernamePassword" {
		parts[secrets.PasswordPart] = stripFinalLF(parts[secrets.PasswordPart])
	}
}

func stripFinalLF(value []byte) []byte {
	if len(value) > 0 && value[len(value)-1] == '\n' {
		value[len(value)-1] = 0
		value = value[:len(value)-1]
	}
	return value
}

func nonempty(value []byte, name string) error {
	if len(value) == 0 {
		return failure("input", name+" is empty", "")
	}
	return nil
}

func line(value []byte, name string) error {
	if len(value) == 0 || !utf8.Valid(value) || bytes.IndexByte(value, 0) >= 0 || bytes.ContainsAny(value, "\r\n") {
		return failure("input", name+" must be one nonempty UTF-8 line", "")
	}
	return nil
}

func username(value []byte) error {
	if err := line(value, "username"); err != nil {
		return err
	}
	if bytes.IndexByte(value, ':') >= 0 {
		return failure("input", "username must not contain a colon", "")
	}
	for len(value) > 0 {
		r, size := utf8.DecodeRune(value)
		if unicode.IsSpace(r) {
			return failure("input", "username must not contain whitespace", "")
		}
		value = value[size:]
	}
	return nil
}

func clearParts(parts map[secrets.Part][]byte) {
	for _, value := range parts {
		clear(value)
	}
}

func (s *Service) now() time.Time {
	if s == nil || s.clock == nil {
		return time.Time{}
	}
	return s.clock().UTC().Truncate(time.Second)
}

func failure(code, message, path string) error {
	return diagnostics.NewFailure("secret."+code, message, path)
}
