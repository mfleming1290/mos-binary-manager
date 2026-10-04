package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// These integration tests deliberately exercise the real supervision engine
// without an IPC listener. They can run in restrictive build containers that
// disallow socket(), while CLI/socket tests remain separate and unskipped.
type coreHarness struct {
	t *testing.T
	s *Supervisor
	p Paths
}

func newCore(t *testing.T) *coreHarness {
	t.Helper()
	root := t.TempDir()
	p := Paths{Root: root, Runtime: filepath.Join(root, "run/binary-manager"), Config: filepath.Join(root, "boot/optional/plugins/binary-manager/settings.json")}
	p.State = filepath.Join(p.Runtime, "runtime.json")
	s, err := newSupervisor(p)
	if err != nil {
		t.Fatal(err)
	}
	h := &coreHarness{t: t, s: s, p: p}
	stopTick := make(chan struct{})
	go func() {
		tick := time.NewTicker(50 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-tick.C:
				s.tick()
			case <-stopTick:
				return
			}
		}
	}()
	t.Cleanup(func() {
		close(stopTick)
		s.mu.Lock()
		s.closing = true
		for _, a := range s.apps {
			a.Generation++
			a.Desired = false
			if err := s.stop(a); err != nil {
				t.Errorf("stop fixture: %v", err)
			}
		}
		s.mu.Unlock()
	})
	return h
}
func (h *coreHarness) request(req Request) map[string]any {
	h.t.Helper()
	v := h.s.handle(req)
	b, err := json.Marshal(v)
	if err != nil {
		h.t.Fatal(err)
	}
	var out map[string]any
	if err = json.Unmarshal(b, &out); err != nil {
		h.t.Fatal(err)
	}
	return out
}
func (h *coreHarness) ok(req Request) map[string]any {
	h.t.Helper()
	v := h.request(req)
	if v["ok"] != true {
		h.t.Fatal(v)
	}
	return v
}
func (h *coreHarness) status() map[string]any { return h.ok(Request{Action: "status"}) }
func (h *coreHarness) save(apps ...App) map[string]any {
	h.t.Helper()
	rev := int64(h.status()["config"].(map[string]any)["revision"].(float64))
	c := defaultConfig()
	c.Apps = apps
	b, _ := json.Marshal(c)
	return h.ok(Request{Action: "save", Config: b, ExpectedRevision: &rev})
}
func (h *coreHarness) toggle(id string, enabled bool) map[string]any {
	return h.ok(Request{Action: "toggle", ID: id, Enabled: &enabled})
}
func TestCoreConcurrentToggleAndSave(t *testing.T) {
	h := newCore(t)
	app := fixtureApp(t, "waiter", "wait")
	h.save(app)
	var wg sync.WaitGroup
	results := make(chan map[string]any, 12)
	yes := true
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- h.s.handle(Request{Action: "toggle", ID: app.ID, Enabled: &yes}) }()
	}
	wg.Wait()
	close(results)
	for r := range results {
		if r["ok"] != true {
			t.Fatal(r)
		}
	}
	first := appStatus(t, h.status(), app.ID)
	if first["running"] != true {
		t.Fatal(first)
	}
	pid := first["pid"]
	h.toggle(app.ID, true)
	if appStatus(t, h.status(), app.ID)["pid"] != pid {
		t.Fatal("duplicate process on repeated enable")
	}
	revision := int64(h.status()["config"].(map[string]any)["revision"].(float64))
	responses := make(chan map[string]any, 2)
	for _, name := range []string{"one", "two"} {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			c := defaultConfig()
			a := app
			a.Name = name
			c.Apps = []App{a}
			b, _ := json.Marshal(c)
			responses <- h.s.handle(Request{Action: "save", ExpectedRevision: &revision, Config: b})
		}(name)
	}
	wg.Wait()
	close(responses)
	wins, conflicts := 0, 0
	for r := range responses {
		if r["ok"] == true {
			wins++
		} else if r["code"] == "REVISION_CONFLICT" {
			conflicts++
		} else {
			t.Fatal(r)
		}
	}
	if wins != 1 || conflicts != 1 {
		t.Fatal(wins, conflicts)
	}
	h.toggle(app.ID, false)
	if appStatus(t, h.status(), app.ID)["running"] != false {
		t.Fatal("stop failed")
	}
}
func TestCoreBootCleanExitAndErrorBackoff(t *testing.T) {
	h := newCore(t)
	auto := fixtureApp(t, "auto", "wait")
	auto.Autostart = true
	clean := fixtureApp(t, "clean", "clean")
	bad := fixtureApp(t, "error", "error")
	h.save(auto, clean, bad)
	if appStatus(t, h.status(), auto.ID)["running"] != false {
		t.Fatal("status applied boot intent")
	}
	h.ok(Request{Action: "boot"})
	if appStatus(t, h.status(), auto.ID)["running"] != true {
		t.Fatal("boot did not start app")
	}
	h.toggle(auto.ID, false)
	h.ok(Request{Action: "boot"})
	if appStatus(t, h.status(), auto.ID)["running"] != false {
		t.Fatal("repeated boot overrode manual stop")
	}
	h.toggle(clean.ID, true)
	h.toggle(bad.ID, true)
	eventually(t, 5*time.Second, func() bool {
		s := h.status()
		return appStatus(t, s, clean.ID)["state"] == "exited" && appStatus(t, s, bad.ID)["restarts"].(float64) >= 1
	})
	c := appStatus(t, h.status(), clean.ID)
	if c["desired"] != false || c["restarts"].(float64) != 0 {
		t.Fatal(c)
	}
	e := appStatus(t, h.status(), bad.ID)
	if e["desired"] != true {
		t.Fatal(e)
	}
}
func TestCoreLiteralArgvAndDefaultCWD(t *testing.T) {
	h := newCore(t)
	app := fixtureApp(t, "literal", "literal", "$(touch SHOULD_NOT_EXIST)", "Unicode 日本語", `double"quote`, "semi;colon")
	app.Workdir = ""
	h.save(app)
	h.toggle(app.ID, true)
	eventually(t, 3*time.Second, func() bool { return appStatus(t, h.status(), app.ID)["state"] == "exited" })
	logs := h.ok(Request{Action: "logs", ID: app.ID})["lines"].(string)
	if !strings.Contains(logs, "$(touch SHOULD_NOT_EXIST)") || !strings.Contains(logs, "日本語") {
		t.Fatal(logs)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(app.Path), "SHOULD_NOT_EXIST")); !os.IsNotExist(err) {
		t.Fatal("argv was shell-expanded")
	}
	// A relative pid-file written by the executable proves empty workdir is its
	// own folder, independent of the supervisor/boot-hook working directory.
	app.Args = []string{"-test.run=^TestFixtureProcess$", "--", "--binary-manager-fixture", "wait", "relative.pid"}
	h.save(app)
	h.toggle(app.ID, true)
	eventually(t, 3*time.Second, func() bool {
		_, err := os.Stat(filepath.Join(filepath.Dir(app.Path), "relative.pid"))
		return err == nil
	})
}
func TestCoreTreeStopRecoveryAndLogBounds(t *testing.T) {
	h := newCore(t)
	pidfile := filepath.Join(t.TempDir(), "descendant.pid")
	app := fixtureApp(t, "tree", "tree", pidfile)
	h.save(app)
	h.toggle(app.ID, true)
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
	before := appStatus(t, h.status(), app.ID)
	parent, _ := processInfo(int(before["pid"].(float64)))
	// Model abrupt supervisor loss: stop its event processing without modifying
	// its durable launch record, then recover through the production constructor.
	h.s.mu.Lock()
	h.s.closing = true
	for _, a := range h.s.apps {
		a.Generation++
	}
	h.s.mu.Unlock()
	recovered, err := newSupervisor(h.p)
	if err != nil {
		t.Fatal(err)
	}
	recovered.mu.Lock()
	recovered.resumeStopped()
	recovered.mu.Unlock()
	if sameProcess(parent.ProcessRef) || sameProcess(descendant) {
		t.Fatal("owned old tree survived recovery")
	}
	recovered.mu.Lock()
	fresh := recovered.apps[app.ID]
	if fresh == nil || fresh.Owned == nil || !sameProcess(fresh.Owned.Root) {
		t.Fatal("same-boot desired app did not resume")
	}
	fresh.Desired = false
	if err := recovered.stop(fresh); err != nil {
		t.Fatal(err)
	}
	recovered.closing = true
	recovered.mu.Unlock()
}
func TestCoreRunningEditRemoveAndLogBounds(t *testing.T) {
	h := newCore(t)
	app := fixtureApp(t, "noisy", "noisy")
	h.save(app)
	h.toggle(app.ID, true)
	old := appStatus(t, h.status(), app.ID)
	oldProc, _ := processInfo(int(old["pid"].(float64)))
	eventually(t, 3*time.Second, func() bool {
		return strings.Contains(h.ok(Request{Action: "logs", ID: app.ID})["lines"].(string), "FINAL LOG MARKER")
	})
	log := h.ok(Request{Action: "logs", ID: app.ID})["lines"].(string)
	if len(log) > logLimit {
		t.Fatal("memory log grew past limit")
	}
	eventually(t, time.Second, func() bool {
		fi, e := os.Stat(filepath.Join(h.p.Runtime, app.ID+".log"))
		return e == nil && fi.Size() <= logLimit
	})
	app.Args = []string{"-test.run=^TestFixtureProcess$", "--", "--binary-manager-fixture", "wait"}
	h.save(app)
	fresh := appStatus(t, h.status(), app.ID)
	if fresh["running"] != true || fresh["pid"] == old["pid"] || sameProcess(oldProc.ProcessRef) {
		t.Fatal("execution edit did not replace safely")
	}
	rev := int64(h.status()["config"].(map[string]any)["revision"].(float64))
	r := h.ok(Request{Action: "remove", ID: app.ID, ExpectedRevision: &rev})
	if len(r["apps"].([]any)) != 0 {
		t.Fatal("remove failed")
	}
}
func TestCoreRuntimeBootIdentityAndLock(t *testing.T) {
	h := newCore(t)
	app := fixtureApp(t, "auto", "wait")
	app.Autostart = true
	h.save(app)
	wrong := RuntimeFile{BootID: "a previous boot", BootApplied: true, Desired: map[string]bool{app.ID: true}, Owned: map[string]*Owned{}}
	if err := atomicJSON(h.p.State, wrong); err != nil {
		t.Fatal(err)
	}
	recovered, err := newSupervisor(h.p)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.bootApplied || recovered.apps[app.ID].Desired {
		t.Fatal("previous boot runtime leaked into new boot")
	}
	lock1, err := openLock(filepath.Join(h.p.Runtime, "test.lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer lock1.Close()
	lock2, err := openLock(filepath.Join(h.p.Runtime, "test.lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer lock2.Close()
	if err = syscall.Flock(int(lock1.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	if err = syscall.Flock(int(lock2.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != syscall.EWOULDBLOCK {
		t.Fatalf("duplicate lock was admitted: %v", err)
	}
}
