package auth

import "testing"

func TestPasswordHashRoundTrip(t *testing.T) {
	hash, parameters, err := HashPassword("a sufficiently strong password")
	if err != nil {
		t.Fatal(err)
	}
	if len(parameters) == 0 {
		t.Fatal("password parameters were not returned")
	}
	valid, err := VerifyPassword("a sufficiently strong password", hash)
	if err != nil || !valid {
		t.Fatalf("valid password result = %v, err=%v", valid, err)
	}
	valid, err = VerifyPassword("a different password", hash)
	if err != nil || valid {
		t.Fatalf("wrong password result = %v, err=%v", valid, err)
	}
}

func TestPasswordPolicyAndMalformedHashFailClosed(t *testing.T) {
	for _, password := range []string{"short", ""} {
		if _, _, err := HashPassword(password); err == nil {
			t.Fatalf("weak password %q was accepted", password)
		}
	}
	for _, encoded := range []string{"", "argon2id$v=19$m=1,t=1,p=1$bad$bad", "argon2i$v=19$m=65536,t=3,p=1$bad$bad"} {
		if _, err := VerifyPassword("anything", encoded); err == nil {
			t.Fatalf("malformed hash %q was accepted", encoded)
		}
	}
}
