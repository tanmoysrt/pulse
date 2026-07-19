package main

import (
	"encoding/json"
	"os"
	"path/filepath"

	"golang.org/x/crypto/bcrypt"
)

// setupRecord stores reusable daemon choices. The password is bcrypt-hashed, so
// restarting Pulse never requires retaining the plaintext credential.
type setupRecord struct {
	Tunnel       bool   `json:"tunnel"`
	Port         int    `json:"port"`
	Notify       bool   `json:"notify"`
	PasswordHash string `json:"passwordHash,omitempty"`
}

func hashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(hash), err
}

func setupPath() string { return filepath.Join(filepath.Dir(statePath()), "setup.json") }

func readSetup() (*setupRecord, error) {
	b, err := os.ReadFile(setupPath())
	if err != nil {
		return nil, err
	}
	var setup setupRecord
	if err := json.Unmarshal(b, &setup); err != nil {
		return nil, err
	}
	if setup.Port < 1 || setup.Port > 65535 {
		return nil, os.ErrInvalid
	}
	return &setup, nil
}

func writeSetup(setup setupRecord) {
	b, _ := json.Marshal(setup)
	os.MkdirAll(filepath.Dir(setupPath()), 0o700)
	os.WriteFile(setupPath(), b, 0o600)
}
