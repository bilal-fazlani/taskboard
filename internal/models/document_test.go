package models

import "testing"

func TestDocumentDisplayName(t *testing.T) {
	if got := DocumentDisplayName("Design spec", DocumentFormatMarkdown); got != "Design spec.md" {
		t.Fatalf("markdown display name = %q", got)
	}
	if got := DocumentDisplayName("Report", DocumentFormatHTML); got != "Report.html" {
		t.Fatalf("html display name = %q", got)
	}
}

func TestFormatSize(t *testing.T) {
	for _, tc := range []struct {
		bytes int
		want  string
	}{
		{0, "0 B"},
		{1023, "1023 B"},
		{1024, "1 KB"},
		{1025, "2 KB"},
		{14 * 1024, "14 KB"},
		{8 << 20, "8.0 MB"},
		{(8 << 20) + 1, "8.1 MB"},
	} {
		if got := FormatSize(tc.bytes); got != tc.want {
			t.Errorf("FormatSize(%d) = %q, want %q", tc.bytes, got, tc.want)
		}
	}
}
