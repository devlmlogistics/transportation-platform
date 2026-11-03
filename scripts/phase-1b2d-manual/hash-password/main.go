// Command hash_password is a local-only fixture helper. It reads a password
// from PHASE_1B2D_PASSWORD and prints only Argon2id material as JSON.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	applicationauth "transportation-platform/internal/application/auth"
)

func main() {
	password := os.Getenv("PHASE_1B2D_PASSWORD")
	if password == "" {
		fmt.Fprintln(os.Stderr, "PHASE_1B2D_PASSWORD is required")
		os.Exit(2)
	}
	hash, parameters, err := applicationauth.HashPassword(password)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	result := struct {
		Hash       string `json:"hash"`
		Parameters string `json:"parameters"`
	}{Hash: hash, Parameters: string(parameters)}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
