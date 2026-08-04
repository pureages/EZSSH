package store

import "database/sql"

// Website 对应 websites 表（网站管理：静态 / 反向代理 / 重定向）。
type Website struct {
	ID          string
	HostID      string
	Name        string
	GroupName   string
	Domains     string
	SiteType    string // static | proxy | redirect
	RootDir     string
	ProxyPass   string
	RedirectURL string
	SSL         bool
	CertID      string
	Enabled     bool
	CreatedAt   string
	UpdatedAt   string
}

// PrimaryDomain 返回第 1 个（主）域名。
func (w *Website) PrimaryDomain() string {
	for _, d := range splitDomains(w.Domains) {
		return d
	}
	return ""
}

// AllDomains 返回去空格后的域名列表。
func (w *Website) AllDomains() []string {
	return splitDomains(w.Domains)
}

func splitDomains(s string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			d := trimSpace(s[start:i])
			if d != "" {
				out = append(out, d)
			}
			start = i + 1
		}
	}
	return out
}

// trimSpace 等价 strings.TrimSpace（避免在本文件引入 strings 而失焦）。
func trimSpace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t' || s[0] == '\n' || s[0] == '\r') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t' || s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}

func scanWebsite(row interface{ Scan(...any) error }) (*Website, error) {
	w := &Website{}
	err := row.Scan(
		&w.ID, &w.HostID, &w.Name, &w.GroupName, &w.Domains, &w.SiteType,
		&w.RootDir, &w.ProxyPass, &w.RedirectURL, &w.SSL, &w.CertID,
		&w.Enabled, &w.CreatedAt, &w.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return w, nil
}

const websiteCols = `id, host_id, name, group_name, domains, site_type, root_dir, proxy_pass, redirect_url, ssl, cert_id, enabled, created_at, updated_at`

func (s *Store) CreateWebsite(w *Website) error {
	_, err := s.db.Exec(
		`INSERT INTO websites (`+websiteCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`,
		w.ID, w.HostID, w.Name, w.GroupName, w.Domains, w.SiteType,
		w.RootDir, w.ProxyPass, w.RedirectURL, boolToInt(w.SSL), w.CertID, boolToInt(w.Enabled),
	)
	return err
}

func (s *Store) GetWebsite(id string) (*Website, error) {
	row := s.db.QueryRow(`SELECT `+websiteCols+` FROM websites WHERE id = ?`, id)
	return scanWebsite(row)
}

// ListWebsites 列出站点；hostID 非空按服务器过滤，group 非空按分组过滤。
func (s *Store) ListWebsites(hostID, group string) ([]*Website, error) {
	query := `SELECT ` + websiteCols + ` FROM websites`
	var args []any
	if hostID != "" {
		query += ` WHERE host_id = ?`
		args = append(args, hostID)
		if group != "" {
			query += ` AND group_name = ?`
			args = append(args, group)
		}
	} else if group != "" {
		query += ` WHERE group_name = ?`
		args = append(args, group)
	}
	query += ` ORDER BY name`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sites []*Website
	for rows.Next() {
		w, err := scanWebsite(rows)
		if err != nil {
			return nil, err
		}
		sites = append(sites, w)
	}
	return sites, rows.Err()
}

// ListWebsiteGroups 返回某服务器（hostID 非空）或全部站点去重后的分组列表。
func (s *Store) ListWebsiteGroups(hostID string) ([]string, error) {
	query := `SELECT DISTINCT group_name FROM websites`
	var args []any
	if hostID != "" {
		query += ` WHERE host_id = ?`
		args = append(args, hostID)
	}
	query += ` AND group_name != '' ORDER BY group_name`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groups []string
	for rows.Next() {
		var g string
		if err := rows.Scan(&g); err != nil {
			return nil, err
		}
		groups = append(groups, g)
	}
	return groups, rows.Err()
}

func (s *Store) UpdateWebsite(w *Website) error {
	_, err := s.db.Exec(
		`UPDATE websites SET name=?, group_name=?, domains=?, site_type=?, root_dir=?, proxy_pass=?, redirect_url=?, ssl=?, enabled=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		w.Name, w.GroupName, w.Domains, w.SiteType, w.RootDir, w.ProxyPass,
		w.RedirectURL, boolToInt(w.SSL), boolToInt(w.Enabled), w.ID,
	)
	return err
}

func (s *Store) DeleteWebsite(id string) error {
	_, err := s.db.Exec(`DELETE FROM websites WHERE id=?`, id)
	return err
}

// UpdateWebsiteCertID 记录某站点关联的证书记录 id。
func (s *Store) UpdateWebsiteCertID(id, certID string) error {
	_, err := s.db.Exec(`UPDATE websites SET cert_id=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`, certID, id)
	return err
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
