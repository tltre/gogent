package mgmt

import (
	"encoding/json"
	"fmt"
	"os"
)

type PortFile struct {
	PID  int    `json:"pid"`
	Port string `json:"port"`
	Name string `json:"name"`
}

const portFileName = ".gogent.pid"

func WritePortFile(port, name string) error {
	f, err := os.Create(portFileName)
	if err != nil {
		return fmt.Errorf("write port file: %w", err)
	}
	defer f.Close()

	pf := PortFile{
		PID:  os.Getpid(),
		Port: port,
		Name: name,
	}
	return json.NewEncoder(f).Encode(&pf)
}

func ReadPortFile() (*PortFile, error) {
	data, err := os.ReadFile(portFileName)
	if err != nil {
		return nil, fmt.Errorf("no agent running (no %s found)", portFileName)
	}

	var pf PortFile
	if err := json.Unmarshal(data, &pf); err != nil {
		return nil, fmt.Errorf("corrupt port file: %w", err)
	}
	return &pf, nil
}

func PortFromFile() (string, error) {
	pf, err := ReadPortFile()
	if err != nil {
		return "", err
	}
	return pf.Port, nil
}

func RemovePortFile() {
	os.Remove(portFileName)
}
