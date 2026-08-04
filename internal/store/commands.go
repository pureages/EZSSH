package store

import "database/sql"

// SavedCommand 对应 saved_commands 表（「一键命令」保存的终端命令）。
type SavedCommand struct {
	ID        int64
	Name      string
	Command   string
	CreatedAt string
	UpdatedAt string
}

func scanCommand(row interface{ Scan(...any) error }) (*SavedCommand, error) {
	c := &SavedCommand{}
	err := row.Scan(&c.ID, &c.Name, &c.Command, &c.CreatedAt, &c.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return c, nil
}

const commandCols = `id, name, command, created_at, updated_at`

func (s *Store) ListCommands() ([]SavedCommand, error) {
	rows, err := s.db.Query(`SELECT ` + commandCols + ` FROM saved_commands ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []SavedCommand
	for rows.Next() {
		c, err := scanCommand(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *c)
	}
	return list, rows.Err()
}

func (s *Store) GetCommand(id int64) (*SavedCommand, error) {
	row := s.db.QueryRow(`SELECT `+commandCols+` FROM saved_commands WHERE id = ?`, id)
	return scanCommand(row)
}

// CreateCommand 插入命令并回读（含默认时间戳）。name 冲突返回 SQLite UNIQUE 错误。
func (s *Store) CreateCommand(name, command string) (*SavedCommand, error) {
	res, err := s.db.Exec(
		`INSERT INTO saved_commands (name, command) VALUES (?,?)`,
		name, command,
	)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return s.GetCommand(id)
}

// UpdateCommand 更新名称与命令并回读；name 冲突返回 SQLite UNIQUE 错误。
func (s *Store) UpdateCommand(id int64, name, command string) (*SavedCommand, error) {
	_, err := s.db.Exec(
		`UPDATE saved_commands SET name=?, command=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		name, command, id,
	)
	if err != nil {
		return nil, err
	}
	return s.GetCommand(id)
}

func (s *Store) DeleteCommand(id int64) error {
	res, err := s.db.Exec(`DELETE FROM saved_commands WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return ErrNotFound
	}
	return nil
}
