# Binary Manager for MOS NAS

Version **0.2.2**, prepared for MOS Hub distribution. Select a folder of standalone programs, add individual executable files, and supervise them without systemd.

## What it does

- Lists executable regular files directly inside your selected folder, plus individually added programs
- Lets you browse the NAS filesystem for a folder or executable, or enter an absolute path
- Keeps an explicitly enabled foreground program running and restarts it after a failure
- Starts programs with **Start on boot** checked during MOS's after-services phase, when pools and main services have been started
- Shows process state, PID, restart count, last exit/error and bounded recent output
- Accepts optional working directory and literal arguments, one argument per line; a blank working directory defaults to the executable’s folder
- Runs separately named instances of the same executable with independent IDs, controls, logs and boot selections
- Offers opt-in persistent per-instance HOME, additional PATH directories, ordinary environment overrides/unsets, protected environment-file references and shared defaults

Newly discovered files are never automatically added or executed. Adding a program does not start it. Neither this source tree nor building the package installs anything on a NAS.

## Important behavior and limits

**Programs run as root in the standard MOS setup.** Only add binaries you trust. This plugin is not a sandbox, permission manager, container or secret store. Keep binaries on storage that untrusted users cannot replace. An enabled program can make any changes that root can make.

Run programs in the **foreground**. Disable their own daemon/background mode. This release does not adopt processes started elsewhere. Avoid managing the same program through another startup mechanism; a second independently started instance is outside the plugin's ownership.

- **Keep running** controls the current boot session. Turning it off stops the program and its owned descendants
- **Start on boot** is independent and persistent. A checked program starts again next boot even if you manually stopped it this session. Uncheck it to prevent that
- Nonzero exits and terminating signals restart with exponential backoff, from 1 second to at most 30 seconds. A stable run resets the delay
- A clean exit (code 0) stays stopped. Use **Restart** or toggle on to run it again
- Applying execution-setting changes restarts only affected instances whose Keep running is enabled; stopped instances stay stopped. Renaming and boot selection do not restart an app
- Up to 32 configured programs. Discovery is nonrecursive and reads at most 2,048 directory entries; large folders show a truncation warning. Symlink binaries are not accepted; pick the actual executable
- Logs are bounded, transient and private to this runtime. Configure a program's own persistent logs on pool storage if needed; do not put busy logs on boot media
- Linux with pidfd support (kernel 5.3+) is required for safe signaling. The helper fails closed if that support is unavailable
- A stopped/crashed supervisor is recovered by the next plugin request or boot hook. This is a small homelab supervisor, not a high-availability service manager

Settings and boot selections persist under `/boot/optional/plugins/binary-manager/settings.json`. Live state, socket and logs belong under `/run/binary-manager`. Config writes use revision checks and atomic replacement; two tabs cannot silently overwrite each other's plugin edits. Do not edit its JSON while the supervisor is running or use MOS's generic settings POST for this plugin.

## Persistent instance settings

Existing entries retain their inherited HOME and environment until you enable their runtime settings. No dotfiles, credentials or application state are automatically moved. Start with [the instance setup guide](docs/INSTANCE-SETTINGS.md) to choose pool-backed storage, isolate duplicates and configure PATH or environment variables. Shared defaults affect only opted-in instances; setting defaults does not migrate old apps.

Protected environment files are read by the host and passed to the child privately. Only their file paths are saved in settings. Ordinary environment values, executable arguments and other settings are visible to the browser and must not contain secrets. Child programs can print secrets into their own logs; the manager cannot guarantee redaction.

## Install through MOS Hub

This package is prepared for the supplied `mfleming1290/mos-hub` catalog, with `mfleming1290/mos-binary-manager` as the confirmed release repository. The prepared source does not itself create either a live release or a Hub entry. Publishing instructions and the ready-to-copy catalog files are in [docs/HUB-RELEASE.md](docs/HUB-RELEASE.md) and `hub/`.

Once the GitHub release has passed the workflow and the Hub entry has been published:

