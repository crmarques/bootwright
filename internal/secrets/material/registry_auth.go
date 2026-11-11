package material

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
)

type authEntry struct {
	Auth json.RawMessage `json:"auth"`
}

func RegistryAuth(pullSecret []byte, origin string, username, password []byte, mirror string) ([]byte, error) {
	var document struct {
		Auths map[string]authEntry `json:"auths"`
	}
	if err := json.Unmarshal(pullSecret, &document); err != nil {
		return nil, errors.New("the pull secret is not a registry auth document")
	}
	defer func() {
		for _, entry := range document.Auths {
			clear(entry.Auth)
		}
	}()
	token := document.Auths[origin].Auth
	if len(token) < 3 || token[0] != '"' || token[len(token)-1] != '"' || bytes.ContainsAny(token[1:len(token)-1], "\"\\") {
		return nil, errors.New("the pull secret names no auth token for the origin")
	}
	if origin == mirror || len(username) == 0 || len(password) == 0 {
		return nil, errors.New("the mirror's credential is empty or names the origin")
	}
	names, err := json.Marshal([]string{origin, mirror})
	if err != nil {
		return nil, err
	}
	pair := make([]byte, 0, len(username)+1+len(password))
	pair = append(append(append(pair, username...), ':'), password...)
	defer clear(pair)
	encoded := make([]byte, base64.StdEncoding.EncodedLen(len(pair)))
	base64.StdEncoding.Encode(encoded, pair)
	defer clear(encoded)
	quoted := bytes.Split(names[1:len(names)-1], []byte(","))
	var out bytes.Buffer
	out.Grow(48 + len(names) + len(token) + len(encoded))
	out.WriteString(`{"auths":{`)
	out.Write(quoted[0])
	out.WriteString(`:{"auth":`)
	out.Write(token)
	out.WriteString(`},`)
	out.Write(quoted[1])
	out.WriteString(`:{"auth":"`)
	out.Write(encoded)
	out.WriteString(`"}}}`)
	return out.Bytes(), nil
}
