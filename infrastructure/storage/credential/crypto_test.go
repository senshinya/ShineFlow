package credential

import (
	"crypto/rand"
	"testing"
)

func TestEncryptDecrypt_RoundTrip(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil { t.Fatal(err) }

	plain := []byte(`{"key":"hello-world"}`)
	cipher, err := Encrypt(key, plain)
	if err != nil { t.Fatalf("encrypt: %v", err) }
	if string(cipher) == string(plain) { t.Fatal("cipher should differ from plain") }

	got, err := Decrypt(key, cipher)
	if err != nil { t.Fatalf("decrypt: %v", err) }
	if string(got) != string(plain) {
		t.Fatalf("roundtrip mismatch: got %s, want %s", got, plain)
	}
}

func TestEncrypt_NonceRandomness(t *testing.T) {
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	plain := []byte("abc")

	c1, _ := Encrypt(key, plain)
	c2, _ := Encrypt(key, plain)
	if string(c1) == string(c2) {
		t.Fatal("two encryptions of same plaintext should produce different ciphertexts (random nonce)")
	}
}

func TestDecrypt_TamperedFails(t *testing.T) {
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	cipher, _ := Encrypt(key, []byte("hello"))
	cipher[len(cipher)-1] ^= 0xFF

	if _, err := Decrypt(key, cipher); err == nil {
		t.Fatal("expected decrypt to fail on tampered ciphertext")
	}
}

func TestDecrypt_TooShortFails(t *testing.T) {
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	if _, err := Decrypt(key, []byte("xx")); err == nil {
		t.Fatal("expected error on too-short ciphertext")
	}
}