1. Refresh the configured MOS Hub repository and find **Binary Manager** under **Utilities**
2. For a first installation, select numeric release **0.2.2** and install. For an existing installation, use MOS's **plugin update** flow instead. MOS selects the matching `amd64` or `arm64` package and retrieves the tag's source files itself
3. Wait for the MOS completion notification, reload MOS, then open **Plugins → Binary Manager**
4. Select a binaries folder or add one executable. Explicitly turn on **Keep running** to start it. Use **Start on boot** separately
5. Test stop, logs, an intentional failure/restart, a clean exit, and finally an actual reboot before relying on it

**This release has not been installed or reboot-tested on a physical MOS host.** Begin with the harmless foreground programs in `tests/fixtures/`: `loop.sh`, `fail.sh` (intentional exit 7), and `exit-clean.sh` (exit 0). Place them on executable pool storage and add them explicitly. If the ZIP extractor does not retain execute permission, run `chmod +x` on those three files. Turn off the failure fixture when finished.

Do not use Hub's initial-install route to replace an existing local test installation. MOS can reject an equal tag; a different-tag install can replace defaults and remove old files. Back up settings and resolve that migration before proceeding. For existing Hub installations use MOS's plugin update flow, which preserves `settings.json`, rather than reinstalling from Hub.

The original first-install helper remains an optional, offline test fallback in [docs/LOCAL-INSTALL.md](docs/LOCAL-INSTALL.md). It is not needed for the Hub route and does not establish a Hub update source. Installing a `.deb` alone is insufficient for MOS's persistent boot registration.

## Removal and upgrades

Stop your programs and uncheck their boot boxes first. Back up settings if you want to keep your list. MOS's plugin removal flow removes the plugin's persistent configuration; it does not delete your executable files or their application data. The package's removal hook also asks the supervisor to stop owned processes. It never uses a broad `pkill` match.

MOS's plugin update flow preserves `settings.json`, records the new numeric tag directory, and invokes `plugin_update`. Package upgrades stop the old owned runtime with session intent preserved; the update hook starts the replacement. Reload MOS after an update so its cached federation module is refreshed. Do not use the first-install helper or Hub reinstall route to overwrite an existing installation.

## Development

Requirements: official Go compiler with support for this module's Go version, Node 20.19+ or 22.12+, and `dpkg-deb`. No third-party Go dependencies or extra runtime daemon is required.

```bash
cd backend
GOTOOLCHAIN=local go test -race ./...
cd ../page
npm ci --ignore-scripts --no-audit --no-fund
npm test
npm run build
npm run package
```

The GitHub workflow runs the full backend suite (including Unix-socket integration), frontend tests, build and package checks before publishing. See [docs/HUB-RELEASE.md](docs/HUB-RELEASE.md) for numeric tags, source-version checks and the catalog addition.

The package builder produces both architectures by default. Use `ARCHES=amd64 npm run package` for one architecture. Set `GO=/absolute/path/to/go` if Go is not on your path. It refuses to overwrite existing artifacts. Static binaries use `CGO_ENABLED=0`.

For safe local process tests, set `BINARY_MANAGER_ROOT` to a fresh temporary directory. The backend's own tests create harmless fixture programs and never execute unknown user binaries. See `docs/VERIFICATION.md` for the actual checks run for this build.

## MOS integration contract

The page is a shared-Vue federated component exposed as `./Plugin` from `remoteEntry.js`. Its narrow packaged command is `/usr/bin/plugins/binary-manager`, invoked through MOS's authenticated `/api/v1/mos/plugins/query`. Requests are JSON encoded as URL-safe base64, with literal argv execution inside the helper. This avoids placing arbitrary file paths or arguments in MOS's shell-built query string.

The hook file is side-effect-free when sourced. `install` and `plugin_update` start the supervisor with existing session intent; `mos_start_after_services` applies boot selections once per boot; `uninstall` stops owned work and clears session intent. There is no early-boot application start, network listener, systemd unit, cron change or runtime package download.

Built against the MOS plugin contract snapshot at mos-api `7281c01150b081b0e0e9e1d44048e0e85a0eacb2`, mos-frontend `fa41c8318c9889dbb7ee1cf1bbb491258f2bad6b`, and mos-rootfs `2099a66451324b558ef3b178f10a62e9e707524b`. Different MOS versions still need their own host verification.
