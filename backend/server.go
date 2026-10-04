package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Paths struct{ Root, Runtime, Config, Socket, State, Lock string }

func paths() (Paths, error) {
	root := os.Getenv("BINARY_MANAGER_ROOT")
	if root != "" {
		if err := validAbsolute(root, false); err != nil || root == "/" {
			return Paths{}, errors.New("BINARY_MANAGER_ROOT must be a clean absolute test directory other than /")
		}
	}
	p := Paths{Root: root}
	p.Runtime = filepath.Join(root, "/run/binary-manager")
	p.Config = filepath.Join(root, "/boot/optional/plugins/binary-manager/settings.json")
	p.Socket = filepath.Join(p.Runtime, "control.sock")
	p.State = filepath.Join(p.Runtime, "runtime.json")
	p.Lock = filepath.Join(p.Runtime, "daemon.lock")
	return p, nil
}
func secureRuntime(p Paths) error {
	if err := os.MkdirAll(p.Runtime, 0700); err != nil {
		return err
	}
	fi, err := os.Lstat(p.Runtime)
	if err != nil {
		return err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 || !ok || int(st.Uid) != os.Geteuid() {
		return errors.New("runtime directory must be a real directory owned by the service user")
	}
	if err = os.Chmod(p.Runtime, 0700); err != nil {
		return err
	}
	return nil
}

type RuntimeFile struct {
	BootID      string            `json:"bootId"`
	BootApplied bool              `json:"bootApplied"`
	Desired     map[string]bool   `json:"desired"`
	Owned       map[string]*Owned `json:"owned"`
}
type Exit struct {
	Code   int    `json:"code"`
	Signal string `json:"signal"`
	At     string `json:"at"`
}
type RuntimeApp struct {
	App        App
	Desired    bool
	Owned      *Owned
	State      string
	Restarts   int
	Failures   int
	LastExit   *Exit
	Error      string
	Started    time.Time
	Next       time.Time
	Generation int
	Log        *tailLog
	Done       chan struct{}
}
type Supervisor struct {
	mu          sync.Mutex
	p           Paths
	cfg         Config
	configError error
	bootID      string
	bootApplied bool
	apps        map[string]*RuntimeApp
	closing     bool
	listener    net.Listener
	done        chan struct{}
}

func bootID() (string, error) {
	b, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	return strings.TrimSpace(string(b)), err
}
func newSupervisor(p Paths) (*Supervisor, error) {
	if err := secureRuntime(p); err != nil {
		return nil, err
	}
	fd, err := pidfdOpen(os.Getpid())
	if err != nil {
		return nil, fmt.Errorf("safe process control requires Linux pidfd support (5.3+): %w", err)
	}
	syscall.Close(fd)
	id, err := bootID()
	if err != nil || id == "" {
		return nil, errors.New("cannot establish kernel boot identity")
	}
	s := &Supervisor{p: p, bootID: id, apps: map[string]*RuntimeApp{}, done: make(chan struct{})}
	s.cfg, s.configError = loadConfig(p.Config)
	disk := RuntimeFile{BootID: id, Desired: map[string]bool{}, Owned: map[string]*Owned{}}
	if b, err := readBounded(p.State, 2*1024*1024); err == nil {
		if err = json.Unmarshal(b, &disk); err != nil {
			return nil, fmt.Errorf("invalid runtime state; refusing unsafe recovery: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if disk.BootID == id {
		s.bootApplied = disk.BootApplied
		// A recovered PID is never adopted. Stop only verified remnants first, and
		// refuse a replacement launch if bounded cleanup cannot be proven complete.
		var wg sync.WaitGroup
		errs := make(chan error, len(disk.Owned))
		for _, o := range disk.Owned {
			if o == nil {
				continue
			}
			wg.Add(1)
			go func(o *Owned) {
				defer wg.Done()
				if err := terminateOwned(o, 2*time.Second); err != nil {
					errs <- err
				}
			}(o)
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			return nil, fmt.Errorf("cannot safely recover owned children: %w", err)
		}
	} else {
		disk.Desired = map[string]bool{}
	}
	if s.configError == nil {
		for _, a := range s.cfg.Apps {
			r := s.newApp(a)
			r.Desired = disk.Desired[a.ID]
			s.apps[a.ID] = r
		}
	}
	if err = s.persist(); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *Supervisor) newApp(a App) *RuntimeApp {
	l := &tailLog{}
	if b, err := readBounded(filepath.Join(s.p.Runtime, a.ID+".log"), logLimit); err == nil {
		l.b = b
	}
	return &RuntimeApp{App: a, State: "stopped", Log: l}
}
func (s *Supervisor) persist() error {
	f := RuntimeFile{BootID: s.bootID, BootApplied: s.bootApplied, Desired: map[string]bool{}, Owned: map[string]*Owned{}}
	for id, a := range s.apps {
		f.Desired[id] = a.Desired
		if a.Owned != nil {
			f.Owned[id] = a.Owned
		}
	}
	return atomicJSON(s.p.State, f)
}
func (s *Supervisor) start(a *RuntimeApp, automatic bool) {
	if s.closing || a.Owned != nil || !a.Desired {
		return
	}
	if err := executablePath(a.App.Path); err != nil {
		s.failed(a, err)
		return
	}
	if a.App.Workdir != "" {
		if err := directoryPath(a.App.Workdir); err != nil {
			s.failed(a, err)
			return
		}
	}
	// Private launch handshake: no user executable runs until ownership is
	// atomically recorded. A dead supervisor closes the pipe before authorization.
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		s.failed(a, err)
		return
	}
	self, err := os.Executable()
	if err != nil {
		s.failed(a, err)
		return
	}
	launchApp := a.App
	launchApp.Extra = nil
	payload, err := json.Marshal(launchApp)
	if err != nil {
		s.failed(a, err)
		return
	}
	r, w, err := os.Pipe()
	if err != nil {
		s.failed(a, err)
		return
	}
	cmd := exec.Command(self, "__child", base64.RawURLEncoding.EncodeToString(payload))
	cmd.ExtraFiles = []*os.File{r}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	env := []string{}
	for _, v := range os.Environ() {
		if !strings.HasPrefix(v, ownerKey) && !strings.HasPrefix(v, "BINARY_MANAGER_ROOT=") {
			env = append(env, v)
		}
	}
	cmd.Env = append(env, ownerKey+hex.EncodeToString(token))
	cmd.Stdout = a.Log
	cmd.Stderr = a.Log
	// Prevent grandchildren holding stdout open from blocking cmd.Wait forever.
	cmd.WaitDelay = 200 * time.Millisecond
	if err = cmd.Start(); err != nil {
		r.Close()
		w.Close()
		s.failed(a, err)
		return
	}
	r.Close()
	info, err := processInfo(cmd.Process.Pid)
	if err != nil {
		w.Close()
		_ = cmd.Wait()
		s.failed(a, errors.New("child exited before launch ownership could be recorded"))
		return
	}
	a.Owned = &Owned{Token: hex.EncodeToString(token), Root: info.ProcessRef, Known: []ProcessRef{info.ProcessRef}}
	a.Generation++
	generation := a.Generation
	a.Done = make(chan struct{})
	done := a.Done
	a.State = "running"
	a.Started = time.Now()
	a.Next = time.Time{}
	a.Error = ""
	if automatic {
		a.Restarts++
	}
	if err = s.persist(); err != nil {
		w.Close()
		_ = cmd.Wait()
		a.Owned = nil
		a.Desired = false
		a.State = "error"
		a.Error = "cannot record process ownership: " + err.Error()
		close(a.Done)
		return
	}
	if _, err = w.Write([]byte{1}); err != nil {
		a.Error = "launch handshake failed: " + err.Error()
	}
	w.Close()
	go func() {
		err := cmd.Wait()
		close(done)
		s.mu.Lock()
		defer s.mu.Unlock()
		if generation != a.Generation || a.Owned == nil {
			return
		}
		s.exited(a, err, cmd.ProcessState)
	}()
}
func (s *Supervisor) failed(a *RuntimeApp, err error) {
	a.Error = err.Error()
	a.State = "backoff"
	a.Failures++
	a.Next = time.Now().Add(backoff(a.Failures))
}
func backoff(failures int) time.Duration {
	if failures < 1 {
		failures = 1
	}
	if failures >= 6 {
		return 30 * time.Second
	}
	return time.Second * time.Duration(1<<uint(failures-1))
}
func (s *Supervisor) exited(a *RuntimeApp, err error, ps *os.ProcessState) {
	code := 0
	signal := ""
	if ps != nil {
		code = ps.ExitCode()
		if w, ok := ps.Sys().(syscall.WaitStatus); ok && w.Signaled() {
			signal = w.Signal().String()
		}
	} else if err != nil {
		code = -1
	}
	a.LastExit = &Exit{code, signal, time.Now().UTC().Format(time.RFC3339)}
	if e := terminateOwned(a.Owned, 500*time.Millisecond); e != nil {
		a.Desired = false
		a.State = "error"
		a.Error = e.Error()
		_ = s.persist()
		return
	}
	a.Owned = nil
	if !a.Desired || s.closing {
		a.State = "stopped"
	} else if code == 0 {
		a.Desired = false
		a.State = "exited"
		a.Error = ""
		a.Failures = 0
		a.Next = time.Time{}
	} else {
		if time.Since(a.Started) >= 30*time.Second {
			a.Failures = 0
		}
		s.failed(a, fmt.Errorf("process exited with code %d%s", code, signalSuffix(signal)))
	}
	if e := s.persist(); e != nil {
		a.Desired = false
		a.State = "error"
		a.Error = "cannot persist runtime state: " + e.Error()
	}
}
func signalSuffix(s string) string {
	if s == "" {
		return ""
	}
	return " (" + s + ")"
}
func (s *Supervisor) stop(a *RuntimeApp) error {
	a.Generation++
	if a.Owned != nil {
		if err := terminateOwned(a.Owned, 2*time.Second); err != nil {
			a.State = "error"
			a.Error = err.Error()
			return err
		}
	}
	a.Owned = nil
	a.State = "stopped"
	a.Next = time.Time{}
	a.Error = ""
	return nil
}
func sameExecution(a, b App) bool {
	return a.Path == b.Path && a.Workdir == b.Workdir && reflect.DeepEqual(a.Args, b.Args)
}

// Stop affected trees before committing a requested settings change. If stop
// fails, retain the old visible app/config and allow the user to retry safely.
func (s *Supervisor) prepareConfig(c Config) error {
	next := map[string]App{}
	for _, a := range c.Apps {
		next[a.ID] = a
	}
	var wg sync.WaitGroup
	var errorMu sync.Mutex
	var firstErr error
	for id, a := range s.apps {
		n, exists := next[id]
		if !exists || !sameExecution(n, a.App) {
			wg.Add(1)
			go func(a *RuntimeApp) {
				defer wg.Done()
				if err := s.stop(a); err != nil {
					a.Desired = false
					errorMu.Lock()
					if firstErr == nil {
						firstErr = err
					}
					errorMu.Unlock()
				}
			}(a)
		}
	}
	wg.Wait()
	if err := s.persist(); err != nil && firstErr == nil {
		firstErr = err
	}
	if firstErr != nil {
		s.resumeStopped()
	}
	return firstErr
}
func (s *Supervisor) resumeStopped() {
	for _, a := range s.apps {
		if a.Desired && a.Owned == nil && a.Next.IsZero() {
			s.start(a, false)
		}
	}
}
func (s *Supervisor) apply(c Config) error {
	newApps := map[string]App{}
	for _, a := range c.Apps {
		newApps[a.ID] = a
	}
	// Persisted config has already committed. Stop changed/removed applications
	// before allowing a new path/argv to replace an owned process.
	var firstErr error
	var wg sync.WaitGroup
	stopErrors := make(map[string]error)
	var errMu sync.Mutex
	for id, r := range s.apps {
		a, ok := newApps[id]
		if !ok || !sameExecution(a, r.App) {
			wg.Add(1)
			go func(id string, r *RuntimeApp) {
				defer wg.Done()
				if err := s.stop(r); err != nil {
					errMu.Lock()
					stopErrors[id] = err
					errMu.Unlock()
				}
			}(id, r)
		}
	}
	wg.Wait()
	for id, r := range s.apps {
		if err := stopErrors[id]; err != nil {
			r.Desired = false
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		a, ok := newApps[id]
		if !ok {
			delete(s.apps, id)
			_ = os.Remove(filepath.Join(s.p.Runtime, id+".log"))
		} else {
			r.App = a
		}
	}
	for _, a := range c.Apps {
		if _, ok := s.apps[a.ID]; !ok {
			s.apps[a.ID] = s.newApp(a)
		}
	}
	s.cfg = c
	s.configError = nil
	if err := s.persist(); err != nil {
		return err
	}
	if firstErr != nil {
		return firstErr
	}
	for _, a := range s.apps {
		if a.Desired && a.Owned == nil && a.Next.IsZero() {
			s.start(a, false)
		}
	}
	return nil
}
func (s *Supervisor) refresh() error {
	c, err := loadConfig(s.p.Config)
	if err != nil {
		s.configError = err
		return err
	}
	old, _ := json.Marshal(s.cfg)
	current, _ := json.Marshal(c)
	if string(old) != string(current) || s.configError != nil {
		return s.apply(c)
	}
	return nil
}
func sortEntries(entries []Entry) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Type != entries[j].Type {
			return entries[i].Type == "directory"
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
}
func (s *Supervisor) status() map[string]any {
	apps := []map[string]any{}
	for _, a := range s.cfg.Apps {
		r, ok := s.apps[a.ID]
		if !ok {
			continue
		}
		pid := 0
		running := false
		if r.Owned != nil && sameProcess(r.Owned.Root) {
			pid = r.Owned.Root.PID
			running = true
		}
		m := map[string]any{"id": a.ID, "desired": r.Desired, "running": running, "pid": pid, "state": r.State, "restarts": r.Restarts, "lastExit": r.LastExit, "error": r.Error}
		if !r.Next.IsZero() {
			m["nextRestartAt"] = r.Next.UTC().Format(time.RFC3339)
		}
		apps = append(apps, m)
	}
	discovered := []Entry{}
	folderError := ""
	truncated := false
	if s.cfg.Folder != "" {
		b, err := browse(s.cfg.Folder)
		if err != nil {
			folderError = err.Error()
		} else {
			truncated, _ = b["truncated"].(bool)
			for _, e := range b["entries"].([]Entry) {
				if e.Type == "executable" {
					discovered = append(discovered, e)
				}
			}
		}
	}
	mode := "manual"
	if s.bootApplied {
		mode = "boot"
	}
	return map[string]any{"ok": true, "config": s.cfg, "apps": apps, "discovered": discovered, "folderError": folderError, "discoveryTruncated": truncated, "supervisor": map[string]any{"pid": os.Getpid(), "mode": mode, "bootApplied": s.bootApplied}}
}

type Request struct {
	Action           string          `json:"action"`
	Path             string          `json:"path"`
	ID               string          `json:"id"`
	Enabled          *bool           `json:"enabled"`
	ExpectedRevision *int64          `json:"expectedRevision"`
	Config           json.RawMessage `json:"config"`
	Clear            bool            `json:"clear"`
}

func failure(code string, err any) map[string]any {
	return map[string]any{"ok": false, "code": code, "error": fmt.Sprint(err)}
}
func (s *Supervisor) handle(req Request) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return failure("SHUTTING_DOWN", "supervisor is shutting down")
	}
	if req.Action == "shutdown" {
		s.closing = true
		var wg sync.WaitGroup
		errs := make(chan error, len(s.apps))
		for _, a := range s.apps {
			if req.Clear {
				a.Desired = false
			}
			a.Generation++
			wg.Add(1)
			go func(a *RuntimeApp) {
				defer wg.Done()
				if err := terminateOwned(a.Owned, 2*time.Second); err != nil {
					errs <- err
					return
				}
				a.Owned = nil
				a.State = "stopped"
			}(a)
		}
		wg.Wait()
		close(errs)
		var stopErr error
		for err := range errs {
			stopErr = err
		}
		if err := s.persist(); err != nil {
			stopErr = err
		}
		for id, a := range s.apps {
			_ = a.Log.flush(filepath.Join(s.p.Runtime, id+".log"))
		}
		if stopErr != nil {
			s.closing = false
			return failure("STOP_FAILED", stopErr)
		}
		go func() {
			time.Sleep(20 * time.Millisecond)
			close(s.done)
			if s.listener != nil {
				s.listener.Close()
			}
		}()
		return map[string]any{"ok": true, "stopped": true}
	}
	if req.Action == "browse" {
		out, err := browse(req.Path)
		if err != nil {
			return failure("BROWSE_FAILED", err)
		}
		return out
	}
	if err := s.refresh(); err != nil {
		return failure("CONFIG_INVALID", err)
	}
	switch req.Action {
	case "status", "ensure":
		return s.status()
	case "boot":
		if !s.bootApplied {
			s.bootApplied = true
			for _, a := range s.apps {
				if a.App.Autostart {
					a.Desired = true
				}
			}
			if err := s.persist(); err != nil {
				return failure("STATE_WRITE_FAILED", err)
			}
			for _, a := range s.apps {
				if a.Desired && a.Owned == nil {
					s.start(a, false)
				}
			}
		}
		return s.status()
	case "save", "remove":
		if req.ExpectedRevision == nil {
			return failure("INVALID_REQUEST", "expectedRevision is required")
		}
		if *req.ExpectedRevision != s.cfg.Revision {
			return failure("REVISION_CONFLICT", "settings changed; reload before saving")
		}
		var c Config
		if req.Action == "save" {
			if len(req.Config) == 0 {
				return failure("INVALID_CONFIG", "config is required")
			}
			if err := decodeJSON(req.Config, &c); err != nil {
				return failure("INVALID_CONFIG", err)
			}
			mergeUnknown(&c, s.cfg)
		} else {
			b, _ := json.Marshal(s.cfg)
			_ = json.Unmarshal(b, &c)
			found := false
			out := []App{}
			for _, a := range c.Apps {
				if a.ID == req.ID {
					found = true
				} else {
					out = append(out, a)
				}
			}
			if !found {
				return failure("NOT_FOUND", "app not found")
			}
			c.Apps = out
		}
		c.Revision = s.cfg.Revision + 1
		if err := validateConfig(&c, false); err != nil {
			return failure("INVALID_CONFIG", err)
		}
		if err := validateChangedFiles(c, s.cfg); err != nil {
			return failure("INVALID_CONFIG", err)
		}
		if err := s.prepareConfig(c); err != nil {
			return failure("STOP_FAILED", fmt.Sprintf("settings unchanged; cannot safely stop affected app: %v", err))
		}
		if err := atomicJSON(s.p.Config, c); err != nil {
			s.resumeStopped()
			return failure("SETTINGS_WRITE_FAILED", err)
		}
		if err := s.apply(c); err != nil {
			return failure("APPLY_FAILED", fmt.Sprintf("settings saved, but runtime update failed: %v", err))
		}
		return s.status()
	case "toggle", "restart", "logs":
		a, ok := s.apps[req.ID]
		if !ok {
			return failure("NOT_FOUND", "app not found")
		}
		if req.Action == "logs" {
			return map[string]any{"ok": true, "id": req.ID, "lines": a.Log.snapshot(), "maxBytes": logLimit}
		}
		if req.Action == "toggle" && req.Enabled == nil {
			return failure("INVALID_REQUEST", "enabled is required")
		}
		desired := true
		if req.Action == "toggle" {
			desired = *req.Enabled
		}
		previous := a.Desired
		a.Desired = desired
		if err := s.persist(); err != nil {
			a.Desired = previous
			return failure("STATE_WRITE_FAILED", err)
		}
		if !desired || req.Action == "restart" {
			if err := s.stop(a); err != nil {
				a.Desired = false
				_ = s.persist()
				return failure("STOP_FAILED", err)
			}
		}
		if desired && a.Owned == nil {
			a.Failures = 0
			a.Next = time.Time{}
			s.start(a, false)
		}
		if err := s.persist(); err != nil {
			return failure("STATE_WRITE_FAILED", err)
		}
		return s.status()
	default:
		return failure("INVALID_ACTION", "unknown action")
	}
}
func (s *Supervisor) tick() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return
	}
	ownedChanged := false
	for id, a := range s.apps {
		if a.Owned != nil {
			before, _ := json.Marshal(a.Owned.Known)
			collectOwned(a.Owned)
			after, _ := json.Marshal(a.Owned.Known)
			if string(before) != string(after) {
				ownedChanged = true
			}
		}
		if a.Owned != nil && time.Since(a.Started) >= 30*time.Second {
			a.Failures = 0
		}
		if s.configError == nil && a.Desired && a.Owned == nil && !a.Next.IsZero() && !time.Now().Before(a.Next) {
			s.start(a, true)
		}
		_ = a.Log.flush(filepath.Join(s.p.Runtime, id+".log"))
	}
	if ownedChanged {
		_ = s.persist()
	}
}
