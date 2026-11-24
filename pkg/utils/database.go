package utils

import (
	"crypto/rand"
	"math/big"
)

const (
	ProviderMySQL    = "mysql"
	ProviderPostgres = "postgres"
	ProviderMariaDB  = "mariadb"
	ProviderRedis    = "redis"
	ProviderValkey   = "valkey"
	ProviderMongoDB  = "mongodb"

	DefaultPasswordLength = 16
)

func GeneratePassword(length int) (string, error) {
	if length <= 0 {
		length = DefaultPasswordLength
	}

	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)

	for i := range b {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			return "", err
		}
		b[i] = charset[n.Int64()]
	}

	return string(b), nil
}
