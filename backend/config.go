package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const maxApps = 32
const maxJSON = 64 * 1024

var validID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

type App struct {
	ID        string                     `json:"id"`
	Path      string                     `json:"path"`
	Name      string                     `json:"name"`
	Args      []string                   `json:"args"`
	Workdir   string                     `json:"workdir"`
	Autostart bool                       `json:"autostart"`
	Extra     map[string]json.RawMessage `json:"-"`
}
type Config struct {
	SchemaVersion int                        `json:"schemaVersion"`
	Revision      int64                      `json:"revision"`
	Folder        string                     `json:"folder"`
	Apps          []App                      `json:"apps"`
	Extra         map[string]json.RawMessage `json:"-"`
}

func (a *App) UnmarshalJSON(b []byte) error {
	type plain App
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	if err := json.Unmarshal(b, &p.Extra); err != nil {
		return err
	}
	if p.Extra == nil {
		return errors.New("app must be an object")
	}
	for _, k := range []string{"id", "path", "name", "args", "workdir", "autostart"} {
		if v, ok := p.Extra[k]; ok && bytes.Equal(v, []byte("null")) {
			return fmt.Errorf("%s cannot be null", k)
		}
		delete(p.Extra, k)
	}
	*a = App(p)
	return nil
}
func (a App) MarshalJSON() ([]byte, error) {
	type plain App
	return marshalExtras(plain(a), a.Extra)
}
func (c *Config) UnmarshalJSON(b []byte) error {
	type plain Config
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	if err := json.Unmarshal(b, &p.Extra); err != nil {
		return err
	}
	if p.Extra == nil {
		return errors.New("config must be an object")
	}
	for _, k := range []string{"schemaVersion", "revision", "folder", "apps"} {
		if v, ok := p.Extra[k]; ok && bytes.Equal(v, []byte("null")) {
			return fmt.Errorf("%s cannot be null", k)
		}
		delete(p.Extra, k)
	}
	*c = Config(p)
	return nil
}
func (c Config) MarshalJSON() ([]byte, error) {
	type plain Config
	return marshalExtras(plain(c), c.Extra)
}
func marshalExtras(v any, extra map[string]json.RawMessage) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	m := map[string]json.RawMessage{}
	if err = json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	for k, v := range extra {
		if _, exists := m[k]; !exists {
			m[k] = v
		}
	}
	return json.Marshal(m)
}
func defaultConfig() Config {
	return Config{SchemaVersion: 1, Apps: []App{}, Extra: map[string]json.RawMessage{}}
}
func decodeJSON(b []byte, v any) error {
	if len(b) > maxJSON {
		return errors.New("JSON exceeds 64 KiB limit")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	if err := d.Decode(v); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return errors.New("expected one JSON object")
	}
	return nil
}
func validAbsolute(p string, empty bool) error {
	if empty && p == "" {
		return nil
	}
	if p == "" || !filepath.IsAbs(p) || filepath.Clean(p) != p || len(p) > 4096 || strings.ContainsAny(p, "\x00\r\n") {
		return errors.New("path must be a clean absolute path without control characters")
	}
	for _, r := range p {
		if r < 32 || r == 127 {
			return errors.New("path contains control characters")
		}
	}
	return nil
}
func executablePath(p string) error {
	if err := validAbsolute(p, false); err != nil {
		return err
	}
	fi, err := os.Lstat(p)
	if err != nil {
		return fmt.Errorf("executable: %w", err)
	}
	if !fi.Mode().IsRegular() || fi.Mode().Perm()&0111 == 0 {
		return errors.New("path must resolve to a regular executable file")
	}
	return nil
}
func directoryPath(p string) error {
	if err := validAbsolute(p, false); err != nil {
		return err
	}
	fi, err := os.Stat(p)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return errors.New("path is not a directory")
	}
	return nil
}
func validateConfig(c *Config, files bool) error {
	if b, err := json.Marshal(c); err != nil || len(b) >= maxJSON {
		return errors.New("config exceeds 64 KiB or is invalid JSON")
	}
	if c.SchemaVersion != 1 {
		return errors.New("unsupported schemaVersion (expected 1)")
	}
	if c.Revision < 0 {
		return errors.New("revision cannot be negative")
	}
	if err := validAbsolute(c.Folder, true); err != nil {
		return fmt.Errorf("folder: %w", err)
	}
	if len(c.Apps) > maxApps {
		return fmt.Errorf("at most %d apps are supported", maxApps)
	}
	if c.Apps == nil {
		c.Apps = []App{}
	}
	ids, paths := map[string]bool{}, map[string]bool{}
	for i := range c.Apps {
		a := &c.Apps[i]
		if !validID.MatchString(a.ID) || ids[a.ID] {
			return errors.New("app ids must be unique, 1–64 letters, numbers, underscores or hyphens")
		}
		ids[a.ID] = true
		if err := validAbsolute(a.Path, false); err != nil {
			return fmt.Errorf("%s: %w", a.ID, err)
		}
		path := a.Path
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			path = resolved
		}
		if paths[path] {
			return errors.New("each executable may be configured only once")
		}
		paths[path] = true
		if a.Name == "" {
			a.Name = filepath.Base(a.Path)
		}
		if len(a.Name) > 160 || strings.ContainsAny(a.Name, "\x00\r\n") {
			return errors.New("name must be at most 160 bytes without newlines")
		}
		if len(a.Args) > 128 {
			return errors.New("at most 128 arguments are supported")
		}
		if a.Args == nil {
			a.Args = []string{}
		}
		total := 0
		for _, arg := range a.Args {
			total += len(arg)
			if len(arg) > 16384 || strings.IndexByte(arg, 0) >= 0 {
				return errors.New("argument exceeds 16 KiB or contains NUL")
			}
		}
		if total > 65536 {
			return errors.New("arguments exceed 64 KiB")
		}
		if err := validAbsolute(a.Workdir, true); err != nil {
			return fmt.Errorf("%s workdir: %w", a.ID, err)
		}
		if files {
			if err := executablePath(a.Path); err != nil {
				return fmt.Errorf("%s: %w", a.ID, err)
			}
			if a.Workdir != "" {
				if err := directoryPath(a.Workdir); err != nil {
					return fmt.Errorf("%s workdir: %w", a.ID, err)
				}
			}
		}
	}
	if b, err := json.Marshal(c); err != nil || len(b) >= maxJSON {
		return errors.New("normalized config exceeds 64 KiB")
	}
	return nil
}

