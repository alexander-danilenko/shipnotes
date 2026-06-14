package notes

import (
	"testing"
	"time"
)

// TestNowISOLayout locks the timestamp format to the ISO-8601-to-the-minute
// shape: "2006-01-02T15:04".
func TestNowISOLayout(t *testing.T) {
	// nowISO uses time.Now(); we cannot assert the value, but we can assert that
	// the layout it uses round-trips a known time to the expected string.
	fixed := time.Date(2024, 1, 15, 10, 30, 0, 123456000, time.UTC)
	got := fixed.Format("2006-01-02T15:04")

	want := "2024-01-15T10:30"
	if got != want {
		t.Errorf("layout produced %q, want %q", got, want)
	}

	// And that nowISO itself parses back as that layout (sanity check).
	if _, err := time.Parse("2006-01-02T15:04", nowISO()); err != nil {
		t.Errorf("nowISO() is not in the expected layout: %v", err)
	}
}
