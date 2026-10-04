package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func strptr(s string) *string { return &s }
func TestRuntimeSchemaUnknownFieldsAndLegacy(t *testing.T) {
	var old Config
	if err := decodeJSON([]byte(`{"schemaVersion":1,"apps":[{"id":"a","path":"/bin/tool","runtime":{"homeMode":"inherit","futureRuntime":{"x":1}}}],"runtimeDefaults":{"futureDefaults":true},"futureTop":3}`), &old); err != nil {
		t.Fatal(err)
	}
	if err := validateConfig(&old, false); err != nil {
		t.Fatal(err)
	}
	next := defaultConfig()
	next.RuntimeDefaults = &RuntimeDefaults{}
	next.Apps = []App{{ID: "a", Path: "/bin/tool", Runtime: &RuntimeSettings{}}}
	mergeUnknown(&next, old)
	if err := validateConfig(&next, false); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(next)
	if err != nil {
		t.Fatal(err)
	}
	var round Config
	if err = json.Unmarshal(b, &round); err != nil {
		t.Fatal(err)
	}
	if string(round.RuntimeDefaults.Extra["futureDefaults"]) != "true" || string(round.Apps[0].Runtime.Extra["futureRuntime"]) != `{"x":1}` || string(round.Extra["futureTop"]) != "3" {
		t.Fatal(string(b))
	}
	legacy := defaultConfig()
	legacy.Apps = []App{{ID: "a", Path: "/bin/tool"}}
	if err = validateConfig(&legacy, false); err != nil {
		t.Fatal(err)
	}
	b, _ = json.Marshal(legacy)
	if strings.Contains(string(b), "runtime") {
		t.Fatal("legacy config gained runtime settings", string(b))
	}
}
func TestRuntimeStrictTypesAndReservedFields(t *testing.T) {
	for _, bad := range []string{`{"runtimeDefaults":null}`, `{"runtimeDefaults":{"env":null}}`, `{"runtimeDefaults":{"pathDirs":null}}`, `{"runtimeDefaults":{"env":{"A":false}}}`, `{"apps":[{"runtime":null}]}`, `{"apps":[{"runtime":{"home":null}}]}`, `{"apps":[{"runtime":{"useDefaults":"true"}}]}`} {
		var c Config
		if err := json.Unmarshal([]byte(bad), &c); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
	for _, key := range []string{"HOME", "PATH", "XDG_CONFIG_HOME", "XDG_OTHER", "BINARY_MANAGER_OWNER_TOKEN", "BINARY_MANAGER_ROOT", "A=B", "1INVALID", "BAD KEY", strings.Repeat("A", 129)} {
		if err := validateEnv(map[string]*string{key: strptr("x")}); err == nil {
			t.Errorf("accepted %s", key)
		}
	}
	for _, value := range []string{"a\x00b", "a\nb", "a\rb", strings.Repeat("x", 16385)} {
		if err := validateEnv(map[string]*string{"OK": &value}); err == nil {
			t.Error("accepted invalid value")
		}
	}
	for _, path := range []string{"", "relative", "/a:bad", "/a/../b"} {
		if err := validatePathDirs([]string{path}); err == nil {
			t.Error("accepted PATH", path)
		}
	}
	if err := validateEnv(map[string]*string{"EMPTY": strptr(""), "UNSET": nil}); err != nil {
		t.Fatal(err)
	}
}
func TestRuntimeEnvironmentPrecedenceAndIsolation(t *testing.T) {
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("HOME", "/legacy/home")
	t.Setenv("XDG_CONFIG_HOME", "/legacy/xdg")
	t.Setenv("BASE", "base")
	t.Setenv("REMOVE", "base")
	t.Setenv("BINARY_MANAGER_OTHER", "internal")
	d := &RuntimeDefaults{StorageRoot: "/mnt/data/instances", PathDirs: []string{"/global/bin"}, Env: map[string]*string{"COLOR": strptr("default"), "BASE": nil, "REMOVE": strptr("default"), "SHARED": strptr("yes")}}
	a := App{ID: "one", Runtime: &RuntimeSettings{HomeMode: "managed", UseDefaults: true, PathDirs: []string{"/instance/bin"}, Env: map[string]*string{"COLOR": strptr("instance"), "REMOVE": nil, "EMPTY": strptr("")}}}
	e := configuredEnvironment(a, d)
	if e["HOME"] != "/mnt/data/instances/one/home" || e["PATH"] != "/instance/bin:/global/bin:/usr/bin:/bin" || e["COLOR"] != "instance" || e["SHARED"] != "yes" || e["XDG_CONFIG_HOME"] != "/legacy/xdg" {
		t.Fatal(e)
	}
	for _, k := range []string{"BASE", "REMOVE", "BINARY_MANAGER_OTHER"} {
		if _, ok := e[k]; ok {
			t.Fatal("not unset", k)
		}
	}
	b := a
	b.ID = "two"
	if configuredEnvironment(b, d)["HOME"] == e["HOME"] {
		t.Fatal("managed homes collide")
	}
	a.Runtime.UseDefaults = false
	e = configuredEnvironment(a, d)
	if e["BASE"] != "base" || e["PATH"] != "/instance/bin:/usr/bin:/bin" || e["SHARED"] != "" {
		t.Fatal(e)
	}
	legacy := configuredEnvironment(App{ID: "legacy"}, d)
	if legacy["HOME"] != "/legacy/home" || legacy["PATH"] != "/usr/bin:/bin" || legacy["REMOVE"] != "base" {
		t.Fatal(legacy)
	}
}
func TestProtectedEnvironmentLiteralParserAndRedaction(t *testing.T) {
	marker := "private-secret-marker"
	parsed, err := parseProtectedEnv([]byte("# comment\nTOKEN=" + marker + "\nEMPTY=\nLITERAL=$(touch /should-not-exist); `id` '$HOME'\nEQUAL=a=b\n"))
	if err != nil {
		t.Fatal(err)
	}
	if *parsed["TOKEN"] != marker || *parsed["EMPTY"] != "" || *parsed["EQUAL"] != "a=b" || !strings.Contains(*parsed["LITERAL"], "$(touch") {
		t.Fatal("parser expanded input")
	}
	for _, b := range []string{"export TOKEN=" + marker, "TOKEN", "TOKEN=" + marker + "\nTOKEN=duplicate", "HOME=" + marker, "BINARY_MANAGER_OWNER_TOKEN=" + marker, "XDG_CONFIG_HOME=" + marker, "TOKEN=" + marker + "\r\n", "TOKEN=" + marker + "\x00", strings.Repeat(marker, 6000)} {
		if _, err := parseProtectedEnv([]byte(b)); err == nil || strings.Contains(err.Error(), marker) {
			t.Fatal("unsafe parser or error", err)
		}
	}
	dst := map[string]string{"TOKEN": "ordinary"}
	overlayEnv(dst, parsed)
	if dst["TOKEN"] != marker {
		t.Fatal("protected file did not win")
	}
}
func TestProtectedEnvironmentFileGuards(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secrets.env")
	marker := "never-print-this-secret"
	if err := os.WriteFile(path, []byte("TOKEN="+marker), 0600); err != nil {
		t.Fatal(err)
	}
	if os.Geteuid() != 0 {
		if _, err := readProtectedEnv(path); err == nil || strings.Contains(err.Error(), marker) {
			t.Fatal("accepted non-root file", err)
		}
	} else {
		if e, err := readProtectedEnv(path); err != nil || *e["TOKEN"] != marker {
			t.Fatal(err)
		}
	}
	for _, mode := range []os.FileMode{0644, 0660, 0700} {
		_ = os.Chmod(path, mode)
		if _, err := readProtectedEnv(path); err == nil {
			t.Fatal("accepted insecure mode", mode)
		}
	}
	_ = os.Chmod(path, 0600)
	link := filepath.Join(dir, "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readProtectedEnv(link); err == nil {
		t.Fatal("accepted symlink")
	}
	parentLink := filepath.Join(t.TempDir(), "parent")
	_ = os.Symlink(dir, parentLink)
	if _, err := readProtectedEnv(filepath.Join(parentLink, "secrets.env")); err == nil {
		t.Fatal("accepted symlink ancestor")
	}
	fifo := filepath.Join(dir, "fifo")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readProtectedEnv(fifo); err == nil {
		t.Fatal("accepted FIFO")
	}
}
func TestRuntimeMountSelectionRejectsFallback(t *testing.T) {
	mounts, err := parseMountInfo([]byte("1 0 0:1 / / rw - tmpfs root rw\n2 1 8:1 / /boot rw - vfat /dev/sda1 rw\n3 1 8:2 / /mnt/pool\\040one rw - ext4 /dev/sdb1 rw\n4 1 0:5 / /mnt/ram rw - tmpfs none rw\n5 1 0:6 / /mnt/virtual rw - fuse.mergerfs pool rw\n6 1 8:1 / /mnt/boot-bind rw - ext4 /dev/sda1 rw\n"))
	if err != nil {
		t.Fatal(err)
	}
	if m, err := persistentMount("/mnt/pool one/instances/a/home", mounts); err != nil || m.ID != "3" {
		t.Fatal(m, err)
	}
	for _, p := range []string{"/mnt/missing/a", "/mnt/pool onex/a", "/mnt/ram/a", "/mnt/virtual/a", "/boot/a", "/mnt/boot-bind/a", "/tmp/a"} {
		if _, err := persistentMount(p, mounts); err == nil {
			t.Fatal("accepted", p)
		}
	}
	// Nearest mount wins: a tmpfs nested inside a real pool must still fail.
	mounts = append(mounts, mountRecord{ID: "7", Device: "0:7", Point: "/mnt/pool one/nested", FSType: "tmpfs"})
	if _, err := persistentMount("/mnt/pool one/nested/a", mounts); err == nil {
		t.Fatal("ignored RAM submount")
	}
	real, err := readMounts()
	if err != nil || len(real) == 0 {
		t.Fatal("cannot inspect actual kernel mounts", err)
	}
}

// Mount metadata is injected only into this in-process harness. These tests
// verify real no-follow dirfd operations without mounting disks in the sandbox.
func simulatedPool(t *testing.T) (string, []mountRecord) {
	t.Helper()
	base := os.Getenv("BINARY_MANAGER_TEST_STORAGE")
	if base == "" {
		base = "/workspace/shared"
		if _, err := os.Stat(base); err != nil {
			base = os.TempDir()
		}
	}
	root, err := os.MkdirTemp(base, "bm-pool-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	f, err := os.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	id, err := fdMountID(f)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	if pathWithin(root, "/tmp") {
		t.Skip("set BINARY_MANAGER_TEST_STORAGE to a writable non-system directory for mount simulation")
	}
	return root, []mountRecord{{ID: "root", Device: "system", Point: "/", FSType: "tmpfs"}, {ID: id, Device: "pool", Point: root, FSType: "ext4"}}
}
func TestManagedHomeCreationPrivateNoLinksNoFallback(t *testing.T) {
	pool, mounts := simulatedPool(t)
	storage := filepath.Join(pool, "instances")
	home := filepath.Join(storage, "a", "home")
	if err := persistentHome(home, storage, false, mounts); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(storage); !os.IsNotExist(err) {
		t.Fatal("validation created state")
	}
	if err := persistentHome(home, storage, true, nil); err == nil {
		t.Fatal("accepted missing mount")
	}
	if _, err := os.Stat(storage); !os.IsNotExist(err) {
		t.Fatal("missing mount created state")
	}
	changed := append([]mountRecord{}, mounts...)
	changed[1].ID = "wrong"
	if err := persistentHome(home, storage, true, changed); err == nil {
		t.Fatal("accepted changed mount ID")
	}
	if _, err := os.Stat(storage); !os.IsNotExist(err) {
		t.Fatal("changed mount created state")
	}
	if err := persistentHome(home, storage, true, mounts); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{storage, filepath.Dir(home), home} {
		st, err := os.Stat(p)
		if err != nil || st.Mode().Perm() != 0700 {
			t.Fatal(p, err)
		}
	}
	target := filepath.Join(pool, "target")
	_ = os.Mkdir(target, 0700)
	link := filepath.Join(storage, "link")
	_ = os.Symlink(target, link)
	if err := persistentHome(filepath.Join(link, "home"), storage, true, mounts); err == nil {
		t.Fatal("followed symlink")
	}
	if _, err := os.Stat(filepath.Join(target, "home")); !os.IsNotExist(err) {
		t.Fatal("wrote through symlink")
	}
	_ = os.Chmod(home, 0755)
	if err := persistentHome(home, storage, true, mounts); err == nil {
		t.Fatal("accepted public managed HOME")
	}
	if err := persistentHome(filepath.Join(pool, "custom-missing"), "", true, mounts); err == nil {
		t.Fatal("created custom HOME")
	}
}
func TestRuntimeDefaultsExecutionComparison(t *testing.T) {
	a := App{ID: "a", Path: "/bin/a", Runtime: &RuntimeSettings{HomeMode: "inherit", UseDefaults: true, Env: map[string]*string{"OVERRIDE": strptr("own")}}}
	d := &RuntimeDefaults{StorageRoot: "/mnt/one", Env: map[string]*string{"OVERRIDE": strptr("old"), "EFFECTIVE": strptr("one")}}
	n := &RuntimeDefaults{StorageRoot: "/mnt/two", Env: map[string]*string{"OVERRIDE": strptr("new"), "EFFECTIVE": strptr("one")}}
	if !sameExecution(a, d, a, n) {
		t.Fatal("unused defaults changed execution")
	}
	n.Env["EFFECTIVE"] = strptr("two")
	if sameExecution(a, d, a, n) {
		t.Fatal("effective default change ignored")
	}
	a.Runtime.UseDefaults = false
	if !sameExecution(a, d, a, n) {
		t.Fatal("opted-out defaults changed execution")
	}
	a.Runtime.HomeMode = "managed"
	if sameExecution(a, d, a, n) {
		t.Fatal("managed storage root ignored")
	}
}
func TestCoreDuplicateExecutableHomeAndLifecycleIsolation(t *testing.T) {
	h := newCore(t)
	pool, mounts := simulatedPool(t)
	h.s.runtimeMounts = func() ([]mountRecord, error) { return mounts, nil }
	app := fixtureApp(t, "one", "environment", filepath.Join(pool, "one.json"))
	app.Runtime = &RuntimeSettings{HomeMode: "managed", Env: map[string]*string{"INSTANCE_VALUE": strptr("one"), "PATH_MARKER": strptr("literal")}, PathDirs: []string{"/custom/one/bin"}}
	other := app
	other.ID = "two"
	other.Args = append([]string{}, app.Args...)
	other.Args[len(other.Args)-1] = filepath.Join(pool, "two.json")
	other.Runtime = &RuntimeSettings{HomeMode: "managed", Env: map[string]*string{"INSTANCE_VALUE": strptr("two")}, PathDirs: []string{"/custom/two/bin"}}
	cfg := defaultConfig()
	cfg.RuntimeDefaults = &RuntimeDefaults{StorageRoot: filepath.Join(pool, "homes")}
	cfg.Apps = []App{app, other}
	saveCoreConfig(t, h, cfg)
	first := h.toggle("one", true)
	second := h.toggle("two", true)
	pid1 := appStatus(t, first, "one")["pid"]
	pid2 := appStatus(t, second, "two")["pid"]
	if pid1 == pid2 {
		t.Fatal("duplicate shared PID")
	}
	for _, id := range []string{"one", "two"} {
		output := filepath.Join(pool, id+".json")
		eventually(t, 3*time.Second, func() bool { _, e := os.Stat(output); return e == nil })
		b, _ := os.ReadFile(output)
		var env map[string]string
		if err := json.Unmarshal(b, &env); err != nil {
			t.Fatal(err)
		}
		home := filepath.Join(pool, "homes", id, "home")
		if env["HOME"] != home || env["INSTANCE_VALUE"] != id || !strings.HasPrefix(env["PATH"], "/custom/"+id+"/bin:") || env["cwd"] != app.Workdir {
			t.Fatal(env)
		}
		dot, err := os.ReadFile(filepath.Join(home, ".instance"))
		if err != nil || string(dot) != id {
			t.Fatal("HOME dotfile mismatch", err)
		}
	}
	h.toggle("one", false)
	if appStatus(t, h.status(), "two")["pid"] != pid2 {
		t.Fatal("stopping one affected duplicate")
	}
	h.ok(Request{Action: "restart", ID: "one"})
	if appStatus(t, h.status(), "two")["pid"] != pid2 {
		t.Fatal("restart affected duplicate")
	}
	rev := int64(h.status()["config"].(map[string]any)["revision"].(float64))
	h.ok(Request{Action: "remove", ID: "one", ExpectedRevision: &rev})
	if appStatus(t, h.status(), "two")["pid"] != pid2 {
		t.Fatal("remove affected duplicate")
	}
	if b, err := os.ReadFile(filepath.Join(pool, "homes", "one", "home", ".instance")); err != nil || string(b) != "one" {
		t.Fatal("remove deleted persistent HOME data", err)
	}
}
func saveCoreConfig(t *testing.T, h *coreHarness, c Config) map[string]any {
	t.Helper()
	rev := int64(h.status()["config"].(map[string]any)["revision"].(float64))
	b, _ := json.Marshal(c)
	return h.ok(Request{Action: "save", Config: b, ExpectedRevision: &rev})
}
func TestCoreRuntimeSaveRestartsOnlyAffectedAndRetainsCAS(t *testing.T) {
	h := newCore(t)
	first := fixtureApp(t, "first", "wait")
	first.Runtime = &RuntimeSettings{HomeMode: "inherit", UseDefaults: true}
	second := first
	second.ID = "second"
	second.Runtime = &RuntimeSettings{HomeMode: "inherit", UseDefaults: false}
	legacy := first
	legacy.ID = "legacy"
	legacy.Runtime = nil
	stopped := first
	stopped.ID = "stopped"
	cfg := defaultConfig()
	cfg.RuntimeDefaults = &RuntimeDefaults{Env: map[string]*string{"FEATURE": strptr("one")}, Extra: map[string]json.RawMessage{"future": json.RawMessage(`true`)}}
	cfg.Apps = []App{first, second, legacy, stopped}
	first.Runtime.Extra = map[string]json.RawMessage{"future": json.RawMessage(`42`)}
	saveCoreConfig(t, h, cfg)
	for _, id := range []string{"first", "second", "legacy"} {
		h.toggle(id, true)
	}
	before := h.status()
	cfg.RuntimeDefaults.Env["FEATURE"] = strptr("two")
	cfg.RuntimeDefaults.Extra = nil
	cfg.Apps[0].Runtime.Extra = nil
	after := saveCoreConfig(t, h, cfg)
	if appStatus(t, before, "first")["pid"] == appStatus(t, after, "first")["pid"] {
		t.Fatal("default edit did not restart opted-in app")
	}
	for _, id := range []string{"second", "legacy"} {
		if appStatus(t, before, id)["pid"] != appStatus(t, after, id)["pid"] {
			t.Fatal("restarted unaffected", id)
		}
	}
	if appStatus(t, after, "stopped")["running"] != false {
		t.Fatal("save started stopped app")
	}
	if after["config"].(map[string]any)["runtimeDefaults"].(map[string]any)["future"] != true {
		t.Fatal("lost default unknown field")
	}
	stale := int64(1)
	b, _ := json.Marshal(cfg)
	if out := h.request(Request{Action: "save", Config: b, ExpectedRevision: &stale}); out["code"] != "REVISION_CONFLICT" {
		t.Fatal(out)
	}
	before = after
	cfg.Apps[0].Name = "renamed"
	after = saveCoreConfig(t, h, cfg)
	if appStatus(t, before, "first")["pid"] != appStatus(t, after, "first")["pid"] {
		t.Fatal("name edit restarted app")
	}
}
func TestPrivateLaunchPipeNoArgvLeakOrUnauthorizedExecution(t *testing.T) {
	marker := "secret-pipe-marker"
	output := filepath.Join(t.TempDir(), "result.json")
	app := fixtureApp(t, "launch", "environment", output)
	secret, err := parseProtectedEnv([]byte("INSTANCE_VALUE=" + marker))
	if err != nil {
		t.Fatal(err)
	}
	env := envMap(baseEnvironment())
	overlayEnv(env, secret)
	env["HOME"] = t.TempDir()
	payload, _ := json.Marshal(launchConfig{Path: app.Path, Args: app.Args, Workdir: app.Workdir, Env: sortedEnv(env)})
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	self, _ := os.Executable()
	cmd := exec.Command(self, "__child")
	cmd.ExtraFiles = []*os.File{r}
	cmd.Env = baseEnvironment()
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	r.Close()
	defer func() { cmd.Process.Kill(); cmd.Wait() }()
	var cmdline []byte
	eventually(t, time.Second, func() bool {
		cmdline, err = os.ReadFile("/proc/" + strconv.Itoa(cmd.Process.Pid) + "/cmdline")
		return err == nil && len(strings.Split(strings.TrimRight(string(cmdline), "\x00"), "\x00")) == 2 && strings.Contains(string(cmdline), "__child")
	})
	if strings.Contains(string(cmdline), marker) {
		t.Fatal("unsafe child argv")
	}
	if _, err = os.Stat(output); !os.IsNotExist(err) {
		t.Fatal("child executed before authorization")
	}
	if _, err = w.Write(append([]byte{1}, payload...)); err != nil {
		t.Fatal(err)
	}
	w.Close()
	eventually(t, 3*time.Second, func() bool { _, e := os.Stat(output); return e == nil })
	cmdline, _ = os.ReadFile("/proc/" + strconv.Itoa(cmd.Process.Pid) + "/cmdline")
	if strings.Contains(string(cmdline), marker) {
		t.Fatal("secret leaked into executable argv")
	}
	result, _ := os.ReadFile(output)
	var got map[string]string
	_ = json.Unmarshal(result, &got)
	if got["INSTANCE_VALUE"] != marker {
		t.Fatal("private launch environment missing")
	}
}
func TestProtectedEnvFailureNeverExposesBytesInStatus(t *testing.T) {
	h := newCore(t)
	app := fixtureApp(t, "envfile", "wait")
	h.save(app)
	path := filepath.Join(t.TempDir(), "secret")
	marker := "status-secret-marker"
	_ = os.WriteFile(path, []byte("BAD ENTRY="+marker), 0644)
	app.Runtime = &RuntimeSettings{HomeMode: "inherit", EnvFile: path}
	cfg := defaultConfig()
	cfg.Apps = []App{app}
	rev := int64(h.status()["config"].(map[string]any)["revision"].(float64))
	b, _ := json.Marshal(cfg)
	result := h.request(Request{Action: "save", Config: b, ExpectedRevision: &rev})
	out, _ := json.Marshal(result)
	if result["ok"] != false || strings.Contains(string(out), marker) {
		t.Fatal("unsafe save response")
	}
	// An externally changed, now-invalid protected file still fails closed at Start.
	_ = atomicJSON(h.p.Config, cfg)
	status := h.toggle("envfile", true)
	out, _ = json.Marshal(status)
	if strings.Contains(string(out), marker) || appStatus(t, status, "envfile")["running"] != false {
		t.Fatal("unsafe failed launch")
	}
}
func TestRuntimeJSONNormalizationIsStable(t *testing.T) {
	c := defaultConfig()
	c.RuntimeDefaults = &RuntimeDefaults{}
	c.Apps = []App{{ID: "a", Path: "/bin/a", Runtime: &RuntimeSettings{}}}
	if err := validateConfig(&c, false); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(c)
	var d Config
	if err := json.Unmarshal(b, &d); err != nil {
		t.Fatal(err)
	}
	if err := validateConfig(&d, false); err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(d)
	if !reflect.DeepEqual(b, after) {
		t.Fatal("normalization unstable")
	}
}

func TestRootProtectedEnvFileEndToEnd(t *testing.T) {
	if os.Geteuid() != 0 {
		if os.Getenv("BINARY_MANAGER_REQUIRE_ROOT_TEST") == "1" {
			t.Fatal("protected-file acceptance test requires root")
		}
		t.Skip("root-owned protected-file acceptance is exercised by the explicit root CI step")
	}
	h := newCore(t)
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	path := filepath.Join(dir, "private.env")
	output := filepath.Join(dir, "result.json")
	marker := "root-secret-marker-unique"
	commandMarker := filepath.Join(dir, "MUST_NOT_EXIST")
	content := "INSTANCE_VALUE=" + marker + "\nFEATURE=protected\nLITERAL=$(touch " + commandMarker + ")\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	app := fixtureApp(t, "private", "environment", output)
	app.Runtime = &RuntimeSettings{HomeMode: "inherit", UseDefaults: true, EnvFile: path}
	cfg := defaultConfig()
	cfg.RuntimeDefaults = &RuntimeDefaults{Env: map[string]*string{"FEATURE": strptr("old-default"), "INSTANCE_VALUE": strptr("ordinary")}}
	cfg.Apps = []App{app}
	saveCoreConfig(t, h, cfg)
	status := h.toggle(app.ID, true)
	eventually(t, 3*time.Second, func() bool { _, err := os.Stat(output); return err == nil })
	result, _ := os.ReadFile(output)
	var got map[string]string
	_ = json.Unmarshal(result, &got)
	if got["INSTANCE_VALUE"] != marker {
		t.Fatal("protected file did not reach launched process")
	}
	pid := appStatus(t, status, app.ID)["pid"]
	assertRedacted := func(status map[string]any) {
		t.Helper()
		b, _ := json.Marshal(status)
		state, _ := os.ReadFile(h.p.State)
		settings, _ := os.ReadFile(h.p.Config)
		cmdline, _ := os.ReadFile("/proc/" + strconv.Itoa(int(pid.(float64))) + "/cmdline")
		for _, data := range [][]byte{b, state, settings, cmdline} {
			if strings.Contains(string(data), marker) {
				t.Fatal("protected value leaked into metadata, state, settings or argv")
			}
		}
	}
	assertRedacted(status)
	if _, err := os.Stat(commandMarker); !os.IsNotExist(err) {
		t.Fatal("protected value was evaluated")
	}
	values, err := readProtectedEnv(path)
	if err != nil || *values["LITERAL"] != "$(touch "+commandMarker+")" {
		t.Fatal("literal protected entry changed")
	}
	_ = os.Chmod(path, 0400)
	if _, err := readProtectedEnv(path); err != nil {
		t.Fatal("0400 protected file rejected", err)
	}
	_ = os.Chmod(path, 0600)
	// Only key names from the launched file are used to mask irrelevant defaults.
	cfg.RuntimeDefaults.Env["FEATURE"] = strptr("new-default")
	cfg.RuntimeDefaults.Env["INSTANCE_VALUE"] = strptr("new-ordinary")
	status = saveCoreConfig(t, h, cfg)
	if appStatus(t, status, app.ID)["pid"] != pid {
		t.Fatal("masked default changed restarted the process")
	}
	assertRedacted(status)
	cfg.Apps[0].Runtime.Env = map[string]*string{"FEATURE": strptr("instance-change")}
	status = saveCoreConfig(t, h, cfg)
	if appStatus(t, status, app.ID)["pid"] != pid {
		t.Fatal("masked instance change restarted the process")
	}
	// An unrelated rename must not re-read an existing file that became invalid.
	_ = os.WriteFile(path, []byte("TOKEN="+marker+"\nINVALID ENTRY="+marker), 0600)
	cfg.Apps[0].Name = "renamed"
	status = saveCoreConfig(t, h, cfg)
	if appStatus(t, status, app.ID)["pid"] != pid {
		t.Fatal("name edit restarted the process")
	}
	assertRedacted(status)
	status = h.ok(Request{Action: "restart", ID: app.ID})
	if appStatus(t, status, app.ID)["running"] != false {
		t.Fatal("invalid protected file still launched")
	}
	assertRedacted(status)
	// Bounds and regular-file metadata are checked on the opened descriptor.
	_ = os.WriteFile(path, []byte(strings.Repeat("x", maxEnvFile+1)), 0600)
	if _, err := readProtectedEnv(path); err == nil {
		t.Fatal("accepted oversized protected file")
	}
	_ = os.WriteFile(path, []byte(content), 0600)
	_ = os.Chmod(path, 0644)
	if _, err := readProtectedEnv(path); err == nil {
		t.Fatal("accepted world-readable secret")
	}
	_ = os.Chmod(path, 0600)
	link := filepath.Join(dir, "hardlink")
	if err := os.Link(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readProtectedEnv(path); err == nil {
		t.Fatal("accepted multiple-link protected file")
	}
}
func TestCoreRuntimeSafetyInputChangesValidatedEvenWithIdenticalEnv(t *testing.T) {
	h := newCore(t)
	invalid := t.TempDir()
	t.Setenv("HOME", invalid)
	t.Setenv("XDG_CONFIG_HOME", invalid)
	app := fixtureApp(t, "safety", "wait")
	app.Runtime = &RuntimeSettings{HomeMode: "inherit"}
	cfg := defaultConfig()
	cfg.Apps = []App{app}
	saveCoreConfig(t, h, cfg)
	app.Runtime = &RuntimeSettings{HomeMode: "custom", Home: invalid}
	cfg.Apps = []App{app}
	if !sameExecution(app, nil, h.s.cfg.Apps[0], nil) {
		t.Fatal("fixture must have identical effective environment")
	}
	rev := int64(h.status()["config"].(map[string]any)["revision"].(float64))
	b, _ := json.Marshal(cfg)
	if out := h.request(Request{Action: "save", Config: b, ExpectedRevision: &rev}); out["code"] != "INVALID_CONFIG" {
		t.Fatal("new unsafe custom HOME was not validated", out)
	}
	app.Runtime = &RuntimeSettings{HomeMode: "inherit", XDGConfigHome: invalid}
	cfg.Apps = []App{app}
	b, _ = json.Marshal(cfg)
	if out := h.request(Request{Action: "save", Config: b, ExpectedRevision: &rev}); out["code"] != "INVALID_CONFIG" {
		t.Fatal("new unsafe XDG path was not validated", out)
	}
}
func TestManagedHomeNestedMountCannotBypassPrivacy(t *testing.T) {
	pool, mounts := simulatedPool(t)
	root := filepath.Join(pool, "instances")
	home := filepath.Join(root, "a", "home")
	_ = os.MkdirAll(home, 0755)
	mounts = append(mounts, mountRecord{ID: mounts[1].ID, Device: "nested", Point: home, FSType: "ext4"})
	if err := persistentHome(home, root, true, mounts); err == nil {
		t.Fatal("nested mount bypassed managed tree checks")
	}
}
func TestCoreMissingActualPoolNeverCreatesDirectory(t *testing.T) {
	h := newCore(t)
	base := t.TempDir()
	root := filepath.Join(base, "not-mounted", "instances")
	cfg := defaultConfig()
	cfg.RuntimeDefaults = &RuntimeDefaults{StorageRoot: root}
	app := fixtureApp(t, "missing-pool", "wait")
	app.Runtime = &RuntimeSettings{HomeMode: "managed"}
	cfg.Apps = []App{app}
	rev := int64(0)
	b, _ := json.Marshal(cfg)
	out := h.request(Request{Action: "save", Config: b, ExpectedRevision: &rev})
	if out["code"] != "INVALID_CONFIG" {
		t.Fatal("save accepted missing actual mount", out)
	}
	if _, err := os.Stat(filepath.Dir(root)); !os.IsNotExist(err) {
		t.Fatal("missing pool created directories")
	}
	// Direct external settings cannot bypass the same fail-closed launch check.
	if err := validateConfig(&cfg, false); err != nil {
		t.Fatal(err)
	}
	if err := atomicJSON(h.p.Config, cfg); err != nil {
		t.Fatal(err)
	}
	out = h.toggle(app.ID, true)
	if appStatus(t, out, app.ID)["running"] != false {
		t.Fatal("missing pool launched")
	}
	if _, err := os.Stat(filepath.Dir(root)); !os.IsNotExist(err) {
		t.Fatal("launch created fallback directories")
	}
}
