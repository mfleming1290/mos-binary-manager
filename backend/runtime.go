package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

const maxEnvFile = 64 * 1024
const maxLaunchJSON = 512 * 1024
const maxEnvEntries = 256

// Plain JSON is for ordinary settings only. Credentials belong in a separately
// provisioned root-owned protected envFile, never in settings.
type RuntimeDefaults struct {
	StorageRoot string                     `json:"storageRoot"`
	PathDirs    []string                   `json:"pathDirs"`
	Env         map[string]*string         `json:"env"`
	Extra       map[string]json.RawMessage `json:"-"`
}
type RuntimeSettings struct {
	UseDefaults   bool                       `json:"useDefaults"`
	HomeMode      string                     `json:"homeMode"`
	Home          string                     `json:"home"`
	PathDirs      []string                   `json:"pathDirs"`
	Env           map[string]*string         `json:"env"`
	EnvFile       string                     `json:"envFile"`
	XDGConfigHome string                     `json:"xdgConfigHome"`
	XDGDataHome   string                     `json:"xdgDataHome"`
	XDGCacheHome  string                     `json:"xdgCacheHome"`
	Extra         map[string]json.RawMessage `json:"-"`
}
type launchConfig struct {
	Path    string   `json:"path"`
	Args    []string `json:"args"`
	Workdir string   `json:"workdir"`
	Env     []string `json:"env"`
}

func runtimeExtras(b []byte, keys []string) (map[string]json.RawMessage, error) {
	var extra map[string]json.RawMessage
	if err := json.Unmarshal(b, &extra); err != nil {
		return nil, err
	}
	if extra == nil {
		return nil, errors.New("runtime settings must be an object")
	}
	for _, key := range keys {
		if v, ok := extra[key]; ok && bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return nil, fmt.Errorf("%s cannot be null", key)
		}
		delete(extra, key)
	}
	return extra, nil
}
func (r *RuntimeDefaults) UnmarshalJSON(b []byte) error {
	type plain RuntimeDefaults
	var v plain
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	extra, err := runtimeExtras(b, []string{"storageRoot", "pathDirs", "env"})
	if err != nil {
		return err
	}
	v.Extra = extra
	*r = RuntimeDefaults(v)
	return nil
}
func (r RuntimeDefaults) MarshalJSON() ([]byte, error) {
	type plain RuntimeDefaults
	if r.PathDirs == nil {
		r.PathDirs = []string{}
	}
	if r.Env == nil {
		r.Env = map[string]*string{}
	}
	return marshalExtras(plain(r), r.Extra)
}
func (r *RuntimeSettings) UnmarshalJSON(b []byte) error {
	type plain RuntimeSettings
	var v plain
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	extra, err := runtimeExtras(b, []string{"useDefaults", "homeMode", "home", "pathDirs", "env", "envFile", "xdgConfigHome", "xdgDataHome", "xdgCacheHome"})
	if err != nil {
		return err
	}
	v.Extra = extra
	*r = RuntimeSettings(v)
	return nil
}
func (r RuntimeSettings) MarshalJSON() ([]byte, error) {
	type plain RuntimeSettings
	if r.PathDirs == nil {
		r.PathDirs = []string{}
	}
	if r.Env == nil {
		r.Env = map[string]*string{}
	}
	return marshalExtras(plain(r), r.Extra)
}
func mergeExtra(dst *map[string]json.RawMessage, src map[string]json.RawMessage) {
	if *dst == nil {
		*dst = map[string]json.RawMessage{}
	}
	for k, v := range src {
		if _, ok := (*dst)[k]; !ok {
			(*dst)[k] = v
		}
	}
}
func mergeRuntimeUnknown(dst, src *RuntimeDefaults) {
	if dst != nil && src != nil {
		mergeExtra(&dst.Extra, src.Extra)
	}
}
func mergeAppRuntimeUnknown(dst, src *RuntimeSettings) {
	if dst != nil && src != nil {
		mergeExtra(&dst.Extra, src.Extra)
	}
}

var envKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func allowedEnvKey(k string) bool {
	return envKey.MatchString(k) && len(k) <= 128 && !strings.HasPrefix(k, "BINARY_MANAGER_") && k != "HOME" && k != "PATH" && !strings.HasPrefix(k, "XDG_")
}
func validateEnv(env map[string]*string) error {
	if len(env) > maxEnvEntries {
		return errors.New("at most 256 environment entries are supported")
	}
	for k, v := range env {
		if !allowedEnvKey(k) {
			return errors.New("invalid or reserved environment key; use dedicated HOME, PATH and XDG fields")
		}
		if v != nil && (len(*v) > 16384 || strings.ContainsAny(*v, "\x00\r\n")) {
			return errors.New("environment values must be at most 16 KiB without NUL or newlines")
		}
	}
	return nil
}
func validatePathDirs(dirs []string) error {
	if len(dirs) > 64 {
		return errors.New("at most 64 PATH directories are supported")
	}
	for _, p := range dirs {
		if err := validAbsolute(p, false); err != nil || strings.Contains(p, ":") {
			return errors.New("PATH directories must be clean absolute paths without colons or control characters")
		}
	}
	return nil
}
func validateRuntimeDefaults(r *RuntimeDefaults) error {
	if r == nil {
		return nil
	}
	if err := validAbsolute(r.StorageRoot, true); err != nil {
		return fmt.Errorf("storageRoot: %w", err)
	}
	if r.StorageRoot == "/" {
		return errors.New("storageRoot cannot be /")
	}
	if err := validatePathDirs(r.PathDirs); err != nil {
		return err
	}
	if err := validateEnv(r.Env); err != nil {
		return err
	}
	if r.PathDirs == nil {
		r.PathDirs = []string{}
	}
	if r.Env == nil {
		r.Env = map[string]*string{}
	}
	return nil
}
func validateRuntimeSettings(r *RuntimeSettings, d *RuntimeDefaults) error {
	if r == nil {
		return nil
	}
	if r.HomeMode == "" {
		r.HomeMode = "inherit"
	}
	switch r.HomeMode {
	case "inherit":
	case "managed":
		if d == nil || d.StorageRoot == "" {
			return errors.New("managed HOME requires runtimeDefaults.storageRoot")
		}
	case "custom":
		if r.Home == "" {
			return errors.New("custom HOME requires an explicit path")
		}
	default:
		return errors.New("homeMode must be inherit, managed or custom")
	}
	for _, p := range []string{r.Home, r.EnvFile, r.XDGConfigHome, r.XDGDataHome, r.XDGCacheHome} {
		if err := validAbsolute(p, true); err != nil {
			return err
		}
	}
	if r.Home == "/" {
		return errors.New("HOME cannot be /")
	}
	if err := validatePathDirs(r.PathDirs); err != nil {
		return err
	}
	if err := validateEnv(r.Env); err != nil {
		return err
	}
	if r.PathDirs == nil {
		r.PathDirs = []string{}
	}
	if r.Env == nil {
		r.Env = map[string]*string{}
	}
	return nil
}
func baseEnvironment() []string {
	out := []string{}
	for _, v := range os.Environ() {
		k, _, ok := strings.Cut(v, "=")
		if ok && !strings.HasPrefix(k, "BINARY_MANAGER_") {
			out = append(out, v)
		}
	}
	return out
}
func envMap(values []string) map[string]string {
	out := map[string]string{}
	for _, v := range values {
		if k, val, ok := strings.Cut(v, "="); ok {
			out[k] = val
		}
	}
	return out
}
func sortedEnv(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+values[k])
	}
	return out
}
func overlayEnv(dst map[string]string, src map[string]*string) {
	for k, v := range src {
		if v == nil {
			delete(dst, k)
		} else {
			dst[k] = *v
		}
	}
}

