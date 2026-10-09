package handlers

import (
	"net/http"

	"journall/internal/auth"
	"journall/internal/store"
)

func (h *Handler) tags(w http.ResponseWriter, r *http.Request) {
	userID := auth.GetUserID(r)
	tags, _ := h.store.GetAllTags(userID)
	h.render(w, "tags", PageData{User: userID, Title: "Tags", Tags: tags})
}

func (h *Handler) tagEntries(w http.ResponseWriter, r *http.Request) {
	userID := auth.GetUserID(r)
	tagName := r.PathValue("tag")
	// Tag pages were unbounded before this refactor; -1 preserves that.
	entries, err := h.store.GetEntries(userID, store.Filters{Tag: tagName, Limit: -1})
	if err != nil {
		entries = []Entry{}
	}
	// ShowTags stays false: every entry on this page already carries tagName.
	h.render(w, "tag_entries", PageData{
		User:         userID,
		Title:        tagName,
		Entries:      entries,
		EmptyMessage: "No entries with this tag.",
	})
}
