package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"io"

	"golang.org/x/crypto/argon2"
)

// 参数（OWASP 推荐档位）
const (
	saltLen  = 16
	keyLen   = 32 // KEK / 口令校验哈希均为 32 字节
	memory   = 64 * 1024
	timeCost = 3
	threads  = 4
)

// derive 一次 Argon2id 派生 64 字节：前 32 为口令校验哈希，后 32 为 KEK。
func derive(password string, salt []byte) []byte {
	return argon2.IDKey([]byte(password), salt, timeCost, memory, threads, keyLen*2)
}

// Vault 持有内存中的 KEK，用于主机凭据的加解密。
type Vault struct {
	kek []byte
}

func New() *Vault {
	return &Vault{}
}

func NewSalt() ([]byte, error) {
	salt := make([]byte, saltLen)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, err
	}
	return salt, nil
}

// HashPassword 初始化时生成口令校验哈希（32 字节）。
func HashPassword(password string, salt []byte) []byte {
	return derive(password, salt)[:keyLen]
}

// Verify 仅校验口令，不解锁（用于 master-key 已解锁但登录口令仍需验证的场景）。
func Verify(password string, salt, storedHash []byte) bool {
	if len(storedHash) != keyLen {
		return false
	}
	return subtle.ConstantTimeCompare(derive(password, salt)[:keyLen], storedHash) == 1
}

func (v *Vault) IsUnlocked() bool {
	return len(v.kek) > 0
}

// Unlock 校验口令并派生 KEK 解锁。
func (v *Vault) Unlock(password string, salt, storedHash []byte) error {
	if len(storedHash) != keyLen {
		return errors.New("vault: bad stored hash")
	}
	out := derive(password, salt)
	if subtle.ConstantTimeCompare(out[:keyLen], storedHash) != 1 {
		return errors.New("vault: invalid password")
	}
	v.kek = append([]byte(nil), out[keyLen:]...)
	return nil
}

// UnlockWithMasterKey 使用环境变量注入的密钥解锁（无人值守启动）。
func (v *Vault) UnlockWithMasterKey(masterKey string) {
	sum := sha256.Sum256([]byte(masterKey))
	v.kek = sum[:]
}

// SetKey 直接设置 KEK（改密码重加密后使用）。
func (v *Vault) SetKey(kek []byte) {
	v.kek = append([]byte(nil), kek...)
}

// GetKey 返回当前 KEK 副本（改密码重加密时读取旧 KEK 用）。
func (v *Vault) GetKey() []byte {
	if len(v.kek) == 0 {
		return nil
	}
	return append([]byte(nil), v.kek...)
}

// DeriveKey 由口令与盐派生 KEK（改密码时用于生成新 KEK）。
func DeriveKey(password string, salt []byte) []byte {
	return append([]byte(nil), derive(password, salt)[keyLen:]...)
}

// EncryptWith 使用指定 KEK 加密（改密码重加密时使用）。
func EncryptWith(kek, plain []byte) ([]byte, error) {
	block, err := aes.NewCipher(kek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plain, nil), nil
}

// DecryptWith 使用指定 KEK 解密（改密码重加密时使用）。
func DecryptWith(kek, data []byte) ([]byte, error) {
	block, err := aes.NewCipher(kek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(data) < gcm.NonceSize() {
		return nil, errors.New("vault: bad ciphertext")
	}
	return gcm.Open(nil, data[:gcm.NonceSize()], data[gcm.NonceSize():], nil)
}

func (v *Vault) Lock() {
	v.kek = nil
}

// Encrypt 返回 nonce + ciphertext 的密文。
func (v *Vault) Encrypt(plain []byte) ([]byte, error) {
	if len(v.kek) == 0 {
		return nil, errors.New("vault: locked")
	}
	block, err := aes.NewCipher(v.kek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plain, nil), nil
}

func (v *Vault) Decrypt(data []byte) ([]byte, error) {
	if len(v.kek) == 0 {
		return nil, errors.New("vault: locked")
	}
	block, err := aes.NewCipher(v.kek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(data) < gcm.NonceSize() {
		return nil, errors.New("vault: bad ciphertext")
	}
	return gcm.Open(nil, data[:gcm.NonceSize()], data[gcm.NonceSize():], nil)
}
