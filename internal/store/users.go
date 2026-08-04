package store

import "database/sql"

// User 对应 users 表（单用户，M1 只允许一行，但结构支持扩展）。
type User struct {
	ID           int64
	Username     string
	PasswordHash []byte
	Salt         []byte
	CreatedAt    string
}

func (s *Store) CreateUser(username string, passwordHash, salt []byte) error {
	_, err := s.db.Exec(
		`INSERT INTO users (username, password_hash, salt) VALUES (?, ?, ?)`,
		username, passwordHash, salt,
	)
	return err
}

func (s *Store) GetUser(username string) (*User, error) {
	u := &User{}
	err := s.db.QueryRow(
		`SELECT id, username, password_hash, salt, created_at FROM users WHERE username = ?`,
		username,
	).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Salt, &u.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return u, nil
}

// UpdateUserPassword 更新口令哈希与盐（改密码用）。
func (s *Store) UpdateUserPassword(username string, passwordHash, salt []byte) error {
	_, err := s.db.Exec(
		`UPDATE users SET password_hash=?, salt=? WHERE username=?`,
		passwordHash, salt, username,
	)
	return err
}

// CountUsers 用于判断是否已完成首次初始化。
func (s *Store) CountUsers() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

// ListUsers 返回全部用户名（单用户场景一般只有一条）。
func (s *Store) ListUsers() ([]string, error) {
	rows, err := s.db.Query(`SELECT username FROM users ORDER BY id LIMIT 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []string
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}
