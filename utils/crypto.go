package utils

// EncryptSecret wraps a secret (e.g. a Gmail token) with AES-256-GCM using the shared encryption key.
func EncryptSecret(cfg AppConfig, plain []byte) (string, error) {
	return encrypt(cfg.EncryptionKey, plain)
}

// DecryptSecret reverses EncryptSecret.
func DecryptSecret(cfg AppConfig, encoded string) ([]byte, error) {
	return decrypt(cfg.EncryptionKey, encoded)
}
