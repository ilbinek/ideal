// Package crypto provides the symmetric decryption Steam uses for depot
// manifests and chunks: AES-256 with a per-message IV that is itself
// AES-ECB-encrypted and prepended, then CBC over the body with PKCS7 padding.
// It is a thin, key-based wrapper over the vendored go-steam cryptoutil.
package crypto

import (
	"crypto/aes"
	"fmt"

	"github.com/0xAozora/go-steam/cryptoutil"
)

// SymmetricDecrypt decrypts data with the given 32-byte depot/session key.
// The input is consumed (decrypted in place) and must be a multiple of the AES
// block size. The returned slice aliases the input.
func SymmetricDecrypt(key, data []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("crypto: bad key: %w", err)
	}
	if len(data) < aes.BlockSize || len(data)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("crypto: ciphertext length %d not a multiple of block size", len(data))
	}
	return cryptoutil.SymmetricDecrypt(block, data)
}
