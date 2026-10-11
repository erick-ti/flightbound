package traveladvice

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode"
)

func TestLookup(t *testing.T) {
	for slug, name := range map[string]string{
		"portugal":      "Portugal",
		"cote-d-ivoire": "Côte d'Ivoire",
		"myanmar":       "Myanmar (Burma)",
	} {
		if c, ok := Lookup(slug); !ok || c.Slug != slug || c.Name != name {
			t.Errorf("Lookup(%q) = %+v, %v; want %q", slug, c, ok, name)
		}
	}
	for _, slug := range []string{"", "atlantis", "Portugal", " portugal", "portugal/", "../portugal", "united-kingdom"} {
		if c, ok := Lookup(slug); ok {
			t.Errorf("Lookup(%q) = %+v, want no country", slug, c)
		}
	}
}

func TestPageURL(t *testing.T) {
	if got, want := PageURL("ukraine"), "https://www.gov.uk/foreign-travel-advice/ukraine"; got != want {
		t.Errorf("PageURL = %q, want %q", got, want)
	}
}

// The saved list is maintained by hand, so check what a refresh must keep.
func TestCountriesList(t *testing.T) {
	list := Countries()
	if len(list) < 200 {
		t.Fatalf("%d countries; the saved list looks truncated", len(list))
	}
	slug := regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	seen := map[string]bool{}
	for i, c := range list {
		if !slug.MatchString(c.Slug) {
			t.Errorf("slug %q is not a GOV.UK path segment", c.Slug)
		}
		if seen[c.Slug] {
			t.Errorf("slug %q appears twice", c.Slug)
		}
		seen[c.Slug] = true
		if c.Name == "" || c.Name != strings.TrimSpace(c.Name) || strings.IndexFunc(c.Name, unicode.IsControl) >= 0 {
			t.Errorf("%s: name %q is empty, padded, or has control characters", c.Slug, c.Name)
		}
		if i > 0 && list[i-1].Slug >= c.Slug {
			t.Errorf("%q comes after %q; keep the list in slug order", c.Slug, list[i-1].Slug)
		}
	}
	files, err := filepath.Glob(filepath.Join("testdata", "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no saved pages: %v", err)
	}
	for _, f := range files {
		if s := strings.TrimSuffix(filepath.Base(f), ".json"); !seen[s] {
			t.Errorf("saved page %s is not in the country list", s)
		}
	}
}

func TestCountriesReturnsACopy(t *testing.T) {
	list := Countries()
	list[0].Name = "changed"
	if Countries()[0].Name == "changed" {
		t.Error("changing the returned list changed the saved one")
	}
}

func TestLoadDirServesSavedPages(t *testing.T) {
	dir, err := LoadDir("testdata")
	if err != nil {
		t.Fatal(err)
	}
	for _, slug := range []string{"portugal", "thailand", "afghanistan", "ukraine"} {
		want, err := Parse(slug, readPage(t, slug))
		if err != nil {
			t.Fatal(err)
		}
		got, err := dir.Advice(t.Context(), slug)
		if err != nil {
			t.Fatalf("%s: %v", slug, err)
		}
		if !got.UpdatedAt.Equal(want.UpdatedAt) || len(got.Alerts) != len(want.Alerts) || !got.CheckedAt.IsZero() {
			t.Errorf("%s: got %+v, want %+v with no check time", slug, got, want)
		}
	}
	if _, err := dir.Advice(t.Context(), "spain"); err == nil {
		t.Error("spain has no saved page but got advice")
	}
}

func TestLoadDirRefusesBadDirectories(t *testing.T) {
	write := func(t *testing.T, files map[string][]byte) string {
		t.Helper()
		dir := t.TempDir()
		for name, body := range files {
			if err := os.WriteFile(filepath.Join(dir, name), body, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		return dir
	}
	portugal := readPage(t, "portugal")
	tests := []struct {
		name  string
		files map[string][]byte
	}{
		{"no saved pages", map[string][]byte{"README.md": []byte("notes")}},
		{"a file named after an unknown country", map[string][]byte{"atlantis.json": portugal}},
		{"a page saved under another country", map[string][]byte{"spain.json": portugal}},
		{"a page that does not parse", map[string][]byte{"portugal.json": []byte("<html>Maintenance</html>")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := LoadDir(write(t, tt.files)); err == nil {
				t.Error("no error")
			}
		})
	}
	if _, err := LoadDir(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Error("missing directory: no error")
	}
	if _, err := LoadDir(write(t, map[string][]byte{"portugal.json": portugal, "notes.txt": []byte("x")})); err != nil {
		t.Errorf("a directory with one saved page and another file: %v", err)
	}
}
