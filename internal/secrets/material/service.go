package material

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
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
	// Files opens secret files under the invoking account's credentials; with
	// none, they open with this process's own.
	Files Files
	// Terminal is the terminal behind standard input; with none, standard
	// input is always read to its end.
	Terminal TerminalInput
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
	files        Files
	terminal     TerminalInput
}

var _ custody.Materializer = (*Service)(nil)

func New(input InputReader, options ...Options) *Service {
	service := &Service{input: input, random: cryptorand.Reader, clock: time.Now, cryptography: standardCryptography{}}
	if len(options) > 0 {
		service.operator = options[0].Operator
		service.files = options[0].Files
		service.terminal = options[0].Terminal
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
	prompt := ""
	if stdinRequest.Part != "" {
		if prompt, err = s.terminalPrompt(declaration, input); err != nil {
			return secrets.Material{}, err
		}
	}
	parts, err := s.readFileParts(ctx, requests)
	if err != nil {
		return secrets.Material{}, err
	}
	defer clearParts(parts)
	for part, value := range literal {
		parts[part] = slices.Clone(value)
	}
	if stdinRequest.Part != "" {
		value, err := s.readStdin(ctx, stdinRequest.Encoding, prompt)
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

// terminalPrompt is the prompt a token or password is typed at when standard
// input is a terminal, and empty when it is not. Any other value is read from
// a pipe or a file, never typed at a terminal, where nothing would delimit it.
func (s *Service) terminalPrompt(declaration secrets.Declaration, input secrets.Input) (string, error) {
	if s.terminal == nil {
		return "", nil
	}
	interactive, err := s.terminal.Interactive()
	if err != nil {
		file := " --value-file <path>"
		if declaration.Type == "usernamePassword" {
			file = " --username <username> --password-file <path>"
		}
		return "", secrets.Refusal("input", "standard input could not be inspected", input.ContextName, declaration.Name, secrets.Command(input.ContextName, "set")+" --name "+declaration.Name+file)
	}
	if !interactive {
		return "", nil
	}
	in := ""
	if input.ContextName != "" {
		in = " in context " + input.ContextName
	}
	switch declaration.Type {
	case "token":
		return "Token for Secret " + declaration.Name + in + ": ", nil
	case "usernamePassword":
		return "Password for Secret " + declaration.Name + in + ": ", nil
	}
	return "", secrets.UsageRefusal("input", "Secret "+declaration.Name+" is a "+declaration.Type+" Secret; its value is read from a pipe or a file, never typed at a terminal", input.ContextName, declaration.Name,
		"pipe the value into "+secrets.Command(input.ContextName, "set")+" --name "+declaration.Name+" --value-stdin, or use --value-file <path>")
}

func (s *Service) readStdin(ctx context.Context, encoding transportEncoding, prompt string) ([]byte, error) {
	if prompt == "" {
		return s.readInput(ctx, encoding)
	}
	maximum, valid := encoding.maximumBytes()
	if !valid {
		return nil, failure("input", "secret standard input uses an invalid transport encoding", "")
	}
	buffer := make([]byte, maximum+1)
	defer clear(buffer)
	n, err := s.terminal.ReadHidden(ctx, prompt, buffer)
	if canceled := ctx.Err(); canceled != nil {
		return nil, canceled
	}
	if err != nil && !errors.Is(err, io.EOF) || n < 0 || n > len(buffer) {
		return nil, failure("input", "secret standard input could not be read safely", "")
	}
	if n > maximum {
		return nil, failure("store.limit", "secret standard input exceeds its transport byte limit", "")
	}
	return slices.Clone(buffer[:n]), nil
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

type transportEncoding uint8

const (
	transportExact transportEncoding = iota
	transportOptionalFinalLF
)

func (encoding transportEncoding) maximumBytes() (int, bool) {
	switch encoding {
	case transportExact:
		return secrets.MaxPartBytes, true
	case transportOptionalFinalLF:
		return secrets.MaxPartBytes + 1, true
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

// acquisition plans the reads of a flag set the declaration's type takes. The
// custody checked it already; this check keeps a direct caller to that rule.
func acquisition(declaration secrets.Declaration, input secrets.Input) ([]fileRequest, map[secrets.Part][]byte, partRequest, error) {
	if err := secrets.CheckInput(input.ContextName, declaration, input); err != nil {
		return nil, nil, partRequest{}, err
	}
	requests := []fileRequest{}
	literal := map[secrets.Part][]byte{}
	stdinRequest := partRequest{}
	one := func(file string, part secrets.Part, encoding transportEncoding) {
		if file != "" {
			requests = append(requests, fileRequest{Part: part, Path: file, Encoding: encoding})
		} else {
			stdinRequest = partRequest{Part: part, Encoding: encoding}
		}
	}

	switch declaration.Type {
	case "opaque", "token", "dockerConfigJson":
		encoding := transportExact
		if declaration.Type == "token" {
			encoding = transportOptionalFinalLF
		}
		one(input.ValueFile, secrets.ValuePart, encoding)
	case "usernamePassword":
		one(input.PasswordFile, secrets.PasswordPart, transportOptionalFinalLF)
		literal[secrets.UsernamePart] = []byte(input.Username)
	case "caBundle":
		requests = append(requests, fileRequest{Part: secrets.CertificatePart, Path: input.CertificateFile})
	case "tlsCertificate":
		requests = append(requests, fileRequest{Part: secrets.CertificatePart, Path: input.CertificateFile}, fileRequest{Part: secrets.PrivateKeyPart, Path: input.PrivateKeyFile})
	case "sshKeyPair":
		requests = append(requests, fileRequest{Part: secrets.PrivateKeyPart, Path: input.PrivateKeyFile})
		if input.PublicKeyFile != "" {
			requests = append(requests, fileRequest{Part: secrets.PublicKeyPart, Path: input.PublicKeyFile})
		}
	default:
		return nil, nil, partRequest{}, failure("declaration", "secret declaration has an unsupported type", "")
	}
	return requests, literal, stdinRequest, nil
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
