package models

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestDependsOnDecodesObjects(t *testing.T) {
	var req UpdateTicketRequest
	body := `{"dependsOn":[{"ticket":"BILL-1"},{"ticket":"BILL-2","kind":"conflict_only","note":"store.go"}]}`
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("decoding %s: %v", body, err)
	}
	want := []DependencyInput{{Ticket: "BILL-1"}, {Ticket: "BILL-2", Kind: DependencyConflictOnly, Note: "store.go"}}
	if !reflect.DeepEqual(req.DependsOn, want) {
		t.Fatalf("dependsOn = %+v, want %+v", req.DependsOn, want)
	}

	// Left out or null leaves the list unset; [] is an empty list that clears.
	for body, wantNil := range map[string]bool{`{}`: true, `{"dependsOn":null}`: true, `{"dependsOn":[]}`: false} {
		var r UpdateTicketRequest
		if err := json.Unmarshal([]byte(body), &r); err != nil {
			t.Fatalf("decoding %s: %v", body, err)
		}
		if (r.DependsOn == nil) != wantNil {
			t.Fatalf("%s: dependsOn nil = %v, want %v", body, r.DependsOn == nil, wantNil)
		}
	}
}

func TestDependsOnRefusesStringsAndUnknownFields(t *testing.T) {
	for _, body := range []string{
		`{"dependsOn":["BILL-1"]}`,
		`{"dependsOn":["BILL-1:conflict_only"]}`,
		`{"dependsOn":[{"id":"BILL-1"}]}`,
		`{"dependsOn":[{"ticket":"BILL-1","kinds":"conflict_only"}]}`,
		`{"dependsOn":[7]}`,
	} {
		var req CreateTicketRequest
		err := json.Unmarshal([]byte(body), &req)
		var de *DependencyInputError
		if !errors.As(err, &de) {
			t.Fatalf("%s: err = %v, want a DependencyInputError", body, err)
		}
		if !strings.Contains(de.Error(), `"ticket"`) || !strings.Contains(de.Error(), "conflict_only") {
			t.Fatalf("%s: error %q does not name the object shape", body, de.Error())
		}
	}
}

func TestDependOn(t *testing.T) {
	if DependOn() != nil {
		t.Fatal("DependOn() is not nil")
	}
	if got := DependOn("A-1", "A-2"); !reflect.DeepEqual(got, []DependencyInput{{Ticket: "A-1"}, {Ticket: "A-2"}}) {
		t.Fatalf("DependOn = %+v", got)
	}
}
