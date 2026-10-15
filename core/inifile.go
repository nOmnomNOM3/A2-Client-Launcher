package core

import (
	"os"
	"path/filepath"
	"strings"
)

// TextFile is a config file edited line by line: every line we do not change
// (comments, unknown keys, other sections, blank lines) is written back
// byte-for-byte, and the original line ending (CRLF or LF) is kept.
type TextFile struct {
	Path  string
	Lines []string
	EOL   string
	Exist bool
	dirty bool
}

func LoadTextFile(p string) (*TextFile, error) {
	f := &TextFile{Path: p, EOL: "\r\n"}
	b, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return f, nil
	}
	if err != nil {
		return nil, err
	}
	f.Exist = true
	s := string(b)
	if !strings.Contains(s, "\r\n") && strings.Contains(s, "\n") {
		f.EOL = "\n"
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.TrimSuffix(s, "\n")
	if s != "" {
		f.Lines = strings.Split(s, "\n")
	}
	return f, nil
}

// Save writes the file atomically (temp file + rename) if anything changed.
func (f *TextFile) Save() error {
	if !f.dirty {
		return nil
	}
	tmp := f.Path + ".a2tmp"
	data := strings.Join(f.Lines, f.EOL) + f.EOL
	if err := os.WriteFile(tmp, []byte(data), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, f.Path); err != nil {
		os.Remove(tmp)
		return err
	}
	f.dirty = false
	f.Exist = true
	return nil
}

// ---- INI ([section] key=value, ';' or '#' comments): cnc-ddraw's ddraw.ini ----

func iniSection(line string) (string, bool) {
	t := strings.TrimSpace(line)
	if strings.HasPrefix(t, "[") && strings.HasSuffix(t, "]") {
		return strings.ToLower(strings.TrimSpace(t[1 : len(t)-1])), true
	}
	return "", false
}

func iniKV(line string) (key, val string, ok bool) {
	t := strings.TrimSpace(line)
	if t == "" || t[0] == ';' || t[0] == '#' {
		return "", "", false
	}
	i := strings.IndexByte(t, '=')
	if i <= 0 {
		return "", "", false
	}
	return strings.ToLower(strings.TrimSpace(t[:i])), strings.TrimSpace(t[i+1:]), true
}

// find returns the line index of key in section, or -1; end is the index after
// the section's last key line (where a new key goes), or -1 if no section.
func (f *TextFile) iniFind(section, key string) (idx, end int) {
	section, key = strings.ToLower(section), strings.ToLower(key)
	idx, end = -1, -1
	cur := ""
	for i, l := range f.Lines {
		if s, ok := iniSection(l); ok {
			cur = s
			if cur == section && end < 0 {
				end = i + 1
			}
			continue
		}
		if cur != section {
			continue
		}
		if k, _, ok := iniKV(l); ok {
			end = i + 1
			if k == key && idx < 0 {
				idx = i
			}
		}
	}
	return idx, end
}

func (f *TextFile) IniGet(section, key string) (string, bool) {
	i, _ := f.iniFind(section, key)
	if i < 0 {
		return "", false
	}
	_, v, _ := iniKV(f.Lines[i])
	return v, true
}

func (f *TextFile) HasSection(section string) bool {
	for _, l := range f.Lines {
		if s, ok := iniSection(l); ok && s == strings.ToLower(section) {
			return true
		}
	}
	return false
}

// IniSet changes key's value in place (keeping the key's spelling and the
// indentation) or adds it at the end of the section (creating it if needed).
func (f *TextFile) IniSet(section, key, val string) {
	i, end := f.iniFind(section, key)
	if i >= 0 {
		l := f.Lines[i]
		eq := strings.IndexByte(l, '=')
		nl := l[:eq+1] + val
		if nl != l {
			f.Lines[i], f.dirty = nl, true
		}
		return
	}
	f.dirty = true
	if end < 0 {
		f.Lines = append(f.Lines, "", "["+section+"]", key+"="+val)
		return
	}
	f.Lines = append(f.Lines[:end], append([]string{key + "=" + val}, f.Lines[end:]...)...)
}

// ---- a2mgr allods2.cfg ("key value" per line; '#', ';' and '//' comments) ----

func cfgKV(line string) (key, val string, ok bool) {
	t := line
	if i := strings.IndexAny(t, "#;"); i >= 0 {
		t = t[:i]
	}
	if i := strings.Index(t, "//"); i >= 0 {
		t = t[:i]
	}
	fs := strings.Fields(t)
	if len(fs) == 0 || len(fs) > 2 {
		return "", "", false
	}
	if len(fs) == 1 {
		return fs[0], "", true
	}
	return fs[0], strings.Trim(fs[1], `"`), true
}

// CfgGet returns the last value of key (a2mgr applies lines in order).
func (f *TextFile) CfgGet(key string) (string, bool) {
	val, found := "", false
	for _, l := range f.Lines {
		if k, v, ok := cfgKV(l); ok && k == key {
			val, found = v, true
		}
	}
	return val, found
}

// CfgSet sets key (val == Unset comments out every line of key).
func (f *TextFile) CfgSet(key, val string) {
	done := false
	for i, l := range f.Lines {
		k, _, ok := cfgKV(l)
		if !ok || k != key {
			continue
		}
		f.dirty = true
		if val == Unset || done {
			f.Lines[i] = "# " + l
			continue
		}
		f.Lines[i], done = key+" "+val, true
	}
	if !done && val != Unset {
		f.Lines, f.dirty = append(f.Lines, key+" "+val), true
	}
}

// Unset as a setting value means "key absent" (game default).
const Unset = "\x00unset"

// BackupFiles copies existing files into dir (created), for undo.
func BackupFiles(dir string, paths ...string) error {
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, filepath.Base(p)), b, 0o644); err != nil {
			return err
		}
	}
	return nil
}
