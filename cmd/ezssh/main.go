package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"

	"ezssh/internal/api"
	"ezssh/internal/auth"
	"ezssh/internal/sshhub"
	"ezssh/internal/store"
	"ezssh/internal/vault"
)

// version 为 EZSSH 网关版本号，可通过构建参数覆盖：
//
//	go build -ldflags "-X main.version=x.y.z"
var version = "0.0.4"

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	listen := getenv("EZSSH_LISTEN", "127.0.0.1")
	port := getenv("EZSSH_PORT", "49466")
	dir := dataDir()
	// 数据库路径：EZSSH_DB 显式指定则用之，否则默认落在数据目录下
	dbPath := getenv("EZSSH_DB", defaultDBPath(dir))

	// 确保数据库所在目录存在；一次性迁移旧版根目录 ezssh.db 到数据目录
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		log.Fatalf("create data dir: %v", err)
	}
	if migrated, err := migrateLegacyDB(dbPath); err != nil {
		log.Printf("warn: migrate legacy db: %v", err)
	} else if migrated {
		log.Printf("legacy ezssh.db migrated to %s (old files kept)", dbPath)
	}
	log.Printf("database: %s", dbPath)

	st, err := store.Open(dbPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer st.Close()

	// 播种内置网关主机（网关本机，127.0.0.1:22，账号密码留空待编辑填写）。
	// 用户删除过则跳过播种；放在生产入口而非 store.Open，避免影响测试的主机数量断言。
	if err := st.EnsureBuiltinHost(); err != nil {
		log.Printf("warn: seed builtin gateway host: %v", err)
	}

	v := vault.New()
	if mk := os.Getenv("EZSSH_MASTER_KEY"); mk != "" {
		v.UnlockWithMasterKey(mk)
		log.Println("vault unlocked via EZSSH_MASTER_KEY")
	}

	am := auth.NewManager()
	hub := sshhub.New(st, v)
	defer hub.CloseAll()

	srv := api.New(st, v, am, hub)
	srv.Version = version
	defer srv.Close()

	addr := listen + ":" + port
	log.Printf("EZSSH v%s gateway listening on http://%s", version, addr)
	if err := http.ListenAndServe(addr, srv.Routes()); err != nil {
		log.Fatal(err)
	}
}
