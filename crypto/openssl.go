package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/pbkdf2"
)

const (
	// OpenSSL "Salted__" magic
	saltedMagic = "Salted__"
	saltLen     = 8
	keyLen      = 32
	ivLen       = 16
	iterations  = 20000
)

// deriveKey derives key and IV from password and salt using PBKDF2-SHA256.
func deriveKey(password string, salt []byte) (key, iv []byte) {
	dk := pbkdf2.Key([]byte(password), salt, iterations, keyLen+ivLen, sha256.New)
	return dk[:keyLen], dk[keyLen:]
}

// pkcs7Pad applies PKCS#7 padding.
func pkcs7Pad(data []byte, blockSize int) []byte {
	padding := blockSize - len(data)%blockSize
	padtext := make([]byte, padding)
	for i := range padtext {
		padtext[i] = byte(padding)
	}
	return append(data, padtext...)
}

// pkcs7Unpad removes PKCS#7 padding.
func pkcs7Unpad(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("empty data")
	}
	padding := int(data[len(data)-1])
	if padding < 1 || padding > aes.BlockSize || padding > len(data) {
		return nil, errors.New("invalid padding")
	}
	for i := len(data) - padding; i < len(data); i++ {
		if data[i] != byte(padding) {
			return nil, errors.New("invalid padding")
		}
	}
	return data[:len(data)-padding], nil
}

// Encrypt encrypts data with password using OpenSSL-compatible AES-256-CBC.
// Output format: "Salted__" + 8-byte salt + ciphertext
func Encrypt(data []byte, password string) ([]byte, error) {
	salt := make([]byte, saltLen)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, fmt.Errorf("generating salt: %w", err)
	}

	key, iv := deriveKey(password, salt)

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("creating cipher: %w", err)
	}

	padded := pkcs7Pad(data, aes.BlockSize)
	ciphertext := make([]byte, len(padded))
	mode := cipher.NewCBCEncrypter(block, iv)
	mode.CryptBlocks(ciphertext, padded)

	// "Salted__" + salt + ciphertext
	result := make([]byte, 0, len(saltedMagic)+saltLen+len(ciphertext))
	result = append(result, []byte(saltedMagic)...)
	result = append(result, salt...)
	result = append(result, ciphertext...)
	return result, nil
}

// Decrypt decrypts OpenSSL-compatible AES-256-CBC encrypted data.
func Decrypt(data []byte, password string) ([]byte, error) {
	if len(data) < len(saltedMagic)+saltLen+aes.BlockSize {
		return nil, errors.New("data too short")
	}

	magic := string(data[:len(saltedMagic)])
	if magic != saltedMagic {
		return nil, errors.New("invalid magic header, expected 'Salted__'")
	}

	salt := data[len(saltedMagic) : len(saltedMagic)+saltLen]
	ciphertext := data[len(saltedMagic)+saltLen:]

	key, iv := deriveKey(password, salt)

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("creating cipher: %w", err)
	}

	if len(ciphertext)%aes.BlockSize != 0 {
		return nil, errors.New("ciphertext not aligned to block size")
	}

	plaintext := make([]byte, len(ciphertext))
	mode := cipher.NewCBCDecrypter(block, iv)
	mode.CryptBlocks(plaintext, ciphertext)

	plaintext, err = pkcs7Unpad(plaintext)
	if err != nil {
		return nil, fmt.Errorf("unpadding: %w", err)
	}

	return plaintext, nil
}

// base64URL is base64 URL encoding without padding.
var base64URL = base64.URLEncoding.WithPadding(base64.NoPadding)

// EncryptName encrypts a filename using the same OpenSSL method, then returns
// a base64url-encoded (no padding) string. Compatible with remotely-save.
func EncryptName(name string, password string) (string, error) {
	encrypted, err := Encrypt([]byte(name), password)
	if err != nil {
		return "", fmt.Errorf("encrypting name %q: %w", name, err)
	}
	return base64URL.EncodeToString(encrypted), nil
}

// DecryptName decodes a base64url-encoded encrypted filename and decrypts it.
func DecryptName(encName string, password string) (string, error) {
	data, err := base64URL.DecodeString(encName)
	if err != nil {
		return "", fmt.Errorf("base64url decoding %q: %w", encName, err)
	}
	decrypted, err := Decrypt(data, password)
	if err != nil {
		return "", fmt.Errorf("decrypting name: %w", err)
	}
	return string(decrypted), nil
}

// IsLikelyEncrypted checks if a key starts with the OpenSSL base64 "Salted__" prefix.
// In base64url, "Salted__" encodes to a string starting with "U2FsdGVkX".
func IsLikelyEncrypted(key string) bool {
	return strings.HasPrefix(key, "U2FsdGVkX")
}
