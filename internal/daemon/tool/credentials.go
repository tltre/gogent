package tool

import (
	"github.com/tltre/gogent/internal/credentials"
)

func DefaultCredentialsPath() string {
	return credentials.DefaultCredentialsPath()
}

func LoadCredentials(path string) (map[string]string, error) {
	return credentials.LoadCredentials(path)
}

func WriteCredentials(path string, entries map[string]string) error {
	return credentials.WriteCredentials(path, entries)
}

func DeleteCredentials(path, key string) error {
	return credentials.DeleteCredentials(path, key)
}

func RemoveCredentialsGroup(path, toolName string) error {
	return credentials.RemoveCredentialsGroup(path, toolName)
}
