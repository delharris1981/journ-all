package handlers

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"journall/internal/auth"
	"journall/internal/store"
)

func (h *Handler) home(w http.ResponseWriter, r *http.Request) {
	userID := auth.GetUserID(r)
	entries, err := h.store.GetEntries(userID, store.Filters{})
	if err != nil {
		entries = []Entry{}
	}
	h.render(w, "home", PageData{
		User:         userID,
		Title:        "Home",
		Entries:      entries,
		ShowTags:     true,
		EmptyMessage: `No entries yet. <a href="/entries/new">Write your first entry</a>.`,
	})
}

func (h *Handler) entries(w http.ResponseWriter, r *http.Request) {
	userID := auth.GetUserID(r)
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	date := r.URL.Query().Get("date")

	// Date filters in SQL rather than after the fetch — filtering in Go
	// would silently drop matches that fell outside the LIMIT window.
	entries, err := h.store.GetEntries(userID, store.Filters{Query: query, Date: date})
	if err != nil {
		entries = []Entry{}
	}
	data := PageData{
		User:         userID,
		Title:        "Entries",
		Entries:      entries,
		Query:        query,
		Date:         date,
		ShowTags:     true,
		EmptyMessage: "No entries found.",
	}
	if date != "" {
		data.Title = "Entries for " + date
	}
	h.render(w, "entries", data)
}

func (h *Handler) entryNew(w http.ResponseWriter, r *http.Request) {
	userID := auth.GetUserID(r)
	tags, _ := h.store.GetAllTags(userID)
	h.render(w, "entry_edit", PageData{User: userID, Title: "New entry", Tags: tags})
}

func (h *Handler) entryCreate(w http.ResponseWriter, r *http.Request) {
	userID := auth.GetUserID(r)
	title := strings.TrimSpace(r.FormValue("title"))
	body := strings.TrimSpace(r.FormValue("body"))
	tagStr := strings.TrimSpace(r.FormValue("tags"))

	entryID, err := h.store.CreateEntry(userID, title, body)
	if err != nil {
		h.render(w, "entry_edit", PageData{User: userID, Title: "New entry", Error: "Could not save entry"})
		return
	}
	h.store.SetTags(entryID, userID, tagStr)

	http.Redirect(w, r, fmt.Sprintf("/entries/%d", entryID), http.StatusSeeOther)
}

func (h *Handler) entryView(w http.ResponseWriter, r *http.Request) {
	userID := auth.GetUserID(r)
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	entry, err := h.store.GetEntry(userID, id)
	if err != nil {
		http.Redirect(w, r, "/entries", http.StatusSeeOther)
		return
	}
	h.render(w, "entry_view", PageData{User: userID, Title: entry.Title, Entry: entry})
}

func (h *Handler) entryEdit(w http.ResponseWriter, r *http.Request) {
	userID := auth.GetUserID(r)
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	entry, err := h.store.GetEntry(userID, id)
	if err != nil {
		http.Redirect(w, r, "/entries", http.StatusSeeOther)
		return
	}
	tags, _ := h.store.GetAllTags(userID)
	h.render(w, "entry_edit", PageData{User: userID, Title: "Edit entry", Entry: entry, Tags: tags})
}

func (h *Handler) entryUpdate(w http.ResponseWriter, r *http.Request) {
	userID := auth.GetUserID(r)
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	title := strings.TrimSpace(r.FormValue("title"))
	body := strings.TrimSpace(r.FormValue("body"))
	tagStr := strings.TrimSpace(r.FormValue("tags"))

	if _, err := h.store.UpdateEntry(userID, id, title, body); err != nil {
		h.render(w, "entry_edit", PageData{User: userID, Title: "Edit entry", Error: "Could not update entry"})
		return
	}
	h.store.SetTags(id, userID, tagStr)
	http.Redirect(w, r, fmt.Sprintf("/entries/%d", id), http.StatusSeeOther)
}

func (h *Handler) entryDelete(w http.ResponseWriter, r *http.Request) {
	userID := auth.GetUserID(r)
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid entry id", http.StatusBadRequest)
		return
	}
	if _, err := h.store.DeleteEntry(userID, id); err != nil {
		http.Error(w, "could not delete entry: "+err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/entries", http.StatusSeeOther)
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