func mergeUnknown(c *Config, old Config) {
	if c.Extra == nil {
		c.Extra = map[string]json.RawMessage{}
	}
	for k, v := range old.Extra {
		if _, ok := c.Extra[k]; !ok {
			c.Extra[k] = v
		}
	}
	byID := map[string]App{}
	for _, a := range old.Apps {
		byID[a.ID] = a
	}
	for i := range c.Apps {
		a := &c.Apps[i]
		if a.Extra == nil {
			a.Extra = map[string]json.RawMessage{}
		}
		for k, v := range byID[a.ID].Extra {
			if _, ok := a.Extra[k]; !ok {
				a.Extra[k] = v
			}
		}
	}
}
func loadConfig(path string) (Config, error) {
	c := defaultConfig()
	b, err := readBounded(path, maxJSON)
	if os.IsNotExist(err) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if err = decodeJSON(b, &c); err != nil {
		return c, err
	}
	err = validateConfig(&c, false)
	return c, err
}
func readBounded(path string, limit int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, int64(limit+1)))
	if err != nil {
		return nil, err
	}
	if len(b) > limit {
		return nil, errors.New("file exceeds limit")
	}
	return b, nil
}
func atomicJSON(path string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return atomicBytes(path, append(b, '\n'))
}
func atomicBytes(path string, b []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if fi, err := os.Lstat(path); err == nil && (!fi.Mode().IsRegular() || fi.Mode()&os.ModeSymlink != 0) {
		return errors.New("refusing non-regular state/settings destination")
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	f, err := os.CreateTemp(dir, ".binary-manager-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(tmp, path); err != nil {
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

type Entry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Type string `json:"type"`
}

func browse(path string) (map[string]any, error) {
	if path == "" {
		path = "/"
	}
	if err := directoryPath(path); err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	entries, err := f.ReadDir(513)
	if err != nil && err != io.EOF {
		return nil, err
	}
	truncated := len(entries) > 512
	if truncated {
		entries = entries[:512]
	}
	out := []Entry{}
	for _, e := range entries {
		p := filepath.Join(path, e.Name())
		if validAbsolute(p, false) != nil {
			continue
		}
		info, err := os.Lstat(p)
		if err != nil {
			continue
		}
		typ := ""
		if info.IsDir() {
			typ = "directory"
		} else if info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0 {
			typ = "executable"
		}
		if typ != "" {
			out = append(out, Entry{e.Name(), p, typ})
		}
	}
	sortEntries(out)
	return map[string]any{"ok": true, "path": path, "parent": filepath.Dir(path), "entries": out, "truncated": truncated}, nil
}

// Existing missing/offline executables do not prevent disabling boot launch or
// editing unrelated entries. New/changed executable and working paths are
// verified now; every actual launch rechecks both.
func validateChangedFiles(c Config, old Config) error {
	prior := map[string]App{}
	for _, a := range old.Apps {
		prior[a.ID] = a
	}
	for _, a := range c.Apps {
		before, exists := prior[a.ID]
		if !exists || a.Path != before.Path {
			if err := executablePath(a.Path); err != nil {
				return fmt.Errorf("%s: %w", a.ID, err)
			}
		}
		if a.Workdir != "" && (!exists || a.Workdir != before.Workdir) {
			if err := directoryPath(a.Workdir); err != nil {
				return fmt.Errorf("%s workdir: %w", a.ID, err)
			}
		}
	}
	return nil
}
