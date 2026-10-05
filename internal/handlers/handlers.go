package handlers

import (
	"archive/zip"
	"database/sql"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
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
	version   string
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
	User           int64
	Title          string
	Error          string
	Message        string
	Entries        []Entry
	Entry          *Entry
	Tags           []Tag
	Query          string
	Date           string
	Calendar       *CalendarData
	Version        string
	IsAdmin        bool
	Users          []UserRow
	SignupsEnabled bool
}

type UserRow struct {
	ID       int64
	Email    string
	IsAdmin  bool
	Disabled bool
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

	pages := []string{"home", "entries", "entry_edit", "entry_view", "calendar", "tags", "tag_entries", "login", "signup", "import", "admin"}
	templates := make(map[string]*template.Template)
	for _, page := range pages {
		t, err := template.New("base.html").Funcs(funcMap).ParseFiles("web/templates/base.html", "web/templates/"+page+".html")
		if err != nil {
			panic(err)
		}
		templates[page] = t
	}
	version := "dev"
	if b, err := os.ReadFile("VERSION"); err == nil {
		if v := strings.TrimSpace(string(b)); v != "" {
			version = v
		}
	}
	return &Handler{db: db, templates: templates, md: md, version: version}
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
	mux.Handle("GET /import", auth.Middleware(h.db, http.HandlerFunc(h.importPage)))
	mux.Handle("POST /import", auth.Middleware(h.db, http.HandlerFunc(h.importZip)))
	mux.Handle("GET /admin", auth.Middleware(h.db, http.HandlerFunc(h.adminPage)))
	mux.Handle("POST /admin/users", auth.Middleware(h.db, http.HandlerFunc(h.adminCreateUser)))
	mux.Handle("POST /admin/users/{id}/disabled", auth.Middleware(h.db, http.HandlerFunc(h.adminToggleDisabled)))
	mux.Handle("POST /admin/signups", auth.Middleware(h.db, http.HandlerFunc(h.adminToggleSignups)))
	return mux
}

func (h *Handler) render(w http.ResponseWriter, page string, data PageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	data.Version = h.version
	if data.User != 0 {
		data.IsAdmin, _ = auth.IsAdmin(h.db, data.User)
	}
	data.SignupsEnabled, _ = h.signupsEnabled()
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
	date := r.URL.Query().Get("date")
	entries, err := h.getEntries(userID, query)
	if err != nil {
		entries = []Entry{}
	}
	if date != "" {
		filtered := entries[:0]
		for _, e := range entries {
			if e.CreatedAt.Format("2006-01-02") == date {
				filtered = append(filtered, e)
			}
		}
		entries = filtered
	}
	if date != "" {
		h.render(w, "entries", PageData{User: userID, Title: "Entries for " + date, Entries: entries, Query: query, Date: date})
		return
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
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid entry id", http.StatusBadRequest)
		return
	}
	if _, err := h.db.Exec("DELETE FROM entries WHERE id = ? AND user_id = ?", id, userID); err != nil {
		http.Error(w, "could not delete entry: "+err.Error(), http.StatusInternalServerError)
		return
	}
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
		format = "zip"
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
	case "zip", "":
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", "attachment; filename=journall-export.zip")
		h.writeZip(w, entries)
	default:
		http.Error(w, "Invalid format", http.StatusBadRequest)
	}
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = slugRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 60 {
		s = s[:60]
	}
	return s
}

// writeZip writes one Markdown file per entry.
func (h *Handler) writeZip(w io.Writer, entries []Entry) error {
	zw := zip.NewWriter(w)
	defer zw.Close()
	used := map[string]int{}
	for _, e := range entries {
		name := slugify(e.Title)
		if name == "" {
			name = fmt.Sprintf("entry-%d", e.ID)
		}
		used[name]++
		filename := name
		if used[name] > 1 {
			filename = fmt.Sprintf("%s-%d", name, used[name])
		}
		if !e.CreatedAt.IsZero() {
			filename = e.CreatedAt.Format("2006-01-02") + "-" + filename
		}
		f, err := zw.Create(filename + ".md")
		if err != nil {
			return err
		}
		fmt.Fprintf(f, "# %s\n\n%s\n", e.Title, e.Body)
	}
	return nil
}

