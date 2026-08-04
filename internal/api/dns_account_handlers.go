package api

import (
	"net/http"
	"strings"

	"ezssh/internal/store"
)

// dnsAccountDTO DNS 账户对外视图（Token 永不返回明文，仅给 has_token 标记）。
type dnsAccountDTO struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Provider  string `json:"provider"`
	HasToken  bool   `json:"has_token"`
	CreatedAt string `json:"created_at"`
}

func (s *Server) dnsAccountToDTO(a *store.DnsAccount) dnsAccountDTO {
	return dnsAccountDTO{
		ID: a.ID, Name: a.Name, Provider: a.Provider,
		HasToken: len(a.TokenEnc) > 0, CreatedAt: a.CreatedAt,
	}
}

type dnsAccountReq struct {
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Token    string `json:"token,omitempty"`
}

// GET /api/dns-accounts
func (s *Server) handleListDnsAccounts(w http.ResponseWriter, r *http.Request) {
	accounts, err := s.st.ListDnsAccounts()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "list dns accounts failed")
		return
	}
	out := make([]dnsAccountDTO, 0, len(accounts))
	for _, a := range accounts {
		out = append(out, s.dnsAccountToDTO(a))
	}
	writeJSON(w, http.StatusOK, out)
}

// POST /api/dns-accounts
func (s *Server) handleCreateDnsAccount(w http.ResponseWriter, r *http.Request) {
	if !s.v.IsUnlocked() {
		writeErr(w, http.StatusForbidden, "vault locked, please login to unlock")
		return
	}
	var req dnsAccountReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Provider = strings.TrimSpace(strings.ToLower(req.Provider))
	if req.Name == "" {
		writeErr(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.Provider != "cloudflare" {
		writeErr(w, http.StatusBadRequest, "provider must be cloudflare")
		return
	}
	if strings.TrimSpace(req.Token) == "" {
		writeErr(w, http.StatusBadRequest, "token is required")
		return
	}
	enc, err := s.v.Encrypt([]byte(strings.TrimSpace(req.Token)))
	if err != nil {
		writeErr(w, http.StatusForbidden, "vault locked")
		return
	}
	id, err := newRandomID("dns")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "id generation failed")
		return
	}
	a := &store.DnsAccount{ID: id, Name: req.Name, Provider: req.Provider, TokenEnc: enc}
	if err := s.st.CreateDnsAccount(a); err != nil {
		writeErr(w, http.StatusInternalServerError, "create dns account failed")
		return
	}
	_ = s.st.AddAudit("dns_account.create", "", req.Name)
	writeJSON(w, http.StatusCreated, s.dnsAccountToDTO(a))
}

// PUT /api/dns-accounts/{id} token 留空表示保留原 Token。
func (s *Server) handleUpdateDnsAccount(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	old, err := s.st.GetDnsAccount(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "dns account not found")
		return
	}
	var req dnsAccountReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Provider = strings.TrimSpace(strings.ToLower(req.Provider))
	if req.Name == "" {
		writeErr(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.Provider != "cloudflare" {
		writeErr(w, http.StatusBadRequest, "provider must be cloudflare")
		return
	}
	upd := &store.DnsAccount{ID: id, Name: req.Name, Provider: req.Provider}
	if err := s.st.UpdateDnsAccount(upd); err != nil {
		writeErr(w, http.StatusInternalServerError, "update dns account failed")
		return
	}
	if strings.TrimSpace(req.Token) != "" {
		if !s.v.IsUnlocked() {
			writeErr(w, http.StatusForbidden, "vault locked, please login to unlock")
			return
		}
		enc, err := s.v.Encrypt([]byte(strings.TrimSpace(req.Token)))
		if err != nil {
			writeErr(w, http.StatusForbidden, "vault locked")
			return
		}
		if err := s.st.UpdateDnsAccountToken(id, enc); err != nil {
			writeErr(w, http.StatusInternalServerError, "update token failed")
			return
		}
	}
	_ = s.st.AddAudit("dns_account.update", "", req.Name)
	writeJSON(w, http.StatusOK, s.dnsAccountToDTO(old))
}

// DELETE /api/dns-accounts/{id}
func (s *Server) handleDeleteDnsAccount(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := s.st.GetDnsAccount(id); err != nil {
		writeErr(w, http.StatusNotFound, "dns account not found")
		return
	}
	if err := s.st.DeleteDnsAccount(id); err != nil {
		writeErr(w, http.StatusInternalServerError, "delete dns account failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}
