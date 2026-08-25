package crypto

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncryptDecrypt(t *testing.T) {
	err := InitKey([]byte("0123456789abcdef0123456789abcdef"))
	require.NoError(t, err)

	tests := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"simple", "hello world"},
		{"password", "MyS3cretP@ssw0rd!2024"},
		{"unicode", "数据库密码测试"},
		{"long", string(make([]byte, 1024))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encrypted, err := Encrypt(tt.input)
			require.NoError(t, err)
			assert.NotEqual(t, tt.input, encrypted)

			decrypted, err := Decrypt(encrypted)
			require.NoError(t, err)
			assert.Equal(t, tt.input, decrypted)
		})
	}
}

func TestEncryptDifferentNonces(t *testing.T) {
	err := InitKey([]byte("0123456789abcdef0123456789abcdef"))
	require.NoError(t, err)

	e1, _ := Encrypt("same")
	e2, _ := Encrypt("same")
	assert.NotEqual(t, e1, e2)
}

func TestDecryptInvalidInput(t *testing.T) {
	err := InitKey([]byte("0123456789abcdef0123456789abcdef"))
	require.NoError(t, err)

	_, err = Decrypt("not-base64!!!")
	assert.Error(t, err)

	_, err = Decrypt("YWJj")
	assert.Error(t, err)
}

func TestInvalidKeyLength(t *testing.T) {
	err := InitKey([]byte("short"))
	assert.Error(t, err)

	err = InitKey([]byte("012345678901234567890123456789012")) // 33 bytes
	assert.Error(t, err)
}
