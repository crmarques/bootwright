package reconciliation

import (
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

const (
	MaxAttempt          = 999999
	operationIDPrefix   = "op-"
	allocationAttempts  = 16
	operationEntropy    = 16
	numberDigits        = 6
	minAttemptOrRestart = 1
)

// Entropy is the operating-system randomness capability. It is a function so
// the domain declares no stream vocabulary of its own.
type Entropy func([]byte) (int, error)

// AllocateOperationID reserves a candidate exclusively. Random failure has no
// clock, process-identity or hash fallback: an operation that cannot be named
// from cryptographic entropy is not started at all.
func AllocateOperationID(entropy Entropy, taken func(string) bool) (string, error) {
	if entropy == nil || taken == nil {
		return "", stateError("operation identity allocation is not configured")
	}
	buffer := make([]byte, operationEntropy)
	for range allocationAttempts {
		if err := readFull(entropy, buffer); err != nil {
			return "", stateError("operation identity requires operating-system entropy")
		}
		candidate := operationIDPrefix + hex.EncodeToString(buffer)
		if !taken(candidate) {
			return candidate, nil
		}
	}
	return "", stateError("operation identity allocation exhausted its collision attempts")
}

func ValidOperationID(value string) bool {
	if !strings.HasPrefix(value, operationIDPrefix) || len(value) != len(operationIDPrefix)+2*operationEntropy {
		return false
	}
	decoded, err := hex.DecodeString(value[len(operationIDPrefix):])
	return err == nil && hex.EncodeToString(decoded) == value[len(operationIDPrefix):]
}

// FormatNumber renders an attempt or resolution ordinal. Exhaustion refuses
// before observation or effects; it never wraps or reuses an earlier path.
func FormatNumber(value int) (string, error) {
	if value < minAttemptOrRestart || value > MaxAttempt {
		return "", stateError("lifecycle attempt or resolution numbering is exhausted")
	}
	return fmt.Sprintf("%0*d", numberDigits, value), nil
}

func ParseNumber(value string) (int, error) {
	if len(value) != numberDigits {
		return 0, stateError("lifecycle attempt or resolution number is malformed")
	}
	number, err := strconv.Atoi(value)
	if err != nil || number < minAttemptOrRestart || number > MaxAttempt {
		return 0, stateError("lifecycle attempt or resolution number is malformed")
	}
	if formatted, err := FormatNumber(number); err != nil || formatted != value {
		return 0, stateError("lifecycle attempt or resolution number is not canonical")
	}
	return number, nil
}

func readFull(entropy Entropy, buffer []byte) error {
	for filled := 0; filled < len(buffer); {
		n, err := entropy(buffer[filled:])
		if n <= 0 || err != nil {
			return stateError("entropy source returned no bytes")
		}
		filled += n
	}
	return nil
}
