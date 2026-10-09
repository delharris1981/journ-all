package handlers

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"journall/internal/auth"
	"journall/internal/store"
)

func (h *Handler) export(w http.ResponseWriter, r *http.Request) {
	userID := auth.GetUserID(r)
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "zip"
	}

	entries, _ := h.store.GetEntries(userID, store.Filters{})

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

// maxSlugLen keeps export filenames inside common filesystem limits.
const maxSlugLen = 60

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = slugRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > maxSlugLen {
		s = s[:maxSlugLen]
		// Truncating can land on the separator it just created, leaving a
		// trailing dash in the filename.
		s = strings.TrimRight(s, "-")
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
		if _, err := h.store.CreateEntry(userID, title, body); err != nil {
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
