package material

import (
	"bytes"
	"errors"

	"golang.org/x/crypto/bcrypt"
)

func BcryptPassword(password []byte) ([]byte, error) {
	if len(password) == 0 || len(password) > 72 || bytes.IndexByte(password, 0) >= 0 {
		return nil, errors.New("invalid password input")
	}
	return bcrypt.GenerateFromPassword(password, bcryptCost)
}

func BcryptPasswordMatches(password, hash []byte) bool {
	cost, err := bcrypt.Cost(hash)
	if err != nil || cost != 10 && cost != bcryptCost {
		return false
	}
	return bcrypt.CompareHashAndPassword(hash, password) == nil
}
