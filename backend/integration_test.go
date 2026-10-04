package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

var integrationBinary string

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "__child" {
		if err := childMain(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(126)
		}
		os.Exit(0)
	}
	// Fixture subprocesses run this same test executable without rebuilding.
	for _, a := range os.Args {
		if a == "--binary-manager-fixture" {
			os.Exit(m.Run())
		}
	}
	dir, err := os.MkdirTemp("", "bm-test-build-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	integrationBinary = filepath.Join(dir, "binary-manager")
	goTool := os.Getenv("BINARY_MANAGER_GO")
	if goTool == "" {
		goTool = "go"
	}
	buildArgs := []string{"build", "-buildvcs=false"}
	if os.Getenv("BINARY_MANAGER_TEST_RACE") == "1" {
		buildArgs = append(buildArgs, "-race")
	}
	buildArgs = append(buildArgs, "-o", integrationBinary, ".")
	cmd := exec.Command(goTool, buildArgs...)
	if b, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build fixture: %v\n%s", err, b)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
func TestFixtureProcess(t *testing.T) {
	marker := -1
	for i, a := range os.Args {
		if a == "--binary-manager-fixture" {
			marker = i
			break
		}
	}
	if marker < 0 {
		return
	}
	args := os.Args[marker+1:]
	if len(args) == 0 {
		os.Exit(91)
	}
	switch args[0] {
	case "wait":
		if len(args) > 1 {
			_ = os.WriteFile(args[1], []byte(strconv.Itoa(os.Getpid())), 0600)
		}
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, syscall.SIGTERM, syscall.SIGINT)
		<-ch
		os.Exit(0)
	case "ignore":
		signal.Ignore(syscall.SIGTERM)
		for {
			time.Sleep(time.Second)
		}
	case "error":
		fmt.Println("intentional failure")
		os.Exit(7)
	case "clean":
		fmt.Println("clean finish")
		os.Exit(0)
	case "literal":
		b, _ := json.Marshal(args[1:])
		fmt.Println(string(b))
		os.Exit(0)
	case "noisy":
		fmt.Print(strings.Repeat("NOISE", 100000))
		fmt.Println("FINAL LOG MARKER")
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, syscall.SIGTERM)
		<-ch
		os.Exit(0)
	case "tree":
		self, _ := os.Executable()
		child := exec.Command(self, "-test.run=^TestFixtureProcess$", "--", "--binary-manager-fixture", "ignore")
		child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		child.Env = os.Environ()
		if err := child.Start(); err != nil {
			os.Exit(92)
		}
		_ = os.WriteFile(args[1], []byte(strconv.Itoa(child.Process.Pid)), 0600)
		signal.Ignore(syscall.SIGTERM)
		for {
			time.Sleep(time.Second)
		}
	default:
		os.Exit(93)
	}
}

