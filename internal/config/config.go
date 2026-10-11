// Package config reads the settings file: one option per line, with the same
// names as the command line options, e.g.
//
//	# ~/.config/ttrace/config
//	interval = 2s
//	no-as
//	format = markdown
package config

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Setting is one option from the file.
type Setting struct {
	Name  string
	Value string
	Line  int
}

// DefaultPath is where ttrace looks for its settings file when no --config
// is given (e.g. ~/.config/ttrace/config on Linux,
// %AppData%\ttrace\config on Windows).
func DefaultPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "ttrace", "config")
}

// Load reads a settings file. A missing file is not an error when
// mustExist is false.
func Load(path string, mustExist bool) ([]Setting, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) && !mustExist {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	return Parse(f, path)
}

// Parse reads settings from r; name is used in error messages.
func Parse(r interface{ Read([]byte) (int, error) }, name string) ([]Setting, error) {
	var out []Setting
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, hasVal := strings.Cut(line, "=")
		key = strings.TrimLeft(strings.TrimSpace(key), "-")
		val = strings.Trim(strings.TrimSpace(val), `"`)
		if !hasVal {
			val = "true"
		}
		if key == "" {
			return nil, fmt.Errorf("%s line %d: expected \"option = value\", e.g. \"interval = 2s\"", name, n)
		}
		out = append(out, Setting{Name: key, Value: val, Line: n})
	}
	return out, sc.Err()
}
