package store

// Audit 对应 audit_logs 表，记录本地轻量审计。
type Audit struct {
	ID        int64
	Action    string
	HostID    string
	Detail    string
	CreatedAt string
}

func (s *Store) AddAudit(action, hostID, detail string) error {
	_, err := s.db.Exec(
		`INSERT INTO audit_logs (action, host_id, detail) VALUES (?, ?, ?)`,
		action, hostID, detail,
	)
	return err
}

func (s *Store) ListAudit(limit int) ([]*Audit, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	rows, err := s.db.Query(
		`SELECT id, action, COALESCE(host_id,''), COALESCE(detail,''), created_at FROM audit_logs ORDER BY id DESC LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []*Audit
	for rows.Next() {
		a := &Audit{}
		if err := rows.Scan(&a.ID, &a.Action, &a.HostID, &a.Detail, &a.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, a)
	}
	return list, rows.Err()
}
