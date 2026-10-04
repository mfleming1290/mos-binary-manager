//go:build linux

package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type ProcessRef struct {
	PID     int    `json:"pid"`
	Start   string `json:"start"`
	Session int    `json:"session"`
}
type Owned struct {
	Token string       `json:"token"`
	Root  ProcessRef   `json:"root"`
	Known []ProcessRef `json:"known"`
}
type procInfo struct {
	ProcessRef
	PPID  int
	State string
}

const ownerKey = "BINARY_MANAGER_OWNER_TOKEN="
const sysPidfdOpen = 434
const sysPidfdSendSignal = 424

func processInfo(pid int) (procInfo, error) {
	var p procInfo
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return p, err
	}
	end := strings.LastIndexByte(string(b), ')')
	if end < 0 {
		return p, errors.New("invalid proc stat")
	}
	parts := strings.Fields(string(b[end+1:]))
	if len(parts) < 20 {
		return p, errors.New("short proc stat")
	}
	p.PID = pid
	p.State = parts[0]
	p.PPID, _ = strconv.Atoi(parts[1])
	p.Session, _ = strconv.Atoi(parts[3])
	p.Start = parts[19]
	return p, nil
}
func sameProcess(ref ProcessRef) bool {
	p, err := processInfo(ref.PID)
	return err == nil && p.Start == ref.Start && p.State != "Z" && p.State != "X"
}
func pidfdOpen(pid int) (int, error) {
	fd, _, e := syscall.Syscall(sysPidfdOpen, uintptr(pid), 0, 0)
	if e != 0 {
		return -1, e
	}
	return int(fd), nil
}

// Opening a pidfd then checking starttime prevents a recycled PID from becoming
// a kill target. No process-group kill and no name-based/adopted process kill.
func signalRef(ref ProcessRef, sig syscall.Signal) error {
	if ref.PID <= 1 || ref.PID == os.Getpid() || ref.Start == "" {
		return nil
	}
	fd, err := pidfdOpen(ref.PID)
	if err == syscall.ESRCH {
		return nil
	}
	if err != nil {
		return err
	}
	defer syscall.Close(fd)
	if !sameProcess(ref) {
		return nil
	}
	_, _, e := syscall.Syscall6(sysPidfdSendSignal, uintptr(fd), uintptr(sig), 0, 0, 0, 0)
	if e != 0 && e != syscall.ESRCH {
		return e
	}
	return nil
}
func processHasToken(pid int, token string) bool {
	if len(token) != 64 {
		return false
	}
	b, err := readBounded(fmt.Sprintf("/proc/%d/environ", pid), 1024*1024)
	if err != nil {
		return false
	}
	match := []byte(ownerKey + token)
	for _, v := range bytes.Split(b, []byte{0}) {
		if bytes.Equal(v, match) {
			return true
		}
	}
	return false
}
func collectOwned(o *Owned) []ProcessRef {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	all := map[int]procInfo{}
	owned := map[int]ProcessRef{}
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 1 || pid == os.Getpid() {
			continue
		}
		p, err := processInfo(pid)
		if err == nil && p.State != "Z" && p.State != "X" {
			all[pid] = p
		}
	}
	if p, ok := all[o.Root.PID]; ok && p.Start == o.Root.Start {
		owned[p.PID] = p.ProcessRef
	}
	for _, ref := range o.Known {
		if p, ok := all[ref.PID]; ok && p.Start == ref.Start {
			owned[p.PID] = p.ProcessRef
		}
	}
	for pid, p := range all {
		if _, ok := owned[pid]; !ok && processHasToken(pid, o.Token) {
			owned[pid] = p.ProcessRef
		}
	}
	// Include live descendants, including those that changed their session or
	// sanitized their environment, while an already-owned ancestor is observable.
	for changed := true; changed; {
		changed = false
		for pid, p := range all {
			if _, ok := owned[pid]; ok {
				continue
			}
			if parent, ok := owned[p.PPID]; ok && sameProcess(parent) {
				current, err := processInfo(pid)
				if err == nil && current.Start == p.Start && current.PPID == parent.PID {
					owned[pid] = p.ProcessRef
					changed = true
				}
			}
		}
	}
	out := make([]ProcessRef, 0, len(owned))
	for _, p := range owned {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PID < out[j].PID })
	o.Known = out
	return out
}
func terminateOwned(o *Owned, grace time.Duration) error {
	if o == nil {
		return nil
	}
	deadline := time.Now().Add(grace)
	sent := map[ProcessRef]bool{}
	var firstErr error
	for {
		refs := collectOwned(o)
		if len(refs) == 0 {
			return firstErr
		}
		for _, ref := range refs {
			if !sent[ref] {
				if err := signalRef(ref, syscall.SIGTERM); err != nil && firstErr == nil {
					firstErr = err
				}
				sent[ref] = true
			}
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(35 * time.Millisecond)
	}
	// Kill boundedly; keep discovering inherited-token descendants during the
	// grace window so a late fork does not escape ordinary stop/restart cleanup.
	killDeadline := time.Now().Add(time.Second)
	for {
		refs := collectOwned(o)
		if len(refs) == 0 {
			return firstErr
		}
		for _, ref := range refs {
			if err := signalRef(ref, syscall.SIGKILL); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		if time.Now().After(killDeadline) {
			return errors.New("owned processes did not exit within the bounded stop window")
		}
		time.Sleep(25 * time.Millisecond)
	}
}
