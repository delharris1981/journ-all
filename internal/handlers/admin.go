package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"journall/internal/auth"
)

func (h *Handler) adminPage(w http.ResponseWriter, r *http.Request) {
	userID := auth.GetUserID(r)
	users, err := h.adminUsers()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	enabled, _ := h.signupsEnabled()
	h.render(w, "admin", PageData{User: userID, Title: "Admin", Users: users, SignupsEnabled: enabled})
}

func (h *Handler) adminCreateUser(w http.ResponseWriter, r *http.Request) {
	userID := auth.GetUserID(r)
	email := strings.TrimSpace(r.FormValue("email"))
	password := r.FormValue("password")
	isAdmin := 0
	if r.FormValue("is_admin") != "" {
		isAdmin = 1
	}
	if email == "" || len(password) < 8 {
		h.adminError(w, r, userID, "Email required and password must be at least 8 characters")
		return
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		h.adminError(w, r, userID, "Could not create user")
		return
	}
	if _, err := h.db.Exec(
		"INSERT INTO users (email, password_hash, is_admin) VALUES (?, ?, ?)",
		email, hash, isAdmin,
	); err != nil {
		h.adminError(w, r, userID, "Email already registered")
		return
	}
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (h *Handler) adminUsers() ([]UserRow, error) {
	rows, err := h.db.Query("SELECT id, email, is_admin, disabled FROM users ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []UserRow
	for rows.Next() {
		var u UserRow
		if err := rows.Scan(&u.ID, &u.Email, &u.IsAdmin, &u.Disabled); err == nil {
			users = append(users, u)
		}
	}
	return users, rows.Err()
}

func (h *Handler) adminError(w http.ResponseWriter, r *http.Request, userID int64, msg string) {
	users, _ := h.adminUsers()
	enabled, _ := h.signupsEnabled()
	h.render(w, "admin", PageData{User: userID, Title: "Admin", Users: users, SignupsEnabled: enabled, Error: msg})
}

func (h *Handler) adminToggleDisabled(w http.ResponseWriter, r *http.Request) {
	userID := auth.GetUserID(r)
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if id != userID { // admins cannot lock themselves out
		h.db.Exec("UPDATE users SET disabled = CASE disabled WHEN 0 THEN 1 ELSE 0 END WHERE id = ?", id)
	}
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (h *Handler) adminToggleSignups(w http.ResponseWriter, r *http.Request) {
	enabled, _ := h.signupsEnabled()
	v := "0"
	if !enabled {
		v = "1"
	}
	h.db.Exec("INSERT INTO settings (key, value) VALUES ('signups_enabled', ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value", v)
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (h *Handler) signupsEnabled() (bool, error) {
	var v string
	err := h.db.QueryRow("SELECT value FROM settings WHERE key = 'signups_enabled'").Scan(&v)
	if err != nil {
		return true, err
	}
	return v == "1", nil
}
