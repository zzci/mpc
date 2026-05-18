package coorddb

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"

	"golang.org/x/crypto/argon2"
)

// kdfParams 是把运营方口令派生为整库加密密钥的 Argon2id 参数（database.md §7）。
//
// salt 非机密：Argon2id 的 salt 仅需唯一以抗彩虹表，无需保密；故随库持久化于
// 旁文件 <db>.kdf（明文 JSON），使重启后能由同一口令稳定重派生同一密钥。
// **口令本身绝不写入此文件、不入配置/env**——丢失即库不可恢复（设计如此）。
type kdfParams struct {
	Version   int    `json:"version"`
	Salt      string `json:"salt"` // base64(raw 16B)，非机密
	TimeCost  uint32 `json:"time"`
	MemoryKiB uint32 `json:"memory_kib"`
	Threads   uint8  `json:"threads"`
	KeyLen    uint32 `json:"key_len"`
}

// defaultKDFParams 选取高内存 Argon2id 参数（64 MiB / t=3 / p=4 / 32B key）。
func defaultKDFParams() kdfParams {
	return kdfParams{
		Version:   1,
		TimeCost:  3,
		MemoryKiB: 64 * 1024,
		Threads:   4,
		KeyLen:    32,
	}
}

func kdfSidecarPath(dbPath string) string { return dbPath + ".kdf" }

// loadOrInitKDF 读取库旁的 KDF 旁文件；不存在则用默认参数 + 新随机 salt 初始化并
// 落盘（0600）。salt 非机密，落盘安全；口令不参与本文件。
func loadOrInitKDF(dbPath string) (kdfParams, error) {
	path := kdfSidecarPath(dbPath)
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		var p kdfParams
		if err := json.Unmarshal(data, &p); err != nil {
			return kdfParams{}, fmt.Errorf("coorddb: parse kdf sidecar %s: %w", path, err)
		}
		if _, err := base64.StdEncoding.DecodeString(p.Salt); err != nil {
			return kdfParams{}, fmt.Errorf("coorddb: invalid kdf salt: %w", err)
		}
		return p, nil
	case os.IsNotExist(err):
		p := defaultKDFParams()
		salt := make([]byte, 16)
		if _, err := rand.Read(salt); err != nil {
			return kdfParams{}, fmt.Errorf("coorddb: gen kdf salt: %w", err)
		}
		p.Salt = base64.StdEncoding.EncodeToString(salt)
		buf, err := json.Marshal(p)
		if err != nil {
			return kdfParams{}, fmt.Errorf("coorddb: marshal kdf sidecar: %w", err)
		}
		if err := os.WriteFile(path, buf, 0o600); err != nil {
			return kdfParams{}, fmt.Errorf("coorddb: write kdf sidecar %s: %w", path, err)
		}
		return p, nil
	default:
		return kdfParams{}, fmt.Errorf("coorddb: read kdf sidecar %s: %w", path, err)
	}
}

// deriveKey 以 Argon2id 把口令派生为 raw 加密密钥；返回的密钥仅供内存使用，
// 调用方用毕须 zeroize。
func deriveKey(passphrase []byte, p kdfParams) ([]byte, error) {
	salt, err := base64.StdEncoding.DecodeString(p.Salt)
	if err != nil {
		return nil, fmt.Errorf("coorddb: decode kdf salt: %w", err)
	}
	return argon2.IDKey(passphrase, salt, p.TimeCost, p.MemoryKiB, p.Threads, p.KeyLen), nil
}

// zeroize 清零敏感字节切片（口令副本 / 派生密钥），降低内存残留窗口。
func zeroize(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
