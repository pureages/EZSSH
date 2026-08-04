package store

import "database/sql"

// BuiltinHostID 内置网关主机的固定 ID（网关本机，默认播种、可删除）。
const BuiltinHostID = "h_builtin_gateway"

// SettingBuiltinDeleted settings 键：用户删除内置网关主机后置 1，重启不再重新播种。
const SettingBuiltinDeleted = "builtin_host_deleted"

// Host 对应 hosts 表。Credential 为 vault 加密后的密文，永不返回前端。
type Host struct {
	ID          string
	Name        string
	Host        string
	Port        int
	Username    string
	AuthType    string // 'password' | 'privatekey'
	Credential  []byte
	GroupName   string
	Remark      string
	Fingerprint string // TOFU 主机密钥指纹
	Hidden      bool   // 桌面图标被隐藏
	Builtin     bool   // 内置主机（网关本机，默认播种、可删除）
	Platform    string // ''（未知/自动检测）| 'linux' | 'windows'
	CreatedAt   string
	UpdatedAt   string
}

func scanHost(row interface{ Scan(...any) error }) (*Host, error) {
	h := &Host{}
	err := row.Scan(
		&h.ID, &h.Name, &h.Host, &h.Port, &h.Username, &h.AuthType,
		&h.Credential, &h.GroupName, &h.Remark, &h.Fingerprint,
		&h.Hidden, &h.Builtin, &h.CreatedAt, &h.UpdatedAt, &h.Platform,
	)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return h, nil
}

const hostCols = `id, name, host, port, username, auth_type, credential, group_name, remark, fingerprint, hidden, builtin, created_at, updated_at, platform`

func (s *Store) CreateHost(h *Host) error {
	_, err := s.db.Exec(
		`INSERT INTO hosts (`+hostCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,?)`,
		h.ID, h.Name, h.Host, h.Port, h.Username, h.AuthType,
		h.Credential, h.GroupName, h.Remark, h.Fingerprint, h.Hidden, h.Builtin, h.Platform,
	)
	return err
}

func (s *Store) GetHost(id string) (*Host, error) {
	row := s.db.QueryRow(`SELECT `+hostCols+` FROM hosts WHERE id = ?`, id)
	return scanHost(row)
}

func (s *Store) ListHosts() ([]*Host, error) {
	rows, err := s.db.Query(`SELECT ` + hostCols + ` FROM hosts ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var hosts []*Host
	for rows.Next() {
		h, err := scanHost(rows)
		if err != nil {
			return nil, err
		}
		hosts = append(hosts, h)
	}
	return hosts, rows.Err()
}

// UpdateHost 更新除凭据外的字段；新凭据需单独调用 UpdateHostCredential。
func (s *Store) UpdateHost(h *Host) error {
	_, err := s.db.Exec(
		`UPDATE hosts SET name=?, host=?, port=?, username=?, auth_type=?, group_name=?, remark=?, platform=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		h.Name, h.Host, h.Port, h.Username, h.AuthType, h.GroupName, h.Remark, h.Platform, h.ID,
	)
	return err
}

// UpdateHostPlatform 仅更新平台字段（探测结果持久化用）。
func (s *Store) UpdateHostPlatform(id, platform string) error {
	_, err := s.db.Exec(
		`UPDATE hosts SET platform=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		platform, id,
	)
	return err
}

func (s *Store) UpdateHostCredential(id string, credential []byte) error {
	_, err := s.db.Exec(
		`UPDATE hosts SET credential=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		credential, id,
	)
	return err
}

func (s *Store) UpdateHostFingerprint(id, fingerprint string) error {
	_, err := s.db.Exec(
		`UPDATE hosts SET fingerprint=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		fingerprint, id,
	)
	return err
}

func (s *Store) DeleteHost(id string) error {
	_, err := s.db.Exec(`DELETE FROM hosts WHERE id=?`, id)
	return err
}

// UpdateHostHidden 设置桌面图标隐藏标记（1=隐藏）。
func (s *Store) UpdateHostHidden(id string, hidden bool) error {
	_, err := s.db.Exec(
		`UPDATE hosts SET hidden=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		hidden, id,
	)
	return err
}

// ShowAllHosts 清除全部主机的隐藏标记（「显示所有桌面隐藏的服务器」）。
func (s *Store) ShowAllHosts() error {
	_, err := s.db.Exec(`UPDATE hosts SET hidden=0, updated_at=CURRENT_TIMESTAMP`)
	return err
}

// EnsureBuiltinHost 若不存在内置网关主机则插入一条（幂等，INSERT OR IGNORE）。
// 账号密码留空由用户之后编辑填写；凭据为空字节（启动时 vault 未解锁，无法加密）。
// 用户删除过内置主机（SettingBuiltinDeleted=1）则跳过播种，尊重删除意愿。
// 在 cmd/ezssh/main.go（生产入口）调用，避免影响测试的主机数量断言。
func (s *Store) EnsureBuiltinHost() error {
	if deleted, _ := s.GetSetting(SettingBuiltinDeleted); deleted == "1" {
		return nil
	}
	h := &Host{
		ID: BuiltinHostID, Name: "Local (Gateway)", Host: "127.0.0.1", Port: 22,
		AuthType: "password", Credential: []byte{}, Builtin: true,
	}
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO hosts (`+hostCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,?)`,
		h.ID, h.Name, h.Host, h.Port, h.Username, h.AuthType,
		h.Credential, h.GroupName, h.Remark, h.Fingerprint, h.Hidden, h.Builtin, h.Platform,
	)
	return err
}
