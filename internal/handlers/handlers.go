package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/yuin/goldmark"
	"journall/internal/auth"
)

type Handler struct {
	db        *sql.DB
	templates map[string]*template.Template
	md        goldmark.Markdown
}

type Entry struct {
	ID        int64
	Title     string
	Body      string
	CreatedAt time.Time
	UpdatedAt time.Time
	Tags      []Tag
}

type Tag struct {
	ID   int64
	Name string
}

type CalendarDay struct {
	Date       string
	Day        int
	HasEntry   bool
	IsToday    bool
	OtherMonth bool
}

type CalendarData struct {
	Year      int
	Month     int
	MonthName string
	Days      []CalendarDay
	PrevYear  int
	PrevMonth int
	NextYear  int
	NextMonth int
}

// buildMonth returns whole weeks (leading and trailing days from the
// neighbouring months) so the 7-column grid has no empty cells.
func buildMonth(first time.Time, entryDates map[string]bool, today string) []CalendarDay {
	monthEnd := first.AddDate(0, 1, 0)
	last := monthEnd.AddDate(0, 0, -1)

	var days []CalendarDay
	startWeekday := int(first.Weekday())
	for i := 0; i < startWeekday; i++ {
		d := first.AddDate(0, 0, -startWeekday+i)
		days = append(days, CalendarDay{
			Date: d.Format("2006-01-02"), Day: d.Day(), OtherMonth: true,
		})
	}
	for d := first; d.Before(monthEnd); d = d.AddDate(0, 0, 1) {
		ds := d.Format("2006-01-02")
		days = append(days, CalendarDay{
			Date: ds, Day: d.Day(), HasEntry: entryDates[ds], IsToday: ds == today,
		})
	}
	lastWeekday := int(last.Weekday())
	for i := lastWeekday + 1; i < 7; i++ {
		d := last.AddDate(0, 0, i-lastWeekday)
		days = append(days, CalendarDay{
			Date: d.Format("2006-01-02"), Day: d.Day(), OtherMonth: true,
		})
	}
	return days
}

type PageData struct {
	User     int64
	Title    string
	Error    string
	Entries  []Entry
	Entry    *Entry
	Tags     []Tag
	Query    string
	Calendar *CalendarData
}

func New(db *sql.DB) *Handler {
	md := goldmark.New()
	funcMap := template.FuncMap{
		"markdown": func(body string) template.HTML {
			var buf strings.Builder
			if err := md.Convert([]byte(body), &buf); err != nil {
				return template.HTML("")
			}
			return template.HTML(buf.String())
		},
	}

	pages := []string{"home", "entries", "entry_edit", "entry_view", "calendar", "tags", "tag_entries", "login", "signup"}
	templates := make(map[string]*template.Template)
	for _, page := range pages {
		t, err := template.New("base.html").Funcs(funcMap).ParseFiles("web/templates/base.html", "web/templates/"+page+".html")
		if err != nil {
			panic(err)
		}
		templates[page] = t
	}
	return &Handler{db: db, templates: templates, md: md}
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.healthz)
	mux.HandleFunc("GET /login", h.login)
	mux.HandleFunc("POST /login", h.loginPost)
	mux.HandleFunc("GET /signup", h.signup)
	mux.HandleFunc("POST /signup", h.signupPost)
	mux.HandleFunc("POST /logout", h.logout)
	// no-cache so browsers always revalidate static assets, otherwise they keep
	// serving a stale app.js/app.css after an upgrade.
	assets := http.StripPrefix("/static/", http.FileServer(http.Dir("web/static")))
	mux.Handle("GET /static/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		assets.ServeHTTP(w, r)
	}))

	mux.Handle("GET /", auth.Middleware(h.db, http.HandlerFunc(h.home)))
	mux.Handle("GET /entries", auth.Middleware(h.db, http.HandlerFunc(h.entries)))
	mux.Handle("GET /entries/new", auth.Middleware(h.db, http.HandlerFunc(h.entryNew)))
	mux.Handle("POST /entries", auth.Middleware(h.db, http.HandlerFunc(h.entryCreate)))
	mux.Handle("GET /entries/{id}", auth.Middleware(h.db, http.HandlerFunc(h.entryView)))
	mux.Handle("GET /entries/{id}/edit", auth.Middleware(h.db, http.HandlerFunc(h.entryEdit)))
	mux.Handle("POST /entries/{id}", auth.Middleware(h.db, http.HandlerFunc(h.entryUpdate)))
	mux.Handle("POST /entries/{id}/delete", auth.Middleware(h.db, http.HandlerFunc(h.entryDelete)))
	mux.Handle("GET /calendar", auth.Middleware(h.db, http.HandlerFunc(h.calendar)))
	mux.Handle("GET /tags", auth.Middleware(h.db, http.HandlerFunc(h.tags)))
	mux.Handle("GET /tags/{tag}", auth.Middleware(h.db, http.HandlerFunc(h.tagEntries)))
	mux.Handle("GET /export", auth.Middleware(h.db, http.HandlerFunc(h.export)))
	mux.Handle("POST /preview", auth.Middleware(h.db, http.HandlerFunc(h.preview)))
	return mux
}