// No shell, interpolation, quoting or command substitution is performed. Empty
// values are literal empty strings. JSON null is the only unset notation.
func parseProtectedEnv(b []byte) (map[string]*string, error) {
	if len(b) > maxEnvFile {
		return nil, errors.New("protected environment file exceeds 64 KiB")
	}
	out := map[string]*string{}
	for i, line := range strings.Split(string(b), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || !allowedEnvKey(key) || len(value) > 16384 || strings.ContainsAny(value, "\x00\r") {
			return nil, fmt.Errorf("invalid protected environment entry at line %d", i+1)
		}
		if _, exists := out[key]; exists {
			return nil, fmt.Errorf("duplicate protected environment key at line %d", i+1)
		}
		val := value
		out[key] = &val
		if len(out) > maxEnvEntries {
			return nil, errors.New("protected environment file has too many entries")
		}
	}
	return out, nil
}
func openDirectoryNoLinks(path string) (*os.File, error) {
	if err := validAbsolute(path, false); err != nil {
		return nil, err
	}
	fd, err := syscall.Open("/", syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	for _, part := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		if part == "" {
			continue
		}
		next, e := syscall.Openat(fd, part, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
		syscall.Close(fd)
		if e != nil {
			return nil, e
		}
		fd = next
	}
	return os.NewFile(uintptr(fd), path), nil
}
func readProtectedEnv(path string) (map[string]*string, error) {
	if err := validAbsolute(path, false); err != nil {
		return nil, errors.New("invalid protected environment file path")
	}
	dir, err := openDirectoryNoLinks(filepath.Dir(path))
	if err != nil {
		return nil, errors.New("cannot securely open protected environment file directory")
	}
	defer dir.Close()
	fd, err := syscall.Openat(int(dir.Fd()), filepath.Base(path), syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, errors.New("cannot securely open protected environment file")
	}
	f := os.NewFile(uintptr(fd), "protected-environment")
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, errors.New("cannot inspect protected environment file")
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !fi.Mode().IsRegular() || !ok || st.Uid != 0 || (fi.Mode().Perm() != 0600 && fi.Mode().Perm() != 0400) || fi.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 || st.Nlink != 1 {
		return nil, errors.New("protected environment file must be root-owned, regular, single-link, and mode 0600 or 0400")
	}
	b, err := io.ReadAll(io.LimitReader(f, maxEnvFile+1))
	if err != nil {
		return nil, errors.New("cannot read protected environment file")
	}
	return parseProtectedEnv(b)
}

// Mount IDs are checked again on opened directory descriptors. Directory walks
// never follow symlinks, and mkdirat stays anchored to the verified mount even
// if a pool is concurrently unmounted (it cannot fall back to the boot root).
type mountRecord struct{ ID, Device, Root, Point, FSType string }

func mountUnescape(s string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' {
			b.WriteByte(s[i])
			continue
		}
		if i+3 >= len(s) {
			return "", errors.New("invalid mount escape")
		}
		v, err := strconv.ParseUint(s[i+1:i+4], 8, 8)
		if err != nil {
			return "", errors.New("invalid mount escape")
		}
		b.WriteByte(byte(v))
		i += 3
	}
	return b.String(), nil
}
func parseMountInfo(b []byte) ([]mountRecord, error) {
	out := []mountRecord{}
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		f := strings.Fields(line)
		separator := -1
		for i, v := range f {
			if v == "-" {
				separator = i
				break
			}
		}
		if len(f) < 10 || separator < 6 || len(f) < separator+4 {
			return nil, errors.New("invalid kernel mount information")
		}
		point, err := mountUnescape(f[4])
		if err != nil {
			return nil, err
		}
		root, err := mountUnescape(f[3])
		if err != nil {
			return nil, err
		}
		out = append(out, mountRecord{ID: f[0], Device: f[2], Root: root, Point: point, FSType: f[separator+1]})
	}
	return out, nil
}
func readMounts() ([]mountRecord, error) {
	b, err := readBounded("/proc/self/mountinfo", 4*1024*1024)
	if err != nil {
		return nil, errors.New("cannot verify persistent storage mounts")
	}
	return parseMountInfo(b)
}
func pathWithin(p, root string) bool {
	return p == root || strings.HasPrefix(p, strings.TrimSuffix(root, "/")+"/")
}
func persistentMount(path string, mounts []mountRecord) (mountRecord, error) {
	var chosen mountRecord
	rootDevice := ""
	for _, m := range mounts {
		if m.Point == "/" {
			rootDevice = m.Device
		}
		if pathWithin(path, m.Point) && len(m.Point) > len(chosen.Point) {
			chosen = m
		}
	}
	for _, system := range []string{"/boot", "/run", "/dev", "/proc", "/sys", "/etc", "/usr", "/var", "/tmp", "/bin", "/sbin", "/lib", "/lib64"} {
		if pathWithin(path, system) || pathWithin(chosen.Point, system) {
			return mountRecord{}, errors.New("HOME storage cannot use boot, RAM or system directories")
		}
	}
	if chosen.Point == "" || chosen.Point == "/" || chosen.Device == rootDevice {
		return mountRecord{}, errors.New("HOME storage requires a separate mounted persistent pool; refusing root-filesystem fallback")
	}
	// mountinfo's Root is the mounted subtree's location inside its filesystem,
	// not its visible mount point. MOS binds pool service directories into /var
	// and /etc; those binds must not disqualify unrelated private Btrfs siblings.
	rel, err := filepath.Rel(chosen.Point, path)
	if err != nil {
		return mountRecord{}, errors.New("cannot verify HOME storage location")
	}
	filesystemPath := filepath.Join(chosen.Root, rel)
	for _, m := range mounts {
		for _, system := range []string{"/boot", "/usr", "/var", "/etc"} {
			if pathWithin(m.Point, system) && m.Device == chosen.Device {
				// Boot media remains excluded device-wide. Other filesystem types
				// keep the conservative device-wide rule: their case-folding or
				// server-side naming can make lexical subtree checks insufficient.
				if system == "/boot" || chosen.FSType != "btrfs" || m.FSType != "btrfs" {
					return mountRecord{}, errors.New("HOME storage cannot use a system or boot filesystem")
				}
				if !filepath.IsAbs(chosen.Root) || filepath.Clean(chosen.Root) != chosen.Root || !filepath.IsAbs(m.Root) || filepath.Clean(m.Root) != m.Root {
					return mountRecord{}, errors.New("cannot verify HOME storage filesystem root")
				}
				if pathWithin(filesystemPath, m.Root) || pathWithin(m.Root, filesystemPath) {
					return mountRecord{}, errors.New("HOME storage cannot overlap a system-data directory; choose a separate private directory on the pool")
				}
			}
		}
	}
	switch chosen.FSType {
	case "ext2", "ext3", "ext4", "xfs", "btrfs", "zfs", "bcachefs", "nfs", "nfs4", "cifs":
	case "fuse.mergerfs", "mergerfs":
		return mountRecord{}, errors.New("HOME storage cannot use a mergerfs virtual pool; choose an underlying mounted persistent pool so missing branches cannot fall back to RAM")
	default:
		return mountRecord{}, errors.New("HOME storage mount is not a supported persistent filesystem")
	}
	return chosen, nil
}
func fdMountID(f *os.File) (string, error) {
	b, err := os.ReadFile(fmt.Sprintf("/proc/self/fdinfo/%d", f.Fd()))
	if err != nil {
		return "", err
	}
	scanner := bufio.NewScanner(bytes.NewReader(b))
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), "mnt_id:") {
			return strings.TrimSpace(strings.TrimPrefix(scanner.Text(), "mnt_id:")), nil
		}
	}
	return "", errors.New("cannot verify directory mount identity")
}
func checkPrivateDirectory(f *os.File) error {
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok || int(st.Uid) != os.Geteuid() || fi.Mode().Perm() != 0700 {
		return errors.New("managed HOME directories must be service-owned and mode 0700")
	}
	return nil
}
func persistentHome(path, privateRoot string, create bool, mounts []mountRecord) error {
	if err := validAbsolute(path, false); err != nil {
		return err
	}
	m, err := persistentMount(path, mounts)
	if err != nil {
		return err
	}
	if privateRoot != "" && m.Point != privateRoot && pathWithin(m.Point, privateRoot) {
		return errors.New("managed HOME tree cannot contain another mount")
	}
	f, err := openDirectoryNoLinks(m.Point)
	if err != nil {
		return errors.New("persistent HOME mount is unavailable or contains symlinks")
	}
	defer func() { f.Close() }()
	if id, e := fdMountID(f); e != nil || id != m.ID {
		return errors.New("persistent HOME mount changed; refusing directory creation")
	}
	current := m.Point
	rel, err := filepath.Rel(m.Point, path)
	if err != nil {
		return err
	}
	if privateRoot == m.Point {
		if err := checkPrivateDirectory(f); err != nil {
			return err
		}
	}
	if rel == "." {
		return nil
	}
	missing := false
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		if missing {
			continue
		}
		next, e := syscall.Openat(int(f.Fd()), part, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
		if e == syscall.ENOENT && privateRoot != "" {
			if !create {
				missing = true
				continue
			}
			if e = syscall.Mkdirat(int(f.Fd()), part, 0700); e != nil && e != syscall.EEXIST {
				return errors.New("cannot create managed HOME directory")
			}
			next, e = syscall.Openat(int(f.Fd()), part, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
		}
		if e != nil {
			return errors.New("HOME directory is missing, inaccessible or contains symlinks")
		}
		f.Close()
		f = os.NewFile(uintptr(next), current)
		if id, e := fdMountID(f); e != nil || id != m.ID {
			return errors.New("HOME directory crossed an unexpected mount")
		}
		if privateRoot != "" && pathWithin(current, privateRoot) {
			if e := checkPrivateDirectory(f); e != nil {
				return e
			}
		}
	}
	return nil
}
func configuredHome(a App, d *RuntimeDefaults) string {
	if a.Runtime == nil {
		return ""
	}
	switch a.Runtime.HomeMode {
	case "managed":
		if d != nil {
			return filepath.Join(d.StorageRoot, a.ID, "home")
		}
	case "custom":
		return a.Runtime.Home
	}
	return ""
}
func configuredEnvironment(a App, d *RuntimeDefaults) map[string]string {
	env := envMap(baseEnvironment())
	r := a.Runtime
	if r == nil {
		return env
	}
	if r.UseDefaults && d != nil {
		overlayEnv(env, d.Env)
	}
	overlayEnv(env, r.Env)
	dirs := append([]string{}, r.PathDirs...)
	if r.UseDefaults && d != nil {
		dirs = append(dirs, d.PathDirs...)
	}
	if len(dirs) > 0 {
		if base, ok := env["PATH"]; ok && base != "" {
			dirs = append(dirs, base)
		}
		env["PATH"] = strings.Join(dirs, ":")
	}
	if home := configuredHome(a, d); home != "" {
		env["HOME"] = home
	}
	for k, v := range map[string]string{"XDG_CONFIG_HOME": r.XDGConfigHome, "XDG_DATA_HOME": r.XDGDataHome, "XDG_CACHE_HOME": r.XDGCacheHome} {
		if v != "" {
			env[k] = v
		}
	}
	return env
}
func prepareRuntime(a App, d *RuntimeDefaults, create bool, mountSource func() ([]mountRecord, error)) ([]string, error) {
	env, _, err := prepareRuntimeDetails(a, d, create, mountSource)
	return env, err
}
func prepareRuntimeDetails(a App, d *RuntimeDefaults, create bool, mountSource func() ([]mountRecord, error)) ([]string, []string, error) {
	env := configuredEnvironment(a, d)
	keys := []string{}
	if a.Runtime == nil {
		return sortedEnv(env), keys, nil
	}
	// Validate and read the protected file before creating any managed directories.
	if a.Runtime.EnvFile != "" {
		values, err := readProtectedEnv(a.Runtime.EnvFile)
		if err != nil {
			return nil, nil, err
		}
		overlayEnv(env, values)
		for key := range values {
			keys = append(keys, key)
		}
		sort.Strings(keys)
	}
	home := configuredHome(a, d)
	if home != "" || a.Runtime.XDGConfigHome != "" || a.Runtime.XDGDataHome != "" || a.Runtime.XDGCacheHome != "" {
		if mountSource == nil {
			mountSource = readMounts
		}
		mounts, err := mountSource()
		if err != nil {
			return nil, nil, err
		}
		for _, p := range []string{a.Runtime.XDGConfigHome, a.Runtime.XDGDataHome, a.Runtime.XDGCacheHome} {
			if p != "" {
				if err := persistentHome(p, "", false, mounts); err != nil {
					return nil, nil, fmt.Errorf("XDG directory: %w", err)
				}
			}
		}
		if home != "" {
			privateRoot := ""
			if a.Runtime.HomeMode == "managed" {
				privateRoot = d.StorageRoot
			}
			if err := persistentHome(home, privateRoot, create, mounts); err != nil {
				return nil, nil, err
			}
		}
	}
	return sortedEnv(env), keys, nil
}
func sameExecution(a App, ad *RuntimeDefaults, b App, bd *RuntimeDefaults) bool {
	return sameExecutionWithKeys(a, ad, b, bd, nil)
}
func sameExecutionWithKeys(a App, ad *RuntimeDefaults, b App, bd *RuntimeDefaults, protectedKeys []string) bool {
	if a.Path != b.Path || a.Workdir != b.Workdir || !reflect.DeepEqual(a.Args, b.Args) {
		return false
	}
	af, bf := "", ""
	if a.Runtime != nil {
		af = a.Runtime.EnvFile
	}
	if b.Runtime != nil {
		bf = b.Runtime.EnvFile
	}
	if af != bf || ((a.Runtime == nil) != (b.Runtime == nil)) {
		return false
	}
	ae, be := configuredEnvironment(a, ad), configuredEnvironment(b, bd)
	// Only names from the last successful launch are retained. File edits remain
	// explicit Restart operations, and protected values never enter comparison
	// state. An unchanged file's overrides mask ordinary/default env edits.
	if af != "" {
		for _, key := range protectedKeys {
			delete(ae, key)
			delete(be, key)
		}
	}
	return reflect.DeepEqual(ae, be)
}
func sameRuntimeSafetyInputs(a App, ad *RuntimeDefaults, b App, bd *RuntimeDefaults) bool {
	if a.Runtime == nil || b.Runtime == nil {
		return a.Runtime == nil && b.Runtime == nil
	}
	ar, br := a.Runtime, b.Runtime
	return ar.HomeMode == br.HomeMode && configuredHome(a, ad) == configuredHome(b, bd) && ar.EnvFile == br.EnvFile && ar.XDGConfigHome == br.XDGConfigHome && ar.XDGDataHome == br.XDGDataHome && ar.XDGCacheHome == br.XDGCacheHome
}
func validateChangedRuntime(c, old Config, mounts func() ([]mountRecord, error), running map[string]*RuntimeApp) error {
	if c.RuntimeDefaults != nil && c.RuntimeDefaults.StorageRoot != "" && (old.RuntimeDefaults == nil || c.RuntimeDefaults.StorageRoot != old.RuntimeDefaults.StorageRoot) {
		source := mounts
		if source == nil {
			source = readMounts
		}
		records, err := source()
		if err != nil {
			return err
		}
		root := c.RuntimeDefaults.StorageRoot
		if err := persistentHome(root, root, false, records); err != nil {
			return fmt.Errorf("storageRoot: %w", err)
		}
	}
	prior := map[string]App{}
	for _, a := range old.Apps {
		prior[a.ID] = a
	}
	for _, a := range c.Apps {
		before, exists := prior[a.ID]
		var keys []string
		if previous := running[a.ID]; previous != nil {
			keys = previous.ProtectedKeys
		}
		if a.Runtime != nil && (!exists || !sameRuntimeSafetyInputs(a, c.RuntimeDefaults, before, old.RuntimeDefaults) || !sameExecutionWithKeys(a, c.RuntimeDefaults, before, old.RuntimeDefaults, keys)) {
			if _, err := prepareRuntime(a, c.RuntimeDefaults, false, mounts); err != nil {
				return fmt.Errorf("%s runtime: %w", a.ID, err)
			}
		}
	}
	return nil
}
