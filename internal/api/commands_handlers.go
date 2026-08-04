package api

import (
	"net/http"
	"strconv"
	"strings"

	"ezssh/internal/store"
)

type commandDTO struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Command   string `json:"command"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

func toCommandDTO(c *store.SavedCommand) commandDTO {
	return commandDTO{ID: c.ID, Name: c.Name, Command: c.Command, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt}
}

// isUniqueErr 判断 SQLite UNIQUE 冲突（命令重名）。
func isUniqueErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// GET /api/commands 保存的命令列表。
func (s *Server) handleListCommands(w http.ResponseWriter, r *http.Request) {
	list, err := s.st.ListCommands()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "load commands failed")
		return
	}
	out := make([]commandDTO, 0, len(list))
	for i := range list {
		out = append(out, toCommandDTO(&list[i]))
	}
	writeJSON(w, http.StatusOK, out)
}

// POST /api/commands 新建命令。
func (s *Server) handleCreateCommand(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name    string `json:"name"`
		Command string `json:"command"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	name := strings.TrimSpace(req.Name)
	command := strings.TrimSpace(req.Command)
	if name == "" {
		writeErr(w, http.StatusBadRequest, "命令名不能为空")
		return
	}
	if command == "" {
		writeErr(w, http.StatusBadRequest, "命令内容不能为空")
		return
	}
	c, err := s.st.CreateCommand(name, command)
	if err != nil {
		if isUniqueErr(err) {
			writeErr(w, http.StatusBadRequest, "命令名已存在")
			return
		}
		writeErr(w, http.StatusInternalServerError, "保存失败")
		return
	}
	_ = s.st.AddAudit("commands.save", "", name)
	writeJSON(w, http.StatusCreated, toCommandDTO(c))
}

// PUT /api/commands/{id} 更新命令。
func (s *Server) handleUpdateCommand(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid command id")
		return
	}
	var req struct {
		Name    string `json:"name"`
		Command string `json:"command"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	name := strings.TrimSpace(req.Name)
	command := strings.TrimSpace(req.Command)
	if name == "" {
		writeErr(w, http.StatusBadRequest, "命令名不能为空")
		return
	}
	if command == "" {
		writeErr(w, http.StatusBadRequest, "命令内容不能为空")
		return
	}
	c, err := s.st.UpdateCommand(id, name, command)
	if err != nil {
		if err == store.ErrNotFound {
			writeErr(w, http.StatusNotFound, "not found")
			return
		}
		if isUniqueErr(err) {
			writeErr(w, http.StatusBadRequest, "命令名已存在")
			return
		}
		writeErr(w, http.StatusInternalServerError, "保存失败")
		return
	}
	_ = s.st.AddAudit("commands.update", "", name)
	writeJSON(w, http.StatusOK, toCommandDTO(c))
}

// DELETE /api/commands/{id} 删除命令。
func (s *Server) handleDeleteCommand(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid command id")
		return
	}
	if err := s.st.DeleteCommand(id); err != nil {
		if err == store.ErrNotFound {
			writeErr(w, http.StatusNotFound, "not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, "删除失败")
		return
	}
	_ = s.st.AddAudit("commands.delete", "", strconv.FormatInt(id, 10))
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}
