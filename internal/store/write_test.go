package store

import "testing"

func TestCreateEntry(t *testing.T) {
	s, userID, _ := newTestStore(t)

	id, err := s.CreateEntry(userID, "Title", "Body")
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	entry, err := s.GetEntry(userID, id)
	if err != nil {
		t.Fatalf("GetEntry: %v", err)
	}
	if entry.Title != "Title" || entry.Body != "Body" {
		t.Errorf("got %q/%q, want Title/Body", entry.Title, entry.Body)
	}
}

func TestUpdateEntry(t *testing.T) {
	s, userID, otherID := newTestStore(t)
	id := seed(t, s, userID, "Old", "Old body", "2024-03-01 09:00:00")

	ok, err := s.UpdateEntry(userID, id, "New", "New body")
	if err != nil {
		t.Fatalf("UpdateEntry: %v", err)
	}
	if !ok {
		t.Fatal("UpdateEntry reported no rows affected")
	}

	entry, _ := s.GetEntry(userID, id)
	if entry.Title != "New" || entry.Body != "New body" {
		t.Errorf("got %q/%q, want New/New body", entry.Title, entry.Body)
	}

	// updated_at must move past created_at.
	if !entry.UpdatedAt.After(entry.CreatedAt) {
		t.Errorf("updated_at %v not after created_at %v", entry.UpdatedAt, entry.CreatedAt)
	}

	// Updating someone else's entry affects nothing.
	ok, err = s.UpdateEntry(otherID, id, "Hijacked", "x")
	if err != nil {
		t.Fatalf("UpdateEntry other: %v", err)
	}
	if ok {
		t.Error("UpdateEntry succeeded on another user's entry")
	}
	entry, _ = s.GetEntry(userID, id)
	if entry.Title != "New" {
		t.Errorf("title = %q, want New — other user's update leaked", entry.Title)
	}
}

func TestDeleteEntry(t *testing.T) {
	s, userID, otherID := newTestStore(t)
	id := seed(t, s, userID, "Doomed", "b", "2024-03-01 09:00:00")

	ok, err := s.DeleteEntry(otherID, id)
	if err != nil {
		t.Fatalf("DeleteEntry other: %v", err)
	}
	if ok {
		t.Error("DeleteEntry succeeded on another user's entry")
	}

	ok, err = s.DeleteEntry(userID, id)
	if err != nil {
		t.Fatalf("DeleteEntry: %v", err)
	}
	if !ok {
		t.Error("DeleteEntry reported no rows affected")
	}

	if _, err := s.GetEntry(userID, id); err == nil {
		t.Error("entry still readable after delete")
	}

	// Deleting again is a no-op, not an error.
	ok, _ = s.DeleteEntry(userID, id)
	if ok {
		t.Error("second DeleteEntry reported success")
	}
}
