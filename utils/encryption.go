package utils

import (
	"crypto/md5"
	"encoding/base64"
)

// EncryptMessage encrypts a message using a simple XOR cipher with a key derived from MD5
// and returns a base64 encoded string that only contains characters safe for Minecraft chat
func EncryptMessage(message, key string) (string, error) {
	// Create a fixed-size encryption key by hashing the provided key
	hasher := md5.New()
	hasher.Write([]byte(key))
	keyBytes := hasher.Sum(nil)

	// Convert message to bytes
	messageBytes := []byte(message)

	// Encrypt the message with a simple XOR cipher
	encryptedBytes := make([]byte, len(messageBytes))
	for i := range messageBytes {
		keyIndex := i % len(keyBytes)
		encryptedBytes[i] = messageBytes[i] ^ keyBytes[keyIndex]
	}

	// Encode with base64 using URL encoding to ensure compatibility with Minecraft
	// URL encoding uses only alphanumeric characters plus some symbols
	encoded := base64.URLEncoding.EncodeToString(encryptedBytes)

	return encoded, nil
}