type harness struct {
	t    *testing.T
	root string
	p    Paths
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	root, err := os.MkdirTemp("", "bm-fixture-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	h := &harness{t: t, root: root}
	h.p = Paths{Root: h.root, Runtime: filepath.Join(h.root, "run/binary-manager"), Config: filepath.Join(h.root, "boot/optional/plugins/binary-manager/settings.json")}
	h.p.State = filepath.Join(h.p.Runtime, "runtime.json")
	h.p.Socket = filepath.Join(h.p.Runtime, "control.sock")
	h.p.Lock = filepath.Join(h.p.Runtime, "daemon.lock")
	t.Cleanup(func() {
		cmd := exec.Command(integrationBinary, "shutdown", "--clear")
		cmd.Env = append(os.Environ(), "BINARY_MANAGER_ROOT="+h.root)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("cleanup: %v %s", err, out)
		}
	})
	return h
}
func (h *harness) raw(args ...string) (map[string]any, error) {
	cmd := exec.Command(integrationBinary, args...)
	cmd.Env = append(os.Environ(), "BINARY_MANAGER_ROOT="+h.root)
	out, err := cmd.CombinedOutput()
	var result map[string]any
	if e := json.Unmarshal(out, &result); e != nil {
		return nil, fmt.Errorf("%v: %s", e, out)
	}
	return result, err
}
func (h *harness) cli(args ...string) map[string]any {
	h.t.Helper()
	out, err := h.raw(args...)
	if err != nil {
		h.t.Fatalf("CLI %v: %v %v", args, err, out)
	}
	return out
}
func (h *harness) request(v map[string]any) map[string]any {
	h.t.Helper()
	b, _ := json.Marshal(v)
	return h.cli("request", base64.RawURLEncoding.EncodeToString(b))
}
func (h *harness) status() map[string]any { return h.request(map[string]any{"action": "status"}) }
func (h *harness) mustOK(v map[string]any) map[string]any {
	h.t.Helper()
	if v["ok"] != true {
		h.t.Fatalf("request failed: %v", v)
	}
	return v
}
func fixtureApp(t *testing.T, id, mode string, extra ...string) App {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// Separate actual executable copies permit multiple configurations without
	// weakening the backend's same-executable duplicate guard.
	dest := filepath.Join(t.TempDir(), id+" 'quoted' ;& executable")
	b, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(dest, b, 0700); err != nil {
		t.Fatal(err)
	}
	args := append([]string{"-test.run=^TestFixtureProcess$", "--", "--binary-manager-fixture", mode}, extra...)
	return App{ID: id, Name: id, Path: dest, Args: args, Workdir: filepath.Dir(dest)}
}
func (h *harness) save(apps ...App) map[string]any {
	h.t.Helper()
	status := h.mustOK(h.status())
	revision := int64(status["config"].(map[string]any)["revision"].(float64))
	c := defaultConfig()
	c.Revision = revision
	c.Apps = apps
	return h.mustOK(h.request(map[string]any{"action": "save", "expectedRevision": revision, "config": c}))
}
func appStatus(t *testing.T, status map[string]any, id string) map[string]any {
	t.Helper()
	for _, v := range status["apps"].([]any) {
		a := v.(map[string]any)
		if a["id"] == id {
			return a
		}
	}
	t.Fatalf("app %s missing: %v", id, status)
	return nil
}
func eventually(t *testing.T, timeout time.Duration, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if f() {
			return
		}
		time.Sleep(70 * time.Millisecond)
	}
	t.Fatal("timed out waiting for condition")
}
func TestConcurrentEnsureAndToggleCAS(t *testing.T) {
	h := newHarness(t)
	var wg sync.WaitGroup
	results := make(chan map[string]any, 10)
	errs := make(chan error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := h.raw("ensure")
			if e != nil {
				errs <- e
			} else {
				results <- r
			}
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	pid := float64(0)
	for r := range results {
		h.mustOK(r)
		p := r["supervisor"].(map[string]any)["pid"].(float64)
		if pid != 0 && pid != p {
			t.Fatal("duplicate supervisors")
		}
		pid = p
	}
	app := fixtureApp(t, "waiter", "wait")
	h.save(app)
	h.mustOK(h.request(map[string]any{"action": "toggle", "id": app.ID, "enabled": true}))
	one := appStatus(t, h.status(), app.ID)
	if one["running"] != true {
		t.Fatal(one)
	}
	h.mustOK(h.request(map[string]any{"action": "toggle", "id": app.ID, "enabled": true}))
	two := appStatus(t, h.status(), app.ID)
	if one["pid"] != two["pid"] {
		t.Fatal("duplicate toggle replaced process")
	}
	c := defaultConfig()
	c.Apps = []App{app}
	conflict := h.request(map[string]any{"action": "save", "expectedRevision": 0, "config": c})
	if conflict["code"] != "REVISION_CONFLICT" {
		t.Fatal(conflict)
	}
	h.mustOK(h.request(map[string]any{"action": "toggle", "id": app.ID, "enabled": false}))
	if a := appStatus(t, h.status(), app.ID); a["running"] != false || a["desired"] != false {
		t.Fatal(a)
	}
}
func TestBootIntentIsSeparateAndAppliedOnce(t *testing.T) {
	h := newHarness(t)
	auto := fixtureApp(t, "auto", "wait")
	auto.Autostart = true
	manual := fixtureApp(t, "manual", "wait")
	h.save(auto, manual)
	if appStatus(t, h.status(), auto.ID)["running"] != false {
		t.Fatal("query unexpectedly started boot app")
	}
	h.mustOK(h.cli("boot"))
	if appStatus(t, h.status(), auto.ID)["running"] != true || appStatus(t, h.status(), manual.ID)["running"] != false {
		t.Fatal("wrong boot set")
	}
	h.mustOK(h.request(map[string]any{"action": "toggle", "id": auto.ID, "enabled": false}))
	h.mustOK(h.cli("boot"))
	if appStatus(t, h.status(), auto.ID)["running"] != false {
		t.Fatal("repeat boot overrode manual stop")
	}
	// Fresh runtime models /run being cleared at reboot, without touching host /run.
	h.mustOK(h.cli("shutdown", "--clear"))
	if err := os.Remove(h.p.State); err != nil {
		t.Fatal(err)
	}
	h.mustOK(h.cli("boot"))
	if appStatus(t, h.status(), auto.ID)["running"] != true {
		t.Fatal("fresh boot did not apply autostart")
	}
}
func TestFailureBackoffCleanExitAndLiteralArgv(t *testing.T) {
	h := newHarness(t)
	bad := fixtureApp(t, "bad", "error")
	clean := fixtureApp(t, "clean", "clean")
	literal := fixtureApp(t, "literal", "literal", "$(touch should-not-exist)", "hello ' world", "semi;colon")
	h.save(bad, clean, literal)
	for _, id := range []string{bad.ID, clean.ID, literal.ID} {
		h.mustOK(h.request(map[string]any{"action": "toggle", "id": id, "enabled": true}))
	}
	eventually(t, 4*time.Second, func() bool { return appStatus(t, h.status(), bad.ID)["restarts"].(float64) >= 1 })
	if a := appStatus(t, h.status(), bad.ID); a["desired"] != true {
		t.Fatal(a)
	}
	eventually(t, 2*time.Second, func() bool { return appStatus(t, h.status(), clean.ID)["state"] == "exited" })
	if a := appStatus(t, h.status(), clean.ID); a["desired"] != false || a["restarts"].(float64) != 0 {
		t.Fatal(a)
	}
	logs := h.mustOK(h.request(map[string]any{"action": "logs", "id": literal.ID}))
	if !strings.Contains(logs["lines"].(string), "$(touch should-not-exist)") {
		t.Fatal(logs)
	}
	if _, err := os.Stat(filepath.Join(literal.Workdir, "should-not-exist")); !os.IsNotExist(err) {
		t.Fatal("argv was shell evaluated")
	}
}
func TestOwnedDescendantStopAndSupervisorCrashRecovery(t *testing.T) {
	h := newHarness(t)
	pidfile := filepath.Join(t.TempDir(), "descendant.pid")
	app := fixtureApp(t, "tree", "tree", pidfile)
	h.save(app)
	h.mustOK(h.request(map[string]any{"action": "toggle", "id": app.ID, "enabled": true}))
	var descendant ProcessRef
	eventually(t, 3*time.Second, func() bool {
		b, e := os.ReadFile(pidfile)
		if e != nil {
			return false
		}
		pid, _ := strconv.Atoi(string(b))
		p, e := processInfo(pid)
		descendant = p.ProcessRef
		return e == nil
	})
	old := appStatus(t, h.status(), app.ID)
	parentPID := int(old["pid"].(float64))
	parentInfo, _ := processInfo(parentPID)
	status := h.status()
	supervisorPID := int(status["supervisor"].(map[string]any)["pid"].(float64))
	sp, _ := processInfo(supervisorPID)
	if err := signalRef(sp.ProcessRef, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	eventually(t, time.Second, func() bool { return !sameProcess(sp.ProcessRef) })
	restored := h.mustOK(h.cli("ensure"))
	fresh := appStatus(t, restored, app.ID)
	if fresh["running"] != true || int(fresh["pid"].(float64)) == parentPID {
		t.Fatal(fresh)
	}
	if sameProcess(parentInfo.ProcessRef) || sameProcess(descendant) {
		t.Fatal("owned remnants survived recovery")
	}
	h.mustOK(h.request(map[string]any{"action": "toggle", "id": app.ID, "enabled": false}))
	if a := appStatus(t, h.status(), app.ID); a["running"] != false {
		t.Fatal(a)
	}
}
func TestStalePIDNeverKilled(t *testing.T) {
	cmd := exec.Command("/bin/sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	p, err := processInfo(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	stale := p.ProcessRef
	stale.Start = "0"
	o := &Owned{Token: strings.Repeat("a", 64), Root: stale, Known: []ProcessRef{stale}}
	if err = terminateOwned(o, 50*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if !sameProcess(p.ProcessRef) {
		t.Fatal("unrelated PID was killed")
	}
}
func TestLogDiskBoundAndShutdownResume(t *testing.T) {
	h := newHarness(t)
	app := fixtureApp(t, "noisy", "noisy")
	h.save(app)
	h.mustOK(h.request(map[string]any{"action": "toggle", "id": app.ID, "enabled": true}))
	eventually(t, 3*time.Second, func() bool {
		logs := h.request(map[string]any{"action": "logs", "id": app.ID})
		return logs["ok"] == true && strings.Contains(logs["lines"].(string), "FINAL LOG MARKER")
	})
	h.mustOK(h.cli("shutdown"))
	fi, err := os.Stat(filepath.Join(h.p.Runtime, app.ID+".log"))
	if err != nil || fi.Size() > logLimit {
		t.Fatal(fi, err)
	}
	resumed := h.mustOK(h.cli("ensure"))
	if appStatus(t, resumed, app.ID)["running"] != true {
		t.Fatal("upgrade-style ensure did not resume")
	}
	h.mustOK(h.cli("shutdown", "--clear"))
	if appStatus(t, h.mustOK(h.cli("ensure")), app.ID)["running"] != false {
		t.Fatal("clear shutdown resumed app")
	}
}
func TestLifecycleFailureExitStatusAndStrictWire(t *testing.T) {
	h := newHarness(t)
	if err := os.MkdirAll(h.p.Runtime, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(h.p.State, []byte("broken state"), 0600); err != nil {
		t.Fatal(err)
	}
	r, err := h.raw("ensure")
	if err == nil || r["ok"] != false {
		t.Fatalf("lifecycle falsely succeeded: %v %v", r, err)
	}
	_ = os.Remove(h.p.State)
	r = h.cli("request", "not+base64")
	if r["ok"] != false || r["code"] != "INVALID_REQUEST" {
		t.Fatal(r)
	}
	r = h.cli("request", base64.RawURLEncoding.EncodeToString([]byte(`{"action":"boot"}`)))
	if r["ok"] != false {
		t.Fatal("query accepted reserved lifecycle")
	}
}

func TestConcurrentSaveHasExactlyOneWinner(t *testing.T) {
	h := newHarness(t)
	app := fixtureApp(t, "app", "wait")
	status := h.save(app)
	revision := int64(status["config"].(map[string]any)["revision"].(float64))
	var wg sync.WaitGroup
	results := make(chan map[string]any, 2)
	errs := make(chan error, 2)
	for _, name := range []string{"First writer", "Second writer"} {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			c := defaultConfig()
			a := app
			a.Name = name
			c.Apps = []App{a}
			b, _ := json.Marshal(map[string]any{"action": "save", "expectedRevision": revision, "config": c})
			r, e := h.raw("request", base64.RawURLEncoding.EncodeToString(b))
			if e != nil {
				errs <- e
			} else {
				results <- r
			}
		}(name)
	}
	wg.Wait()
	close(results)
	close(errs)
	for e := range errs {
		t.Fatal(e)
	}
	wins, conflicts := 0, 0
	for r := range results {
		if r["ok"] == true {
			wins++
		} else if r["code"] == "REVISION_CONFLICT" {
			conflicts++
		} else {
			t.Fatal(r)
		}
	}
	if wins != 1 || conflicts != 1 {
		t.Fatalf("wins=%d conflicts=%d", wins, conflicts)
	}
}
