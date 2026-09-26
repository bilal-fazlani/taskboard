package ticketlist

import (
	"strings"
	"testing"
)

func intp(i int) *int { return &i }

// Paging is on when the caller asks for the summary or for a page, never
// otherwise, so a caller asking for neither keeps the old answer.
func TestRequestPaged(t *testing.T) {
	for _, tc := range []struct {
		req  Request
		want bool
	}{
		{Request{}, false},
		{Request{Summary: true}, true},
		{Request{Limit: intp(10)}, true},
		{Request{Offset: intp(0)}, true},
	} {
		if got := tc.req.Paged(); got != tc.want {
			t.Fatalf("%+v.Paged() = %v, want %v", tc.req, got, tc.want)
		}
	}
}

func TestRequestBounds(t *testing.T) {
	for _, tc := range []struct {
		req               Request
		limit, offset     int
		wantErrMentioning string
	}{
		{Request{Summary: true}, DefaultLimit, 0, ""},
		{Request{Limit: intp(1), Offset: intp(40)}, 1, 40, ""},
		{Request{Limit: intp(MaxLimit)}, MaxLimit, 0, ""},
		{Request{Limit: intp(0)}, 0, 0, "limit"},
		{Request{Limit: intp(MaxLimit + 1)}, 0, 0, "limit"},
		{Request{Offset: intp(-1)}, 0, 0, "offset"},
	} {
		limit, offset, err := tc.req.Bounds()
		if tc.wantErrMentioning != "" {
			if err == nil || !strings.Contains(err.Error(), tc.wantErrMentioning) {
				t.Fatalf("%+v.Bounds() err = %v, want one about %s", tc.req, err, tc.wantErrMentioning)
			}
			continue
		}
		if err != nil || limit != tc.limit || offset != tc.offset {
			t.Fatalf("%+v.Bounds() = %d, %d, %v, want %d, %d", tc.req, limit, offset, err, tc.limit, tc.offset)
		}
	}
}

// A page says whether more follow and where the next one starts.
func TestNewPage(t *testing.T) {
	p := NewPage(nil, 3, 7, 3, 3)
	if !p.HasMore || p.NextOffset == nil || *p.NextOffset != 6 {
		t.Fatalf("middle page = %+v, want hasMore and nextOffset 6", p)
	}
	for _, last := range []Page{NewPage(nil, 1, 7, 6, 3), NewPage(nil, 0, 7, 50, 3), NewPage(nil, 0, 0, 0, 3)} {
		if last.HasMore || last.NextOffset != nil {
			t.Fatalf("last page = %+v, want no more", last)
		}
	}
}
