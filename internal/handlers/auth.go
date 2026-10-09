package handlers

import (
	"net/http"
	"strings"

	"journall/internal/auth"
)

// setSessionCookie writes the session cookie. An empty token clears it.
func setSessionCookie(w http.ResponseWriter, token string) {
	maxAge := 30 * 24 * 60 * 60
	if token == "" {
		maxAge = -1
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   maxAge,
	})
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	h.render(w, "login", PageData{Title: "Log in"})
}

func (h *Handler) loginPost(w http.ResponseWriter, r *http.Request) {
	email := strings.TrimSpace(r.FormValue("email"))
	password := r.FormValue("password")

	var id int64
	var hash string
	var disabled bool
	err := h.db.QueryRow("SELECT id, password_hash, disabled FROM users WHERE email = ?", email).Scan(&id, &hash, &disabled)
	if err != nil || !auth.CheckPassword(hash, password) {
		h.render(w, "login", PageData{Title: "Log in", Error: "Invalid email or password"})
		return
	}
	if disabled {
		h.render(w, "login", PageData{Title: "Log in", Error: "This account has been disabled"})
		return
	}

	token, err := auth.CreateSession(h.db, id)
	if err != nil {
		h.render(w, "login", PageData{Title: "Log in", Error: "Could not create session"})
		return
	}
	setSessionCookie(w, token)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *Handler) signup(w http.ResponseWriter, r *http.Request) {
	h.render(w, "signup", PageData{Title: "Sign up"})
}

func (h *Handler) signupPost(w http.ResponseWriter, r *http.Request) {
	email := strings.TrimSpace(r.FormValue("email"))
	password := r.FormValue("password")
	confirm := r.FormValue("confirm")

	if email == "" || password == "" {
		h.render(w, "signup", PageData{Title: "Sign up", Error: "Email and password required"})
		return
	}
	if password != confirm {
		h.render(w, "signup", PageData{Title: "Sign up", Error: "Passwords do not match"})
		return
	}
	if len(password) < 8 {
		h.render(w, "signup", PageData{Title: "Sign up", Error: "Password must be at least 8 characters"})
		return
	}

	enabled, _ := h.signupsEnabled()
	if !enabled {
		h.render(w, "signup", PageData{Title: "Sign up", Error: "Signups are currently disabled"})
		return
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		h.render(w, "signup", PageData{Title: "Sign up", Error: "Could not hash password"})
		return
	}

	var userCount int
	h.db.QueryRow("SELECT COUNT(*) FROM users").Scan(&userCount)
	isAdmin := 0
	if userCount == 0 {
		isAdmin = 1 // first account administers the app
	}

	var id int64
	err = h.db.QueryRow(
		"INSERT INTO users (email, password_hash, is_admin) VALUES (?, ?, ?) RETURNING id",
		email, hash, isAdmin,
	).Scan(&id)
	if err != nil {
		h.render(w, "signup", PageData{Title: "Sign up", Error: "Email already registered"})
		return
	}

	token, err := auth.CreateSession(h.db, id)
	if err != nil {
		h.render(w, "signup", PageData{Title: "Sign up", Error: "Could not create session"})
		return
	}
	setSessionCookie(w, token)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("session")
	if err == nil {
		auth.DeleteSession(h.db, cookie.Value)
	}
	setSessionCookie(w, "")
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
