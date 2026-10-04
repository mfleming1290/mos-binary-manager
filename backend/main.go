// Binary Manager is a Linux foreground-process supervisor and narrow JSON CLI.
// It intentionally has no network listener, shell parser, systemd unit or deps.
package main

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"syscall"
	"time"
)

var tokenAlphabet = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func emit(v any) {
	_ = json.NewEncoder(os.Stdout).Encode(v)
	if len(os.Args) > 1 && (os.Args[1] == "ensure" || os.Args[1] == "boot" || os.Args[1] == "shutdown") {
		if m, ok := v.(map[string]any); ok && m["ok"] == false {
			os.Exit(1)
		}
	}
}
func main() {
	if len(os.Args) > 1 && os.Args[1] == "__child" {
		if err := childMain(); err != nil {
			fmt.Fprintln(os.Stderr, "binary-manager launch:", err)
			os.Exit(126)
		}
		return
	}
	p, err := paths()
	if err != nil {
		emit(failure("INVALID_ROOT", err))
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "__daemon" {
		resume := len(os.Args) < 3 || os.Args[2] != "--no-resume"
		if err := daemonMain(p, resume); err != nil {
			_ = atomicJSON(p.Runtime+"/daemon-error.json", failure("DAEMON_FAILED", err))
			os.Exit(1)
		}
		return
	}
	var req Request
	if len(os.Args) < 2 {
		emit(failure("USAGE", "use request BASE64URL_JSON, ensure, boot, shutdown [--clear]"))
		return
	}
	switch os.Args[1] {
	case "request":
		if len(os.Args) != 3 || len(os.Args[2]) > maxJSON*4/3+4 || !tokenAlphabet.MatchString(os.Args[2]) {
			emit(failure("INVALID_REQUEST", "request requires unpadded base64url JSON using A-Z a-z 0-9 _ - only"))
			return
		}
		b, err := base64.RawURLEncoding.Strict().DecodeString(os.Args[2])
		if err != nil {
			emit(failure("INVALID_REQUEST", err))
			return
		}
		if err = decodeJSON(b, &req); err != nil {
			emit(failure("INVALID_REQUEST", err))
			return
		}
		if req.Action == "boot" || req.Action == "shutdown" || req.Action == "ensure" {
			emit(failure("INVALID_ACTION", "lifecycle actions are CLI-only"))
			return
		}
	case "ensure", "boot":
		if len(os.Args) != 2 {
			emit(failure("USAGE", "unexpected arguments"))
			return
		}
		req.Action = os.Args[1]
	case "shutdown":
		if len(os.Args) > 3 || (len(os.Args) == 3 && os.Args[2] != "--clear") {
			emit(failure("USAGE", "use shutdown [--clear]"))
			return
		}
		req.Action = "shutdown"
		req.Clear = len(os.Args) == 3
	default:
		emit(failure("USAGE", "unknown command"))
		return
	}
	if err = ensureDaemon(p, req.Action != "shutdown"); err != nil {
		emit(failure("SUPERVISOR_UNAVAILABLE", err))
		return
	}
	out, err := call(p, req)
	if err != nil {
		emit(failure("TRANSPORT_ERROR", fmt.Sprintf("request outcome may be unknown; reload status: %v", err)))
		return
	}
	if req.Action == "shutdown" && out["ok"] == true {
		if err := waitStopped(p); err != nil {
			emit(failure("STOP_PENDING", err))
			return
		}
	}
	emit(out)
}
func childMain() error {
	if len(os.Args) != 3 {
		return errors.New("invalid child invocation")
	}
	b, err := base64.RawURLEncoding.DecodeString(os.Args[2])
	if err != nil {
		return err
	}
	var app App
	if err = decodeJSON(b, &app); err != nil {
		return err
	}
	gate := os.NewFile(3, "launch-gate")
	if gate == nil {
		return errors.New("missing launch authorization pipe")
	}
	var permit [1]byte
	n, err := gate.Read(permit[:])
	gate.Close()
	if err != nil || n != 1 || permit[0] != 1 {
		return errors.New("supervisor did not authorize launch")
	}
	if err = executablePath(app.Path); err != nil {
		return err
	}
	cwd := app.Workdir
	if cwd == "" {
		cwd = filepath.Dir(app.Path)
	}
	if err = os.Chdir(cwd); err != nil {
		return err
	}
	return syscall.Exec(app.Path, append([]string{app.Path}, app.Args...), os.Environ())
}
func openLock(path string) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_RDWR|syscall.O_CREAT|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !fi.Mode().IsRegular() || !ok || int(st.Uid) != os.Geteuid() {
		f.Close()
		return nil, errors.New("lock must be a regular file owned by service user")
	}
	return f, nil
}
func ensureDaemon(p Paths, resume bool) error {
	if err := secureRuntime(p); err != nil {
		return err
	}
	if c, err := net.DialTimeout("unix", p.Socket, 300*time.Millisecond); err == nil {
		c.Close()
		return nil
	}
	f, err := openLock(p.Runtime + "/startup.lock")
	if err != nil {
		return err
	}
	defer f.Close()
	deadline := time.Now().Add(12 * time.Second)
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if err != syscall.EWOULDBLOCK {
			return err
		}
		if time.Now().After(deadline) {
			return errors.New("another supervisor start is still pending")
		}
		time.Sleep(30 * time.Millisecond)
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	if c, err := net.DialTimeout("unix", p.Socket, 300*time.Millisecond); err == nil {
		c.Close()
		return nil
	}
	_ = os.Remove(p.Runtime + "/daemon-error.json")
	self, err := os.Executable()
	if err != nil {
		return err
	}
	args := []string{"__daemon"}
	if !resume {
		args = append(args, "--no-resume")
	}
	cmd := exec.Command(self, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer null.Close()
	cmd.Stdin = null
	cmd.Stdout = null
	cmd.Stderr = null
	if err = cmd.Start(); err != nil {
		return err
	}
	_ = cmd.Process.Release()
	for time.Now().Before(deadline) {
		if c, err := net.DialTimeout("unix", p.Socket, 100*time.Millisecond); err == nil {
			c.Close()
			return nil
		}
		if b, err := readBounded(p.Runtime+"/daemon-error.json", 8192); err == nil {
			return fmt.Errorf("%s", b)
		}
		time.Sleep(35 * time.Millisecond)
	}
	return errors.New("supervisor did not become ready within 12 seconds")
}
func call(p Paths, req Request) (map[string]any, error) {
	c, err := net.DialTimeout("unix", p.Socket, time.Second)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(20 * time.Second))
	if err = json.NewEncoder(c).Encode(req); err != nil {
		return nil, err
	}
	var out map[string]any
	err = json.NewDecoder(io.LimitReader(c, 8*1024*1024)).Decode(&out)
	if err == nil {
		if _, ok := out["ok"].(bool); !ok {
			err = errors.New("invalid supervisor response")
		}
	}
	return out, err
}
func daemonMain(p Paths, resume bool) error {
	if err := secureRuntime(p); err != nil {
		return err
	}
	lock, err := openLock(p.Lock)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == syscall.EWOULDBLOCK {
		return nil
	} else if err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	s, err := newSupervisor(p)
	if err != nil {
		return err
	}
	if fi, err := os.Lstat(p.Socket); err == nil {
		if fi.Mode()&os.ModeSocket == 0 {
			return errors.New("refusing to replace a non-socket control path")
		}
		if err = os.Remove(p.Socket); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	ln, err := net.Listen("unix", p.Socket)
	if err != nil {
		return err
	}
	s.listener = ln
	defer ln.Close()
	if err = os.Chmod(p.Socket, 0600); err != nil {
		return err
	}
	if resume {
		s.mu.Lock()
		if s.configError == nil {
			for _, a := range s.apps {
				if a.Desired {
					s.start(a, false)
				}
			}
		}
		s.mu.Unlock()
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(signals)
	go func() {
		for {
			select {
			case <-ticker.C:
				s.tick()
			case <-signals:
				_ = s.handle(Request{Action: "shutdown"})
			case <-s.done:
				return
			}
		}
	}()
	sem := make(chan struct{}, 16)
	for {
		c, err := ln.Accept()
		if err != nil {
			select {
			case <-s.done:
				return nil
			default:
			}
			return err
		}
		select {
		case sem <- struct{}{}:
			go func() {
				defer func() { <-sem }()
				defer c.Close()
				_ = c.SetDeadline(time.Now().Add(20 * time.Second))
				line, err := bufio.NewReaderSize(io.LimitReader(c, maxJSON+2), maxJSON+2).ReadBytes('\n')
				if err != nil {
					if err != io.EOF || len(line) == 0 {
						return
					}
				}
				var req Request
				if err = decodeJSON(line, &req); err != nil {
					_ = json.NewEncoder(c).Encode(failure("INVALID_REQUEST", err))
					return
				}
				_ = json.NewEncoder(c).Encode(s.handle(req))
			}()
		default:
			c.Close()
		}
	}
}

func waitStopped(p Paths) error {
	f, err := openLock(p.Lock)
	if err != nil {
		return err
	}
	defer f.Close()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
			_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
			return nil
		}
		if err != syscall.EWOULDBLOCK {
			return err
		}
		time.Sleep(25 * time.Millisecond)
	}
	return errors.New("supervisor stop is still pending; retry status before another action")
}
