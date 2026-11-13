package main

import (
	"flag"
	"io"
	"reflect"
	"testing"
)

func TestOptionsFirst(t *testing.T) {
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	six := fs.Bool("6", false, "")
	count := fs.Int("c", 0, "")
	format := fs.String("format", "text", "")
	for _, c := range []struct {
		in, want []string
	}{
		{[]string{"example.com", "-6"}, []string{"-6", "--", "example.com"}},
		{[]string{"-6", "example.com"}, []string{"-6", "--", "example.com"}},
		{[]string{"example.com", "-c", "5", "-r"}, []string{"-c", "5", "-r", "--", "example.com"}},
		{[]string{"a.example", "--format=json", "b.example"}, []string{"--format=json", "--", "a.example", "b.example"}},
		{[]string{"x", "--", "-y"}, []string{"--", "x", "--", "-y"}},
		{[]string{"--", "-y"}, []string{"--", "-y"}},
	} {
		if got := optionsFirst(fs, c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q: got %q, want %q", c.in, got, c.want)
		}
	}
	if err := fs.Parse(optionsFirst(fs, []string{"example.com", "-c", "7", "-6", "--format", "json"})); err != nil {
		t.Fatal(err)
	}
	if !*six || *count != 7 || *format != "json" || fs.NArg() != 1 || fs.Arg(0) != "example.com" {
		t.Fatalf("6=%v c=%d format=%s args=%q", *six, *count, *format, fs.Args())
	}
}
