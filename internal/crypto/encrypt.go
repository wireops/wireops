package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
)

func Encrypt(plaintext []byte, key []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func Decrypt(encoded string, key []byte) ([]byte, error) {
	ciphertext, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, errors.New("ciphertext too short")
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	return gcm.Open(nil, nonce, ciphertext, nil)
}

// gcmMinCiphertextLen is the smallest possible output of Encrypt: a 12-byte
// GCM nonce plus the 16-byte authentication tag Seal appends even for an
// empty plaintext. Anything shorter cannot be our ciphertext.
const gcmMinCiphertextLen = 12 + 16

// IsEncrypted heuristically detects whether value is already AES-GCM
// ciphertext produced by Encrypt, so callers can avoid double-encrypting on
// update. It cannot be exact (any sufficiently long base64 string decodes
// successfully), but rejecting anything shorter than the minimum possible
// ciphertext length rules out ordinary passwords/tokens, which is the
// common false-positive case this previously missed with a bare ">12" check.
func IsEncrypted(value string) bool {
	if len(value) == 0 {
		return false
	}
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return false
	}
	return len(decoded) >= gcmMinCiphertextLen
}
