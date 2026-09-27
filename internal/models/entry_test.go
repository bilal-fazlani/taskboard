package models

import (
	"reflect"
	"testing"
	"time"
)

// EntryTypes is the one list an entry's type is checked against, and
// TicketOnlyEntryTypes the part of it only a ticket can have.
func TestEntryTypesAndTicketOnlyTypes(t *testing.T) {
	if want := []string{"decision", "learning", "hand_off", "proof", "review", "note"}; !reflect.DeepEqual(EntryTypes, want) {
		t.Fatalf("EntryTypes = %v, want %v", EntryTypes, want)
	}
	if want := []string{"hand_off", "proof", "review"}; !reflect.DeepEqual(TicketOnlyEntryTypes, want) {
		t.Fatalf("TicketOnlyEntryTypes = %v, want %v", TicketOnlyEntryTypes, want)
	}
	for _, typ := range TicketOnlyEntryTypes {
		if !ValidEntryType(typ) {
			t.Errorf("ticket-only type %q is not in EntryTypes", typ)
		}
	}
	for _, typ := range []string{"decision", "learning", "note"} {
		if !ValidEntryType(typ) || TicketOnlyEntryType(typ) {
			t.Errorf("%q: valid %v, ticket-only %v; want valid anywhere", typ, ValidEntryType(typ), TicketOnlyEntryType(typ))
		}
	}
	for _, typ := range []string{"", "Decision", "hand-off", "handoff", "journal", "approval"} {
		if ValidEntryType(typ) {
			t.Errorf("ValidEntryType(%q) = true, want false", typ)
		}
	}
}

// Decision sources, review verdicts and finding severities are each one list.
func TestEntryFieldLists(t *testing.T) {
	for _, c := range []struct {
		list  []string
		want  []string
		valid func(string) bool
	}{
		{DecisionSources, []string{"agent", "person"}, ValidDecisionSource},
		{ReviewVerdicts, []string{"approve", "changes"}, ValidReviewVerdict},
		{ReviewSeverities, []string{"blocker", "major", "minor", "nit"}, ValidReviewSeverity},
	} {
		if !reflect.DeepEqual(c.list, c.want) {
			t.Errorf("list = %v, want %v", c.list, c.want)
		}
		for _, v := range c.want {
			if !c.valid(v) {
				t.Errorf("%q reads as invalid", v)
			}
		}
		if c.valid("") || c.valid("other") {
			t.Errorf("list %v accepts a value not in it", c.want)
		}
	}
}

// A note is open until it is handled or replaced.
func TestOnlyAnUnhandledNoteIsOpen(t *testing.T) {
	at := time.Now()
	for _, c := range []struct {
		e    Entry
		open bool
	}{
		{Entry{Type: EntryNote}, true},
		{Entry{Type: EntryNote, HandledBy: "a1", HandledAt: &at}, false},
		{Entry{Type: EntryNote, ReplacedBy: "n2"}, false},
		{Entry{Type: EntryDecision}, false},
	} {
		if c.e.Open() != c.open {
			t.Errorf("%+v: Open() = %v, want %v", c.e, c.e.Open(), c.open)
		}
	}
}
