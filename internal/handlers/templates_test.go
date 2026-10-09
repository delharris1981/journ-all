package handlers

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"html/template"

	"journall/internal/store"
)

// repoRoot is the directory holding web/templates, derived from this file's
// location so the test does not depend on the working directory. Production
// code resolves templates relative to the cwd, which is the repo root when
// the server runs; a test that chdir'd would be at the mercy of whatever the
// runner was started in.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test source")
	}
	return filepath.Join(filepath.Dir(file), "..", "..")
}

// renderTemplate parses the real templates and renders one page, so a broken
// {{define}} or a missing partial is caught here rather than as a 500 at
// runtime.
func renderTemplate(t *testing.T, page string, data PageData) string {
	t.Helper()

	root := repoRoot(t)
	for _, f := range []string{
		"web/templates/base.html",
		"web/templates/partials/entry_list.html",
		"web/templates/" + page + ".html",
	} {
		if _, err := os.Stat(filepath.Join(root, f)); err != nil {
			t.Fatalf("template %s: %v", f, err)
		}
	}

	tmpl, err := template.New("base.html").ParseFiles(
		filepath.Join(root, "web/templates/base.html"),
		filepath.Join(root, "web/templates/partials/entry_list.html"),
		filepath.Join(root, "web/templates/"+page+".html"),
	)
	if err != nil {
		t.Fatalf("parse %s: %v", page, err)
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "base.html", data); err != nil {
		t.Fatalf("render %s: %v", page, err)
	}
	return buf.String()
}

func testEntries() []Entry {
	return []Entry{
		{
			ID:        7,
			Title:     "Sourdough",
			Body:      "baking",
			CreatedAt: time.Date(2024, 3, 5, 9, 0, 0, 0, time.UTC),
			Tags:      []store.Tag{{ID: 1, Name: "baking"}},
		},
	}
}

// The three list pages share one partial. Each must still render its own
// heading and empty-state copy.
func TestEntryListPartialAcrossPages(t *testing.T) {
	for _, tc := range []struct {
		page string
		data PageData
		want []string
	}{
		{
			page: "home",
			data: PageData{Entries: testEntries(), ShowTags: true, EmptyMessage: "No entries yet."},
			want: []string{"Recent entries", "/entries/7", "Sourdough"},
		},
		{
			page: "entries",
			data: PageData{Entries: testEntries(), ShowTags: true, EmptyMessage: "No entries found."},
			want: []string{"Entries", "/entries/7"},
		},
		{
			page: "tag_entries",
			data: PageData{Title: "baking", Entries: testEntries(), EmptyMessage: "No entries with this tag."},
			want: []string{"baking", "/entries/7"},
		},
	} {
		got := renderTemplate(t, tc.page, tc.data)
		for _, w := range tc.want {
			if !strings.Contains(got, w) {
				t.Errorf("%s: output missing %q", tc.page, w)
			}
		}
	}
}

// Home and the entries list show each entry's tags; a tag page does not, since
// repeating the tag under every row is noise. The partial keys off ShowTags.
func TestEntryListShowTagsFlag(t *testing.T) {
	withTags := renderTemplate(t, "home", PageData{Entries: testEntries(), ShowTags: true})
	if !strings.Contains(withTags, `href="/tags/baking"`) {
		t.Error("home did not render entry tags")
	}

	withoutTags := renderTemplate(t, "tag_entries", PageData{Title: "baking", Entries: testEntries()})
	if strings.Contains(withoutTags, `href="/tags/baking"`) {
		t.Error("tag page rendered entry tags")
	}
	if !strings.Contains(withoutTags, "/entries/7") {
		t.Error("tag page lost its entry list")
	}
}

// Each page keeps its own empty-state copy rather than a shared default.
func TestEntryListEmptyMessage(t *testing.T) {
	for _, tc := range []struct {
		page string
		want template.HTML
	}{
		{"home", "No entries yet."},
		{"entries", "No entries found."},
		{"tag_entries", "No entries with this tag."},
	} {
		got := renderTemplate(t, tc.page, PageData{EmptyMessage: tc.want})
		if !strings.Contains(got, string(tc.want)) {
			t.Errorf("%s: empty state %q not rendered", tc.page, tc.want)
		}
		if strings.Contains(got, "entry-list") {
			t.Errorf("%s: rendered an empty list instead of the empty state", tc.page)
		}
	}
}

// The home empty state carries a link, which is why EmptyMessage is
// template.HTML — the link must survive escaping.
func TestEmptyMessageLinkRenders(t *testing.T) {
	got := renderTemplate(t, "home", PageData{
		EmptyMessage: `No entries yet. <a href="/entries/new">Write your first entry</a>.`,
	})
	if !strings.Contains(got, `<a href="/entries/new">Write your first entry</a>`) {
		t.Errorf("link in empty state was escaped: %s", got)
	}
}

// The entries page swaps its list over htmx, so the wrapper div the JS targets
// has to survive the move into the partial.
func TestEntriesListKeepsHtmxTarget(t *testing.T) {
	got := renderTemplate(t, "entries", PageData{Entries: testEntries(), ShowTags: true})
	if !strings.Contains(got, `id="entries-list"`) {
		t.Error(`entries page lost id="entries-list", which htmx swaps into`)
	}
}