func (h *Handler) render(w http.ResponseWriter, page string, data PageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	err := h.templates[page].ExecuteTemplate(w, "base.html", data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (h *Handler) healthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	h.render(w, "login", PageData{Title: "Log in"})
}

func (h *Handler) loginPost(w http.ResponseWriter, r *http.Request) {
	email := strings.TrimSpace(r.FormValue("email"))
	password := r.FormValue("password")

	var id int64
	var hash string
	err := h.db.QueryRow("SELECT id, password_hash FROM users WHERE email = ?", email).Scan(&id, &hash)
	if err != nil || !auth.CheckPassword(hash, password) {
		h.render(w, "login", PageData{Title: "Log in", Error: "Invalid email or password"})
		return
	}

	token, err := auth.CreateSession(h.db, id)
	if err != nil {
		h.render(w, "login", PageData{Title: "Log in", Error: "Could not create session"})
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   30 * 24 * 60 * 60,
	})
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

	hash, err := auth.HashPassword(password)
	if err != nil {
		h.render(w, "signup", PageData{Title: "Sign up", Error: "Could not hash password"})
		return
	}

	var id int64
	err = h.db.QueryRow(
		"INSERT INTO users (email, password_hash) VALUES (?, ?) RETURNING id",
		email, hash,
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
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   30 * 24 * 60 * 60,
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("session")
	if err == nil {
		auth.DeleteSession(h.db, cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (h *Handler) home(w http.ResponseWriter, r *http.Request) {
	userID := auth.GetUserID(r)
	entries, err := h.getEntries(userID, "")
	if err != nil {
		entries = []Entry{}
	}
	h.render(w, "home", PageData{User: userID, Title: "Home", Entries: entries})
}

func (h *Handler) entries(w http.ResponseWriter, r *http.Request) {
	userID := auth.GetUserID(r)
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	entries, err := h.getEntries(userID, query)
	if err != nil {
		entries = []Entry{}
	}
	h.render(w, "entries", PageData{User: userID, Title: "Entries", Entries: entries, Query: query})
}

func (h *Handler) entryNew(w http.ResponseWriter, r *http.Request) {
	userID := auth.GetUserID(r)
	tags, _ := h.getAllTags(userID)
	h.render(w, "entry_edit", PageData{User: userID, Title: "New entry", Tags: tags})
}

func (h *Handler) entryCreate(w http.ResponseWriter, r *http.Request) {
	userID := auth.GetUserID(r)
	title := strings.TrimSpace(r.FormValue("title"))
	body := strings.TrimSpace(r.FormValue("body"))
	tagStr := strings.TrimSpace(r.FormValue("tags"))

	res, err := h.db.Exec(
		"INSERT INTO entries (user_id, title, body) VALUES (?, ?, ?)",
		userID, title, body,
	)
	if err != nil {
		h.render(w, "entry_edit", PageData{User: userID, Title: "New entry", Error: "Could not save entry"})
		return
	}
	entryID, _ := res.LastInsertId()
	h.setTags(entryID, userID, tagStr)

	http.Redirect(w, r, fmt.Sprintf("/entries/%d", entryID), http.StatusSeeOther)
}

func (h *Handler) entryView(w http.ResponseWriter, r *http.Request) {
	userID := auth.GetUserID(r)
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	entry, err := h.getEntry(userID, id)
	if err != nil {
		http.Redirect(w, r, "/entries", http.StatusSeeOther)
		return
	}
	h.render(w, "entry_view", PageData{User: userID, Title: entry.Title, Entry: entry})
}

func (h *Handler) entryEdit(w http.ResponseWriter, r *http.Request) {
	userID := auth.GetUserID(r)
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	entry, err := h.getEntry(userID, id)
	if err != nil {
		http.Redirect(w, r, "/entries", http.StatusSeeOther)
		return
	}
	tags, _ := h.getAllTags(userID)
	h.render(w, "entry_edit", PageData{User: userID, Title: "Edit entry", Entry: entry, Tags: tags})
}

func (h *Handler) entryUpdate(w http.ResponseWriter, r *http.Request) {
	userID := auth.GetUserID(r)
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	title := strings.TrimSpace(r.FormValue("title"))
	body := strings.TrimSpace(r.FormValue("body"))
	tagStr := strings.TrimSpace(r.FormValue("tags"))

	_, err := h.db.Exec(
		"UPDATE entries SET title = ?, body = ?, updated_at = datetime('now') WHERE id = ? AND user_id = ?",
		title, body, id, userID,
	)
	if err != nil {
		h.render(w, "entry_edit", PageData{User: userID, Title: "Edit entry", Error: "Could not update entry"})
		return
	}
	h.setTags(id, userID, tagStr)
	http.Redirect(w, r, fmt.Sprintf("/entries/%d", id), http.StatusSeeOther)
}

func (h *Handler) entryDelete(w http.ResponseWriter, r *http.Request) {
	userID := auth.GetUserID(r)
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	h.db.Exec("DELETE FROM entries WHERE id = ? AND user_id = ?", id, userID)
	http.Redirect(w, r, "/entries", http.StatusSeeOther)
}

func (h *Handler) calendar(w http.ResponseWriter, r *http.Request) {
	userID := auth.GetUserID(r)
	now := time.Now()
	year, month := now.Year(), int(now.Month())

	if y := r.URL.Query().Get("year"); y != "" {
		if v, err := strconv.Atoi(y); err == nil {
			year = v
		}
	}
	if m := r.URL.Query().Get("month"); m != "" {
		if v, err := strconv.Atoi(m); err == nil && v >= 1 && v <= 12 {
			month = v
		}
	}

	first := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	monthEnd := first.AddDate(0, 1, 0)

	rows, err := h.db.Query(
		"SELECT DISTINCT date(created_at) FROM entries WHERE user_id = ? AND created_at >= ? AND created_at < ?",
		userID, first.Format(time.RFC3339), monthEnd.Format(time.RFC3339),
	)
	entryDates := make(map[string]bool)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var d string
			if err := rows.Scan(&d); err == nil {
				entryDates[d] = true
			}
		}
	}

	days := buildMonth(first, entryDates, time.Now().Format("2006-01-02"))

	prevMonth := month - 1
	prevYear := year
	if prevMonth < 1 {
		prevMonth = 12
		prevYear--
	}
	nextMonth := month + 1
	nextYear := year
	if nextMonth > 12 {
		nextMonth = 1
		nextYear++
	}

	h.render(w, "calendar", PageData{
		User:  userID,
		Title: "Calendar",
		Calendar: &CalendarData{
			Year:      year,
			Month:     month,
			MonthName: time.Month(month).String(),
			Days:      days,
			PrevYear:  prevYear,
			PrevMonth: prevMonth,
			NextYear:  nextYear,
			NextMonth: nextMonth,
		},
	})
}

func (h *Handler) tags(w http.ResponseWriter, r *http.Request) {
	userID := auth.GetUserID(r)
	tags, _ := h.getAllTags(userID)
	h.render(w, "tags", PageData{User: userID, Title: "Tags", Tags: tags})
}

func (h *Handler) tagEntries(w http.ResponseWriter, r *http.Request) {
	userID := auth.GetUserID(r)
	tagName := r.PathValue("tag")
	entries, err := h.getEntriesByTag(userID, tagName)
	if err != nil {
		entries = []Entry{}
	}
	h.render(w, "tag_entries", PageData{User: userID, Title: tagName, Entries: entries})
}

func (h *Handler) export(w http.ResponseWriter, r *http.Request) {
	userID := auth.GetUserID(r)
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "json"
	}

	entries, _ := h.getEntries(userID, "")

	switch format {
	case "json":
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", "attachment; filename=journall-export.json")
		json.NewEncoder(w).Encode(entries)
	case "markdown":
		w.Header().Set("Content-Type", "text/markdown")
		w.Header().Set("Content-Disposition", "attachment; filename=journall-export.md")
		for _, e := range entries {
			fmt.Fprintf(w, "# %s\n\n%s\n\n---\n\n", e.Title, e.Body)
		}
	default:
		http.Error(w, "Invalid format", http.StatusBadRequest)
	}
}

func (h *Handler) preview(w http.ResponseWriter, r *http.Request) {
	body := r.FormValue("body")
	var buf strings.Builder
	if err := h.md.Convert([]byte(body), &buf); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(buf.String()))
}

