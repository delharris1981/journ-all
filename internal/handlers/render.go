package handlers

import (
	"database/sql"
	"html/template"
	"net/http"
	"os"
	"strings"

	"github.com/yuin/goldmark"
	"journall/internal/auth"
	"journall/internal/store"
)

type Handler struct {
	db        *sql.DB
	store     *store.Store
	templates map[string]*template.Template
	md        goldmark.Markdown
	version   string
}

// Entry and Tag live in the store package; aliases keep the templates and
// handler signatures unchanged.
type (
	Entry = store.Entry
	Tag   = store.Tag
)

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
	return &Handler{db: db, store: store.New(db), templates: templates, md: md, version: version}
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

	mux.Handle("GET /", h.authed(h.home))
	mux.Handle("GET /entries", h.authed(h.entries))
	mux.Handle("GET /entries/new", h.authed(h.entryNew))
	mux.Handle("POST /entries", h.authed(h.entryCreate))
	mux.Handle("GET /entries/{id}", h.authed(h.entryView))
	mux.Handle("GET /entries/{id}/edit", h.authed(h.entryEdit))
	mux.Handle("POST /entries/{id}", h.authed(h.entryUpdate))
	mux.Handle("POST /entries/{id}/delete", h.authed(h.entryDelete))
	mux.Handle("GET /calendar", h.authed(h.calendar))
	mux.Handle("GET /tags", h.authed(h.tags))
	mux.Handle("GET /tags/{tag}", h.authed(h.tagEntries))
	mux.Handle("GET /export", h.authed(h.export))
	mux.Handle("POST /preview", h.authed(h.preview))
	mux.Handle("GET /import", h.authed(h.importPage))
	mux.Handle("POST /import", h.authed(h.importZip))
	mux.Handle("GET /admin", h.admin(h.adminPage))
	mux.Handle("POST /admin/users", h.admin(h.adminCreateUser))
	mux.Handle("POST /admin/users/{id}/disabled", h.admin(h.adminToggleDisabled))
	mux.Handle("POST /admin/signups", h.admin(h.adminToggleSignups))
	return mux
}

// authed requires a logged-in session.
func (h *Handler) authed(fn http.HandlerFunc) http.Handler {
	return auth.Middleware(h.db, fn)
}

// admin requires a logged-in session that is an admin.
func (h *Handler) admin(fn http.HandlerFunc) http.Handler {
	return h.authed(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ok, _ := auth.IsAdmin(h.db, auth.GetUserID(r)); !ok {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		fn(w, r)
	}))
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