func (h *Handler) importPage(w http.ResponseWriter, r *http.Request) {
	userID := auth.GetUserID(r)
	msg := ""
	if n := r.URL.Query().Get("imported"); n != "" {
		msg = fmt.Sprintf("Imported %s file(s).", n)
	}
	h.render(w, "import", PageData{User: userID, Title: "Import", Message: msg})
}

func (h *Handler) importZip(w http.ResponseWriter, r *http.Request) {
	userID := auth.GetUserID(r)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		h.render(w, "import", PageData{User: userID, Title: "Import", Error: "Could not read upload"})
		return
	}
	f, _, err := r.FormFile("file")
	if err != nil {
		h.render(w, "import", PageData{User: userID, Title: "Import", Error: "Choose a .zip file"})
		return
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		h.render(w, "import", PageData{User: userID, Title: "Import", Error: "Could not read file"})
		return
	}
	n, err := h.importEntries(userID, data)
	if err != nil {
		h.render(w, "import", PageData{User: userID, Title: "Import", Error: err.Error()})
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/import?imported=%d", n), http.StatusSeeOther)
}

// importEntries imports every .md file in a zip as its own entry. The first
// '# ' line becomes the title when present; otherwise the filename does.
func (h *Handler) importEntries(userID int64, zipData []byte) (int, error) {
	zr, err := zip.NewReader(strings.NewReader(string(zipData)), int64(len(zipData)))
	if err != nil {
		return 0, fmt.Errorf("not a valid zip file")
	}
	n := 0
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || !strings.HasSuffix(strings.ToLower(f.Name), ".md") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			continue
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			continue
		}
		title, body := titleAndBody(string(b))
		if title == "" {
			base := path.Base(f.Name)
			title = strings.TrimSuffix(base, filepath.Ext(base))
		}
		if _, err := h.db.Exec(
			"INSERT INTO entries (user_id, title, body) VALUES (?, ?, ?)",
			userID, title, body,
		); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func titleAndBody(content string) (string, string) {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	i := strings.Index(content, "\n")
	first, rest := content, ""
	if i >= 0 {
		first, rest = content[:i], strings.TrimLeft(content[i+1:], "\n")
	}
	if strings.HasPrefix(first, "# ") {
		return strings.TrimSpace(first[2:]), rest
	}
	return "", content
}

func (h *Handler) adminPage(w http.ResponseWriter, r *http.Request) {
	userID := auth.GetUserID(r)
	if ok, _ := auth.IsAdmin(h.db, userID); !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	rows, err := h.db.Query("SELECT id, email, is_admin, disabled FROM users ORDER BY id")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	var users []UserRow
	for rows.Next() {
		var u UserRow
		if err := rows.Scan(&u.ID, &u.Email, &u.IsAdmin, &u.Disabled); err == nil {
			users = append(users, u)
		}
	}
	enabled, _ := h.signupsEnabled()
	h.render(w, "admin", PageData{User: userID, Title: "Admin", Users: users, SignupsEnabled: enabled})
}

func (h *Handler) adminCreateUser(w http.ResponseWriter, r *http.Request) {
	userID := auth.GetUserID(r)
	if ok, _ := auth.IsAdmin(h.db, userID); !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
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

func (h *Handler) adminError(w http.ResponseWriter, r *http.Request, userID int64, msg string) {
	rows, _ := h.db.Query("SELECT id, email, is_admin, disabled FROM users ORDER BY id")
	defer rows.Close()
	var users []UserRow
	for rows.Next() {
		var u UserRow
		if err := rows.Scan(&u.ID, &u.Email, &u.IsAdmin, &u.Disabled); err == nil {
			users = append(users, u)
		}
	}
	enabled, _ := h.signupsEnabled()
	h.render(w, "admin", PageData{User: userID, Title: "Admin", Users: users, SignupsEnabled: enabled, Error: msg})
}

func (h *Handler) adminToggleDisabled(w http.ResponseWriter, r *http.Request) {
	userID := auth.GetUserID(r)
	if ok, _ := auth.IsAdmin(h.db, userID); !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if id != userID { // admins cannot lock themselves out
		h.db.Exec("UPDATE users SET disabled = CASE disabled WHEN 0 THEN 1 ELSE 0 END WHERE id = ?", id)
	}
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (h *Handler) adminToggleSignups(w http.ResponseWriter, r *http.Request) {
	userID := auth.GetUserID(r)
	if ok, _ := auth.IsAdmin(h.db, userID); !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
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
