package models

import "testing"

func TestIsImageRef(t *testing.T) {
	for _, c := range []struct {
		name, format, ref string
		want              bool
	}{
		{"Login screen", DocumentFormatPNG, "Login screen.png", true},
		{"Login screen", DocumentFormatPNG, "LOGIN SCREEN.PNG", true},
		{"Login screen", DocumentFormatPNG, " login screen.png ", true},
		{"Login screen", DocumentFormatPNG, "Login screen", false},
		{"Login screen", DocumentFormatPNG, "Login screen.gif", false},
		{"Photo", DocumentFormatJPEG, "Photo.jpg", true},
		{"Photo", DocumentFormatJPEG, "photo.JPEG", true},
		{"Shot", DocumentFormatPNG, "Shot.jpeg", false},
		{"Flow", DocumentFormatGIF, "flow.gif", true},
		{"Mock", DocumentFormatWebP, "Mock.webp", true},
		{"Plan", DocumentFormatMarkdown, "Plan.md", false},
		{"Report", DocumentFormatHTML, "Report.html", false},
	} {
		if got := IsImageRef(c.name, c.format, c.ref); got != c.want {
			t.Errorf("IsImageRef(%q, %q, %q) = %v, want %v", c.name, c.format, c.ref, got, c.want)
		}
	}
}
