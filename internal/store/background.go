package store

import "database/sql"

// BackgroundTask 对应 background_tasks 表：一条在远端服务器上长期后台运行的命令。
// Started 为网关侧发起时刻的 unix 秒（远端进程的启动时间不可靠/不可达时仍有意义）。
type BackgroundTask struct {
	ID       string
	HostID   string
	HostName string
	PID      int
	Command  string
	LogPath  string
	ErrPath  string
	Started  int64
}

func scanTask(row interface{ Scan(...any) error }) (*BackgroundTask, error) {
	t := &BackgroundTask{}
	err := row.Scan(&t.ID, &t.HostID, &t.HostName, &t.PID, &t.Command, &t.LogPath, &t.ErrPath, &t.Started)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return t, nil
}

const taskCols = `id, host_id, host_name, pid, command, log_path, err_path, started`

func (s *Store) CreateTask(t *BackgroundTask) error {
	_, err := s.db.Exec(
		`INSERT INTO background_tasks (`+taskCols+`) VALUES (?,?,?,?,?,?,?,?)`,
		t.ID, t.HostID, t.HostName, t.PID, t.Command, t.LogPath, t.ErrPath, t.Started,
	)
	return err
}

func (s *Store) ListTasks() ([]BackgroundTask, error) {
	rows, err := s.db.Query(`SELECT ` + taskCols + ` FROM background_tasks ORDER BY started DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []BackgroundTask
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *t)
	}
	return list, rows.Err()
}

func (s *Store) GetTask(id string) (*BackgroundTask, error) {
	row := s.db.QueryRow(`SELECT `+taskCols+` FROM background_tasks WHERE id = ?`, id)
	return scanTask(row)
}

// DeleteTask 删除任务记录（保留给未来清理；UI 停止操作不删行，进程消失后列表显示「已退出」）。
func (s *Store) DeleteTask(id string) error {
	res, err := s.db.Exec(`DELETE FROM background_tasks WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteTasksOlderThan 删除 started 早于 cutoff（unix 秒）的全部任务，返回删除条数。
func (s *Store) DeleteTasksOlderThan(cutoff int64) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM background_tasks WHERE started < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
