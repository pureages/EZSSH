package store

import (
	"database/sql"
	"encoding/json"
)

// ListQuickAccess 返回某主机的文件管理器快速访问路径列表（不含默认根目录，
// 根目录由前端始终展示）。从未保存过时返回空列表。
func (s *Store) ListQuickAccess(hostID string) ([]string, error) {
	var raw string
	err := s.db.QueryRow(`SELECT paths FROM quick_access WHERE host_id=?`, hostID).Scan(&raw)
	if err == sql.ErrNoRows {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	var paths []string
	if err := json.Unmarshal([]byte(raw), &paths); err != nil {
		return []string{}, nil
	}
	if paths == nil {
		paths = []string{}
	}
	return paths, nil
}

// SaveQuickAccess 覆盖保存某主机的快速访问路径列表。
func (s *Store) SaveQuickAccess(hostID string, paths []string) error {
	b, err := json.Marshal(paths)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		`INSERT INTO quick_access (host_id, paths, updated_at) VALUES (?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(host_id) DO UPDATE SET paths=excluded.paths, updated_at=CURRENT_TIMESTAMP`,
		hostID, string(b),
	)
	return err
}
