package fs

import (
	"regexp"
	"strings"
	"testing"
)

// mailboxIDPattern matches the short mailbox id shape produced by
// newMailboxID and the kernel's `_new_mailbox_id`: 8 digits, a `T`, 6
// digits, a dash, then 4 lowercase hex chars (20 chars total).
var mailboxIDPattern = regexp.MustCompile(`^\d{8}T\d{6}-[0-9a-f]{4}$`)

func TestNewMailboxID_Shape(t *testing.T) {
	id := newMailboxID()
	if !mailboxIDPattern.MatchString(id) {
		t.Fatalf("id = %q, want match of %s", id, mailboxIDPattern)
	}
	if len(id) != 20 {
		t.Errorf("len(id) = %d, want 20", len(id))
	}
}

func TestNewMailboxID_Sortable(t *testing.T) {
	// Two ids generated back-to-back should sort either equal-prefix
	// (same second) or strictly increasing. They must never be
	// strictly less than each other in violation of monotonic time.
	a := newMailboxID()
	b := newMailboxID()
	aPrefix, bPrefix := a[:15], b[:15] // YYYYMMDDTHHMMSS
	if strings.Compare(bPrefix, aPrefix) < 0 {
		t.Errorf("second id prefix %q < first %q — time went backwards", bPrefix, aPrefix)
	}
}
