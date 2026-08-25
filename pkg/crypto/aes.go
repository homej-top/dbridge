package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
)

var (
	// In production, this key should be managed by HashiCorp Vault or KMS
	defaultKey  []byte
	legacyKeys  [][]byte
)

// InitKey initializes the encryption key (32 bytes for AES-256-GCM)
func InitKey(key []byte) error {
	if len(key) != 32 {
		return errors.New("key must be 32 bytes for AES-256-GCM")
	}
	defaultKey = make([]byte, 32)
	copy(defaultKey, key)
	return nil
}

// InitKeyWithLegacy initializes the encryption key with legacy keys for backward compatibility.
// When decrypting, it will try the primary key first, then fall back to legacy keys.
func InitKeyWithLegacy(key []byte, legacyKeysInput [][]byte) error {
	if err := InitKey(key); err != nil {
		return err
	}
	for _, lk := range legacyKeysInput {
		if len(lk) == 32 {
			k := make([]byte, 32)
			copy(k, lk)
			legacyKeys = append(legacyKeys, k)
		}
	}
	return nil
}

// Encrypt encrypts plaintext using AES-GCM
func Encrypt(plaintext string) (string, error) {
	if defaultKey == nil {
		return "", errors.New("encryption key not initialized")
	}

	block, err := aes.NewCipher(defaultKey)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt decrypts ciphertext using AES-GCM.
// Tries the primary key first, then falls back to legacy keys.
func Decrypt(ciphertext string) (string, error) {
	if defaultKey == nil {
		return "", errors.New("encryption key not initialized")
	}

	// Try primary key first
	if plaintext, err := decryptWithKey(defaultKey, ciphertext); err == nil {
		return plaintext, nil
	}

	// Try legacy keys
	for _, lk := range legacyKeys {
		if plaintext, err := decryptWithKey(lk, ciphertext); err == nil {
			return plaintext, nil
		}
	}

	return "", errors.New("decrypt error: unable to decrypt with any key")
}

// NeedsMigration returns true if the ciphertext was encrypted with a legacy key
// and needs to be re-encrypted with the current key.
func NeedsMigration(ciphertext string) bool {
	if defaultKey == nil {
		return false
	}
	if _, err := decryptWithKey(defaultKey, ciphertext); err == nil {
		return false
	}
	for _, lk := range legacyKeys {
		if _, err := decryptWithKey(lk, ciphertext); err == nil {
			return true
		}
	}
	return false
}

// decryptWithKey decrypts ciphertext using a specific key.
func decryptWithKey(key []byte, ciphertext string) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", errors.New("ciphertext too short")
	}

	plaintext, err := gcm.Open(nil, data[:nonceSize], data[nonceSize:], nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}

func deriveKey(password string) []byte {
	h := sha256.Sum256([]byte(password))
	return h[:]
}

// EncryptWithPassword encrypts plaintext using AES-GCM with a key derived from the password
func EncryptWithPassword(plaintext, password string) (string, error) {
	key := deriveKey(password)

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// DecryptWithPassword decrypts ciphertext using AES-GCM with a key derived from the password
func DecryptWithPassword(ciphertext, password string) (string, error) {
	key := deriveKey(password)

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", errors.New("ciphertext too short")
	}

	plaintext, err := gcm.Open(nil, data[:nonceSize], data[nonceSize:], nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}