const sqliteTimeFormat = "2006-01-02 15:04:05"

func (h *Handler) getEntries(userID int64, query string) ([]Entry, error) {
	var rows *sql.Rows
	var err error
	if query != "" {
		rows, err = h.db.Query(`
			SELECT e.id, e.title, e.body, e.created_at, e.updated_at
			FROM entries e
			WHERE e.user_id = ? AND (e.title LIKE ? OR e.body LIKE ?)
			ORDER BY e.created_at DESC
			LIMIT 100
		`, userID, "%"+query+"%", "%"+query+"%")
	} else {
		rows, err = h.db.Query(`
			SELECT id, title, body, created_at, updated_at
			FROM entries
			WHERE user_id = ?
			ORDER BY created_at DESC
			LIMIT 100
		`, userID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []Entry
	for rows.Next() {
		var e Entry
		var createdAt, updatedAt string
		if err := rows.Scan(&e.ID, &e.Title, &e.Body, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		e.CreatedAt, _ = time.Parse(sqliteTimeFormat, createdAt)
		e.UpdatedAt, _ = time.Parse(sqliteTimeFormat, updatedAt)
		entries = append(entries, e)
	}
	for i := range entries {
		entries[i].Tags, _ = h.getTagsForEntry(entries[i].ID)
	}
	return entries, nil
}

func (h *Handler) getEntry(userID, id int64) (*Entry, error) {
	var e Entry
	var createdAt, updatedAt string
	err := h.db.QueryRow(`
		SELECT id, title, body, created_at, updated_at
		FROM entries WHERE id = ? AND user_id = ?
	`, id, userID).Scan(&e.ID, &e.Title, &e.Body, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	e.CreatedAt, _ = time.Parse(sqliteTimeFormat, createdAt)
	e.UpdatedAt, _ = time.Parse(sqliteTimeFormat, updatedAt)
	e.Tags, _ = h.getTagsForEntry(e.ID)
	return &e, nil
}

func (h *Handler) getTagsForEntry(entryID int64) ([]Tag, error) {
	rows, err := h.db.Query(`
		SELECT t.id, t.name FROM tags t
		JOIN entry_tags et ON et.tag_id = t.id
		WHERE et.entry_id = ?
		ORDER BY t.name
	`, entryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tags []Tag
	for rows.Next() {
		var t Tag
		if err := rows.Scan(&t.ID, &t.Name); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	return tags, nil
}

func (h *Handler) getAllTags(userID int64) ([]Tag, error) {
	rows, err := h.db.Query(`
		SELECT t.id, t.name FROM tags t
		WHERE t.user_id = ?
		ORDER BY t.name
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tags []Tag
	for rows.Next() {
		var t Tag
		if err := rows.Scan(&t.ID, &t.Name); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	return tags, nil
}

func (h *Handler) getEntriesByTag(userID int64, tagName string) ([]Entry, error) {
	rows, err := h.db.Query(`
		SELECT e.id, e.title, e.body, e.created_at, e.updated_at
		FROM entries e
		JOIN entry_tags et ON et.entry_id = e.id
		JOIN tags t ON t.id = et.tag_id
		WHERE e.user_id = ? AND t.name = ?
		ORDER BY e.created_at DESC
	`, userID, tagName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []Entry
	for rows.Next() {
		var e Entry
		var createdAt, updatedAt string
		if err := rows.Scan(&e.ID, &e.Title, &e.Body, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		e.CreatedAt, _ = time.Parse(sqliteTimeFormat, createdAt)
		e.UpdatedAt, _ = time.Parse(sqliteTimeFormat, updatedAt)
		entries = append(entries, e)
	}
	for i := range entries {
		entries[i].Tags, _ = h.getTagsForEntry(entries[i].ID)
	}
	return entries, nil
}

func (h *Handler) setTags(entryID, userID int64, tagStr string) {
	h.db.Exec("DELETE FROM entry_tags WHERE entry_id = ?", entryID)
	if tagStr == "" {
		return
	}
	for _, name := range strings.Split(tagStr, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		var tagID int64
		err := h.db.QueryRow(
			"INSERT INTO tags (user_id, name) VALUES (?, ?) ON CONFLICT(user_id, name) DO UPDATE SET name = excluded.name RETURNING id",
			userID, name,
		).Scan(&tagID)
		if err != nil {
			continue
		}
		h.db.Exec("INSERT OR IGNORE INTO entry_tags (entry_id, tag_id) VALUES (?, ?)", entryID, tagID)
	}
}
