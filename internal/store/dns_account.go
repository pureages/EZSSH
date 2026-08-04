package store

import "database/sql"

// DnsAccount 对应 dns_accounts 表（DNS 验证账户，如 Cloudflare API Token）。
// TokenEnc 为 vault 加密后的密文，永不返回前端。
type DnsAccount struct {
	ID        string
	Name      string
	Provider  string // cloudflare
	TokenEnc  []byte
	CreatedAt string
}

func scanDnsAccount(row interface{ Scan(...any) error }) (*DnsAccount, error) {
	a := &DnsAccount{}
	err := row.Scan(&a.ID, &a.Name, &a.Provider, &a.TokenEnc, &a.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return a, nil
}

const dnsAccountCols = `id, name, provider, token_enc, created_at`

func (s *Store) CreateDnsAccount(a *DnsAccount) error {
	_, err := s.db.Exec(
		`INSERT INTO dns_accounts (`+dnsAccountCols+`) VALUES (?,?,?,?,CURRENT_TIMESTAMP)`,
		a.ID, a.Name, a.Provider, a.TokenEnc,
	)
	return err
}

func (s *Store) GetDnsAccount(id string) (*DnsAccount, error) {
	row := s.db.QueryRow(`SELECT `+dnsAccountCols+` FROM dns_accounts WHERE id = ?`, id)
	return scanDnsAccount(row)
}

func (s *Store) ListDnsAccounts() ([]*DnsAccount, error) {
	rows, err := s.db.Query(`SELECT ` + dnsAccountCols + ` FROM dns_accounts ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []*DnsAccount
	for rows.Next() {
		a, err := scanDnsAccount(rows)
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, a)
	}
	return accounts, rows.Err()
}

// UpdateDnsAccount 更新除 Token 外的字段；新 Token 需单独调用 UpdateDnsAccountToken。
func (s *Store) UpdateDnsAccount(a *DnsAccount) error {
	_, err := s.db.Exec(
		`UPDATE dns_accounts SET name=?, provider=? WHERE id=?`,
		a.Name, a.Provider, a.ID,
	)
	return err
}

func (s *Store) UpdateDnsAccountToken(id string, tokenEnc []byte) error {
	_, err := s.db.Exec(`UPDATE dns_accounts SET token_enc=? WHERE id=?`, tokenEnc, id)
	return err
}

func (s *Store) DeleteDnsAccount(id string) error {
	_, err := s.db.Exec(`DELETE FROM dns_accounts WHERE id=?`, id)
	return err
}
