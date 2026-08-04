package vault

import (
	"bytes"
	"testing"
)

func TestVaultRoundTrip(t *testing.T) {
	v := New()
	if v.IsUnlocked() {
		t.Fatal("new vault should be locked")
	}

	salt, err := NewSalt()
	if err != nil {
		t.Fatal(err)
	}
	hash := HashPassword("correct-horse-123", salt)

	// 错误口令解锁失败
	if err := v.Unlock("wrong-password", salt, hash); err == nil {
		t.Fatal("unlock with wrong password should fail")
	}
	if v.IsUnlocked() {
		t.Fatal("vault should stay locked")
	}

	// 正确口令解锁成功
	if err := v.Unlock("correct-horse-123", salt, hash); err != nil {
		t.Fatal(err)
	}
	if !v.IsUnlocked() {
		t.Fatal("vault should be unlocked")
	}

	// 加密解密往返
	plain := []byte("S3cret-passw0rd\nsecond line")
	ct, err := v.Encrypt(plain)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(ct, plain) {
		t.Fatal("ciphertext should not contain plaintext")
	}
	back, err := v.Decrypt(ct)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(back, plain) {
		t.Fatalf("round trip mismatch: %q != %q", back, plain)
	}

	// 锁定后无法解密
	v.Lock()
	if _, err := v.Decrypt(ct); err == nil {
		t.Fatal("decrypt after lock should fail")
	}

	// 篡改密文应解密失败
	if err := v.Unlock("correct-horse-123", salt, hash); err != nil {
		t.Fatal(err)
	}
	tampered := append([]byte{}, ct...)
	tampered[len(tampered)-1] ^= 0xFF
	if _, err := v.Decrypt(tampered); err == nil {
		t.Fatal("decrypt of tampered ciphertext should fail")
	}
}

func TestMasterKeyUnlock(t *testing.T) {
	v := New()
	v.UnlockWithMasterKey("env-injected-key")
	if !v.IsUnlocked() {
		t.Fatal("master key unlock failed")
	}
	ct, err := v.Encrypt([]byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	back, err := v.Decrypt(ct)
	if err != nil || string(back) != "secret" {
		t.Fatalf("master key round trip failed: %q %v", back, err)
	}

	// 不同的 master key 不能解出相同数据
	v2 := New()
	v2.UnlockWithMasterKey("different-key")
	if _, err := v2.Decrypt(ct); err == nil {
		t.Fatal("different key should fail to decrypt")
	}
}

func TestVerify(t *testing.T) {
	salt, _ := NewSalt()
	hash := HashPassword("pass-123", salt)
	if !Verify("pass-123", salt, hash) {
		t.Fatal("verify correct password failed")
	}
	if Verify("pass-456", salt, hash) {
		t.Fatal("verify wrong password should fail")
	}
}
