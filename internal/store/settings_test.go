package store

import "testing"

func TestGetSettingUnset(t *testing.T) {
	s, _, _ := newTestStore(t)

	_, found, err := s.GetSetting("nothing_here")
	if err != nil {
		t.Fatalf("GetSetting: %v", err)
	}
	if found {
		t.Error("GetSetting reported an unset key as present")
	}
}

// An empty string is a real value, distinct from the key being absent. The
// boolean helpers depend on telling those apart.
func TestGetSettingEmptyStringIsSet(t *testing.T) {
	s, _, _ := newTestStore(t)

	if err := s.SetSetting("blank", ""); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	v, found, err := s.GetSetting("blank")
	if err != nil {
		t.Fatalf("GetSetting: %v", err)
	}
	if !found || v != "" {
		t.Errorf("GetSetting = %q, %v; want \"\", true", v, found)
	}
}

func TestSetSettingOverwrites(t *testing.T) {
	s, _, _ := newTestStore(t)

	if err := s.SetSetting("k", "first"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	if err := s.SetSetting("k", "second"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	v, _, _ := s.GetSetting("k")
	if v != "second" {
		t.Errorf("value = %q, want second", v)
	}

	// Replacing must not insert a second row.
	var n int
	s.db.QueryRow("SELECT count(*) FROM settings WHERE key = 'k'").Scan(&n)
	if n != 1 {
		t.Errorf("%d rows for key k, want 1", n)
	}
}

// Migration 0002 seeds signups_enabled, so the default it should agree with is
// true. The fallback matters for databases where the row was removed.
func TestGetBoolSettingDefaults(t *testing.T) {
	s, _, _ := newTestStore(t)

	got, err := s.GetBoolSetting("never_set", true)
	if err != nil {
		t.Fatalf("GetBoolSetting: %v", err)
	}
	if !got {
		t.Error("unset key returned false, want the true default")
	}

	got, err = s.GetBoolSetting("never_set", false)
	if err != nil {
		t.Fatalf("GetBoolSetting: %v", err)
	}
	if got {
		t.Error("unset key returned true, want the false default")
	}

	// Migration 0002 seeds this key, so in production there is a row to read;
	// the fallback in signupsEnabled only covers one that has gone missing.
	if _, found, _ := s.GetSetting("signups_enabled"); !found {
		t.Error("migration 0002 did not seed signups_enabled")
	}
}

func TestBoolSettingsRoundTrip(t *testing.T) {
	s, _, _ := newTestStore(t)

	if err := s.SetBoolSetting("flag", false); err != nil {
		t.Fatalf("SetBoolSetting: %v", err)
	}
	if got, _ := s.GetBoolSetting("flag", true); got {
		t.Error("flag read back true after storing false")
	}

	if err := s.SetBoolSetting("flag", true); err != nil {
		t.Fatalf("SetBoolSetting: %v", err)
	}
	if got, _ := s.GetBoolSetting("flag", false); !got {
		t.Error("flag read back false after storing true")
	}
}

// A value that is neither "1" nor "0" — a truncated write, a future schema
// change — must not read as false by accident; it falls back to the default.
func TestGetBoolSettingNonBooleanValue(t *testing.T) {
	s, _, _ := newTestStore(t)

	if err := s.SetSetting("weird", "yes"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	got, err := s.GetBoolSetting("weird", true)
	if err != nil {
		t.Fatalf("GetBoolSetting: %v", err)
	}
	if !got {
		t.Error(`value "yes" returned false instead of the default`)
	}
}
