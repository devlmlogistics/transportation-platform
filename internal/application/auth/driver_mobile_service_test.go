package auth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"testing"

	"github.com/google/uuid"
)

func TestVerifyDriverPossessionUsesInstallationAndRejectsMalformedInputs(t *testing.T) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	installationID := uuid.New()
	challengeHash := sha256.Sum256([]byte("one-time-driver-challenge"))
	message := append(append([]byte("tp-driver-possession\x00"), challengeHash[:]...), installationID[:]...)
	digest := sha256.Sum256(message)
	r, s, err := ecdsa.Sign(rand.Reader, privateKey, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	signature := make([]byte, 64)
	r.FillBytes(signature[:32])
	s.FillBytes(signature[32:])
	publicKey := elliptic.Marshal(elliptic.P256(), privateKey.PublicKey.X, privateKey.PublicKey.Y)

	if !verifyDriverPossession(publicKey, "ECDSA_P256_SHA256", challengeHash[:], installationID, signature) {
		t.Fatal("valid installation proof was rejected")
	}
	if verifyDriverPossession(publicKey, "ECDSA_P256_SHA256", challengeHash[:], uuid.New(), signature) {
		t.Fatal("proof for a different installation was accepted")
	}
	if verifyDriverPossession(publicKey, "ECDSA_P256_SHA256", challengeHash[:], installationID, append(signature, 0)) {
		t.Fatal("malformed signature was accepted")
	}
	if verifyDriverPossession(publicKey, "ECDSA_P256_SHA256", challengeHash[:], installationID, make([]byte, 64)) {
		t.Fatal("zero signature was accepted")
	}
}
