package config

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	in := "# comment\n\ninterval = 2s\n--no-as\nformat=\"markdown\"\n"
	s, err := Parse(strings.NewReader(in), "test")
	if err != nil {
		t.Fatal(err)
	}
	want := []Setting{{"interval", "2s", 3}, {"no-as", "true", 4}, {"format", "markdown", 5}}
	if len(s) != len(want) {
		t.Fatalf("%+v", s)
	}
	for i := range want {
		if s[i] != want[i] {
			t.Errorf("%d: %+v, want %+v", i, s[i], want[i])
		}
	}
	if _, err := Parse(strings.NewReader("= 5\n"), "test"); err == nil {
		t.Error("missing name accepted")
	}
}
