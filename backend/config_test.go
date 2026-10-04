package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func executableFixture(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	return p
}
func TestConfigValidationAndUnknownRoundtrip(t *testing.T) {
	p := executableFixture(t, "a file 'quoted' ;&.sh")
	c := defaultConfig()
	c.Apps = []App{{ID: "app-1", Path: p, Args: []string{"literal $HOME", "; & | ' \""}}}
	if err := validateConfig(&c, true); err != nil {
		t.Fatal(err)
	}
	c.Extra = map[string]json.RawMessage{"future": json.RawMessage(`{"v":2}`)}
	c.Apps[0].Extra = map[string]json.RawMessage{"another": json.RawMessage(`true`)}
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var round Config
	if err = json.Unmarshal(b, &round); err != nil {
		t.Fatal(err)
	}
	if string(round.Extra["future"]) != `{"v":2}` || string(round.Apps[0].Extra["another"]) != "true" {
		t.Fatal(string(b))
	}
	next := defaultConfig()
	next.Apps = []App{{ID: "app-1", Path: p}}
	mergeUnknown(&next, round)
	if len(next.Extra) != 1 || len(next.Apps[0].Extra) != 1 {
		t.Fatal("lost unknown keys")
	}
}
func TestRejectUnsafePathsAndTypes(t *testing.T) {
	p := executableFixture(t, "ok")
	sym := filepath.Join(t.TempDir(), "symlink")
	if err := os.Symlink(p, sym); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"relative", "/tmp/../tmp/a", "/tmp/line\nbreak", "/tmp/zero\x00", sym, filepath.Dir(p)} {
		if err := executablePath(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	for _, bad := range []string{`null`, `[]`, `{"schemaVersion":1,"apps":null}`, `{"schemaVersion":"1","apps":[]}`, `{"schemaVersion":1,"apps":[{"id":"a","path":null}]}`} {
		var c Config
		if err := decodeJSON([]byte(bad), &c); err == nil {
			t.Errorf("accepted invalid type %s", bad)
		}
	}
	var c Config
	if err := decodeJSON([]byte(`{} {}`), &c); err == nil {
		t.Fatal("accepted trailing JSON")
	}
}
func TestMissingExistingFileDoesNotBlockDisable(t *testing.T) {
	p := executableFixture(t, "app")
	old := defaultConfig()
	old.Apps = []App{{ID: "app", Path: p, Autostart: true}}
	next := old
	next.Apps = append([]App(nil), old.Apps...)
	next.Apps[0].Autostart = false
	_ = os.Remove(p)
	if err := validateConfig(&next, false); err != nil {
		t.Fatal(err)
	}
	if err := validateChangedFiles(next, old); err != nil {
		t.Fatal(err)
	}
	next.Apps[0].Path = p + "-new"
	if err := validateChangedFiles(next, old); err == nil {
		t.Fatal("accepted newly missing executable")
	}
}
func TestAtomicSettingsAndBoundedBrowse(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "settings.json")
	c := defaultConfig()
	if err := atomicJSON(p, c); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadConfig(p)
	if err != nil || loaded.SchemaVersion != 1 {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "target")
	if err = os.WriteFile(target, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err = os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err = atomicBytes(link, []byte("changed")); err == nil {
		t.Fatal("overwrote symlink")
	}
	b, _ := os.ReadFile(target)
	if string(b) != "original" {
		t.Fatal("target modified")
	}
	for i := 0; i < 540; i++ {
		f, err := os.CreateTemp(dir, "item-")
		if err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
	result, err := browse(dir)
	if err != nil {
		t.Fatal(err)
	}
	if result["truncated"] != true {
		t.Fatal("browse not bounded")
	}
}
func TestBackoffCap(t *testing.T) {
	for i, want := range []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 30 * time.Second, 30 * time.Second} {
		if got := backoff(i + 1); got != want {
			t.Fatalf("%d: %v != %v", i, got, want)
		}
	}
}
func TestLogMemoryAndDiskBounded(t *testing.T) {
	l := &tailLog{}
	for i := 0; i < 100; i++ {
		p := []byte(strings.Repeat("x", 10000))
		n, err := l.Write(p)
		if n != len(p) || err != nil {
			t.Fatal(n, err)
		}
	}
	l.Write([]byte("THE END"))
	if len(l.snapshot()) != logLimit || !strings.HasSuffix(l.snapshot(), "THE END") {
		t.Fatal("bad log tail")
	}
	path := filepath.Join(t.TempDir(), "app.log")
	if err := l.flush(path); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil || fi.Size() != logLimit {
		t.Fatal(fi, err)
	}
}

func TestStableRunResetsFailureCounter(t *testing.T) {
	root := t.TempDir()
	s := &Supervisor{p: Paths{Runtime: root, State: filepath.Join(root, "runtime.json")}, bootID: "fixture", apps: map[string]*RuntimeApp{}}
	a := &RuntimeApp{App: App{ID: "a"}, Desired: true, Failures: 5, Started: time.Now().Add(-31 * time.Second), Log: &tailLog{}}
	s.apps["a"] = a
	s.exited(a, os.ErrInvalid, nil)
	if a.Failures != 1 || time.Until(a.Next) > time.Second || time.Until(a.Next) < 800*time.Millisecond {
		t.Fatalf("stable failure did not reset backoff: failures=%d next=%v", a.Failures, a.Next)
	}
}
func TestDiscoveryNeverAddsOrRunsApps(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "available")
	if err := os.WriteFile(p, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	c := defaultConfig()
	c.Folder = dir
	s := &Supervisor{cfg: c, apps: map[string]*RuntimeApp{}}
	result := s.status()
	if len(result["apps"].([]map[string]any)) != 0 || len(result["discovered"].([]Entry)) != 1 || len(s.cfg.Apps) != 0 {
		t.Fatal("discovery changed managed app set")
	}
}

func TestNormalizedConfigCannotOutgrowStorageBound(t *testing.T) {
	c := defaultConfig()
	c.Apps = []App{{ID: "app", Path: "/tmp/" + strings.Repeat("x", 120)}}
	c.Extra = map[string]json.RawMessage{"padding": json.RawMessage(`""`)}
	initial, _ := json.Marshal(c)
	padding, _ := json.Marshal(strings.Repeat("p", maxJSON-1-len(initial)))
	c.Extra["padding"] = padding
	before, _ := json.Marshal(c)
	if len(before) != maxJSON-1 {
		t.Fatal("bad boundary fixture", len(before))
	}
	if err := validateConfig(&c, false); err == nil || !strings.Contains(err.Error(), "normalized") {
		t.Fatalf("normalization overflow was not rejected: %v", err)
	}
	c.Extra["padding"] = json.RawMessage(`"small"`)
	if err := validateConfig(&c, false); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "settings.json")
	if err := atomicJSON(p, c); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(p); err != nil {
		t.Fatal(err)
	}
}
