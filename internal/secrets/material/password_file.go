package material

import (
	"bytes"
	"errors"
	"slices"

	"golang.org/x/crypto/bcrypt"
)

const bcryptCost = 12

func BcryptPasswordFile(username, password []byte) ([]byte, error) {
	if len(username) == 0 || bytes.ContainsFunc(username, func(r rune) bool { return r == ':' || r < 0x20 || r > 0x7e }) {
		return nil, errors.New("the username is empty or holds a colon, a control or a non-ASCII character")
	}
	if len(password) == 0 || len(password) > 72 || bytes.IndexByte(password, 0) >= 0 {
		return nil, errors.New("the password is empty, longer than 72 bytes or holds a NUL")
	}
	hash, err := bcrypt.GenerateFromPassword(password, bcryptCost)
	if err != nil {
		return nil, err
	}
	return slices.Concat(username, []byte(":"), hash, []byte("\n")), nil
}
