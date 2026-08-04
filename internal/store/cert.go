package store

import "database/sql"

// Certificate 对应 certificates 表（Let's Encrypt 证书记录）。
type Certificate struct {
	ID           string
	HostID       string
	Domain       string
	Method       string // http | dns
	DNSAccountID string // method=dns 时对应 dns_accounts.id
	Email        string
	Status       string // issuing | active | renewing | error
	ExpiresAt    string // 'YYYY-MM-DD HH:MM:SS'（SQLite 时间）；空表示未知
	LastRenew    string
	Error        string
	CreatedAt    string
}

func scanCert(row interface{ Scan(...any) error }) (*Certificate, error) {
	c := &Certificate{}
	err := row.Scan(
		&c.ID, &c.HostID, &c.Domain, &c.Method, &c.DNSAccountID, &c.Email,
		&c.Status, &c.ExpiresAt, &c.LastRenew, &c.Error, &c.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return c, nil
}

const certCols = `id, host_id, domain, method, dns_account_id, email, status, expires_at, last_renew, error, created_at`

func (s *Store) CreateCertificate(c *Certificate) error {
	_, err := s.db.Exec(
		`INSERT INTO certificates (`+certCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,CURRENT_TIMESTAMP)`,
		c.ID, c.HostID, c.Domain, c.Method, c.DNSAccountID, c.Email,
		c.Status, c.ExpiresAt, c.LastRenew, c.Error,
	)
	return err
}

func (s *Store) GetCertificate(id string) (*Certificate, error) {
	row := s.db.QueryRow(`SELECT `+certCols+` FROM certificates WHERE id = ?`, id)
	return scanCert(row)
}

func (s *Store) ListCertificates(hostID string) ([]*Certificate, error) {
	query := `SELECT ` + certCols + ` FROM certificates`
	var args []any
	if hostID != "" {
		query += ` WHERE host_id = ?`
		args = append(args, hostID)
	}
	query += ` ORDER BY created_at DESC`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var certs []*Certificate
	for rows.Next() {
		c, err := scanCert(rows)
		if err != nil {
			return nil, err
		}
		certs = append(certs, c)
	}
	return certs, rows.Err()
}

// UpdateCertificateState 更新签发/续签后的状态、到期与错误信息。
func (s *Store) UpdateCertificateState(id, status, expiresAt, lastRenew, errMsg string) error {
	_, err := s.db.Exec(
		`UPDATE certificates SET status=?, expires_at=?, last_renew=?, error=? WHERE id=?`,
		status, expiresAt, lastRenew, errMsg, id,
	)
	return err
}

func (s *Store) DeleteCertificate(id string) error {
	_, err := s.db.Exec(`DELETE FROM certificates WHERE id=?`, id)
	return err
}

// ListActiveCertificates 返回非 error 状态的证书记录（供自动续签遍历）。
func (s *Store) ListActiveCertificates() ([]*Certificate, error) {
	rows, err := s.db.Query(`SELECT `+certCols+` FROM certificates WHERE status != 'error'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var certs []*Certificate
	for rows.Next() {
		c, err := scanCert(rows)
		if err != nil {
			return nil, err
		}
		certs = append(certs, c)
	}
	return certs, rows.Err()
}
