package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	argon2Version     uint32 = argon2.Version
	argon2Memory      uint32 = 64 * 1024
	argon2Time        uint32 = 3
	argon2Threads     uint8  = 1
	argon2KeyLen      uint32 = 32
	argon2SaltLen            = 16
	minPasswordLength        = 12
	maxPasswordLength        = 128
)

type PasswordParameters struct {
	Version uint32 `json:"version"`
	Memory  uint32 `json:"memory"`
	Time    uint32 `json:"time"`
	Threads uint8  `json:"threads"`
	KeyLen  uint32 `json:"key_len"`
	SaltLen uint32 `json:"salt_len"`
}

var ErrWeakPassword = errors.New("password does not meet the minimum policy")

func DefaultPasswordParameters() PasswordParameters {
	return PasswordParameters{
		Version: argon2Version,
		Memory:  argon2Memory,
		Time:    argon2Time,
		Threads: argon2Threads,
		KeyLen:  argon2KeyLen,
		SaltLen: argon2SaltLen,
	}
}

func ValidatePassword(password string) error {
	if len([]rune(password)) < minPasswordLength || len([]rune(password)) > maxPasswordLength {
		return ErrWeakPassword
	}
	return nil
}

func HashPassword(password string) (string, json.RawMessage, error) {
	if err := ValidatePassword(password); err != nil {
		return "", nil, err
	}
	parameters := DefaultPasswordParameters()
	salt := make([]byte, parameters.SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", nil, fmt.Errorf("generate password salt: %w", err)
	}
	hash := argon2.IDKey([]byte(password), salt, parameters.Time, parameters.Memory, parameters.Threads, parameters.KeyLen)
	encoded := strings.Join([]string{
		"argon2id",
		"v=" + strconv.FormatUint(uint64(parameters.Version), 10),
		"m=" + strconv.FormatUint(uint64(parameters.Memory), 10) + ",t=" + strconv.FormatUint(uint64(parameters.Time), 10) + ",p=" + strconv.FormatUint(uint64(parameters.Threads), 10),
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	}, "$")
	encodedParameters, err := json.Marshal(parameters)
	if err != nil {
		return "", nil, fmt.Errorf("encode password parameters: %w", err)
	}
	return encoded, encodedParameters, nil
}

func VerifyPassword(password, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 5 || parts[0] != "argon2id" || parts[1] == "" {
		return false, ErrInvalidInput
	}
	version, err := parseUintParameter(parts[1], "v", 1, 32)
	if err != nil || uint32(version) != argon2Version {
		return false, ErrInvalidInput
	}
	settings := strings.Split(parts[2], ",")
	if len(settings) != 3 {
		return false, ErrInvalidInput
	}
	memory, err := parseUintParameter(settings[0], "m", 8*1024, 256*1024)
	if err != nil {
		return false, ErrInvalidInput
	}
	iterations, err := parseUintParameter(settings[1], "t", 1, 10)
	if err != nil {
		return false, ErrInvalidInput
	}
	threads, err := parseUintParameter(settings[2], "p", 1, 8)
	if err != nil {
		return false, ErrInvalidInput
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil || len(salt) < 16 || len(salt) > 64 {
		return false, ErrInvalidInput
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(want) < 16 || len(want) > 64 {
		return false, ErrInvalidInput
	}
	got := argon2.IDKey([]byte(password), salt, uint32(iterations), uint32(memory), uint8(threads), uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

func parseUintParameter(raw, name string, minimum, maximum uint64) (uint64, error) {
	parts := strings.SplitN(raw, "=", 2)
	if len(parts) != 2 || parts[0] != name {
		return 0, ErrInvalidInput
	}
	value, err := strconv.ParseUint(parts[1], 10, 32)
	if err != nil || value < minimum || value > maximum {
		return 0, ErrInvalidInput
	}
	return value, nil
}
