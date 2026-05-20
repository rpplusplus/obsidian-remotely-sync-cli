package crypto

import (
	"bytes"
	"strings"
	"testing"
)

func TestEncryptDecryptRoundtrip(t *testing.T) {
	password := "test-password-123"
	testCases := []struct {
		name string
		data []byte
	}{
		{"empty", []byte{}},
		{"short", []byte("hello")},
		{"exact block", bytes.Repeat([]byte("A"), 16)},
		{"multi block", bytes.Repeat([]byte("B"), 100)},
		{"unicode", []byte("你好世界 🔑")},
		{"binary", []byte{0, 1, 2, 255, 254, 253}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			encrypted, err := Encrypt(tc.data, password)
			if err != nil {
				t.Fatalf("Encrypt failed: %v", err)
			}

			// Verify "Salted__" prefix
			if !bytes.HasPrefix(encrypted, []byte("Salted__")) {
				t.Fatal("encrypted data missing 'Salted__' prefix")
			}

			decrypted, err := Decrypt(encrypted, password)
			if err != nil {
				t.Fatalf("Decrypt failed: %v", err)
			}

			if !bytes.Equal(decrypted, tc.data) {
				t.Fatalf("roundtrip failed: got %d bytes, want %d bytes", len(decrypted), len(tc.data))
			}
		})
	}
}

func TestEncryptProducesDifferentCiphertext(t *testing.T) {
	data := []byte("same plaintext")
	password := "same-password"

	enc1, err := Encrypt(data, password)
	if err != nil {
		t.Fatal(err)
	}
	enc2, err := Encrypt(data, password)
	if err != nil {
		t.Fatal(err)
	}

	if bytes.Equal(enc1, enc2) {
		t.Fatal("two encryptions of the same data produced identical ciphertext (salt not random?)")
	}

	// Both should decrypt to the same plaintext
	dec1, _ := Decrypt(enc1, password)
	dec2, _ := Decrypt(enc2, password)
	if !bytes.Equal(dec1, dec2) {
		t.Fatal("decrypted results differ")
	}
}

func TestDecryptWrongPassword(t *testing.T) {
	data := []byte("secret data")
	encrypted, err := Encrypt(data, "correct-password")
	if err != nil {
		t.Fatal(err)
	}

	_, err = Decrypt(encrypted, "wrong-password")
	if err == nil {
		t.Fatal("Decrypt should fail with wrong password")
	}
}

func TestDecryptCorruptData(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"too short", []byte("Salted__")},
		{"no magic", []byte("not-encrypted-data-that-is-long-enough-for-a-block")},
		{"corrupt ciphertext", func() []byte {
			enc, _ := Encrypt([]byte("test"), "pass")
			enc[20] ^= 0xFF // flip a bit in the ciphertext
			return enc
		}()},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Decrypt(tc.data, "any-password")
			if err == nil {
				t.Fatal("Decrypt should fail on corrupt data")
			}
		})
	}
}

func TestEncryptDecryptName(t *testing.T) {
	password := "name-password"

	names := []string{
		"simple.md",
		"folder/nested/file.md",
		"特殊字符.txt",
		"a",
		strings.Repeat("long-name-", 20) + ".md",
	}

	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			encName, err := EncryptName(name, password)
			if err != nil {
				t.Fatalf("EncryptName failed: %v", err)
			}

			// Should be valid base64url
			if encName == "" {
				t.Fatal("EncryptName returned empty string")
			}

			// Should not contain standard base64 padding
			if strings.Contains(encName, "=") {
				t.Fatal("encrypted name contains padding '='")
			}

			// Should be decryptable
			decName, err := DecryptName(encName, password)
			if err != nil {
				t.Fatalf("DecryptName failed: %v", err)
			}

			if decName != name {
				t.Fatalf("name roundtrip failed: got %q, want %q", decName, name)
			}
		})
	}
}

func TestIsLikelyEncrypted(t *testing.T) {
	password := "test"
	encName, _ := EncryptName("test.md", password)

	if !IsLikelyEncrypted(encName) {
		t.Fatalf("IsLikelyEncrypted should return true for %q", encName)
	}

	if IsLikelyEncrypted("plain-filename.md") {
		t.Fatal("IsLikelyEncrypted should return false for plain filename")
	}

	if IsLikelyEncrypted("") {
		t.Fatal("IsLikelyEncrypted should return false for empty string")
	}
}

func TestPKCS7Padding(t *testing.T) {
	for size := 1; size <= 32; size++ {
		data := bytes.Repeat([]byte("X"), size)
		padded := pkcs7Pad(data, 16)
		if len(padded)%16 != 0 {
			t.Fatalf("padded length %d not multiple of 16 for input size %d", len(padded), size)
		}
		unpadded, err := pkcs7Unpad(padded)
		if err != nil {
			t.Fatalf("pkcs7Unpad failed for input size %d: %v", size, err)
		}
		if !bytes.Equal(unpadded, data) {
			t.Fatalf("unpad mismatch for input size %d", size)
		}
	}
}
