# Persistent instance settings

Binary Manager 0.2.2 keeps existing entries unchanged until you enable runtime settings in their editor. There is no automatic relocation of `/root`, dotfiles, API credentials or application state.

## Choose persistent storage first

Open **Runtime defaults** and choose a storage root within an actual mounted MOS pool, for example a directory you administer beneath your pool's real mount. Do not copy an example path without checking your host. The manager reads the kernel mount table before creating managed HOME directories and refuses absent, RAM-backed or unsupported storage. A missing pool causes a visible start failure with the normal bounded backoff; it must not silently create application state in the RAM root filesystem.

Supported HOME/XDG mounts are ext2/ext3/ext4, XFS, Btrfs, ZFS, bcachefs, NFS/NFS4 and CIFS, subject to ownership and permissions checks. Root/boot/system/RAM filesystems and mergerfs virtual pools are rejected. Use the underlying mounted persistent pool for this release; a mergerfs mount alone cannot prove its branches are safely mounted. Symlink components are not followed. The storage root and managed directories must be service-owned and mode 0700 once present; the manager creates missing directories with 0700 but does not loosen or rewrite permissions on existing paths.

Choose a dedicated private subdirectory, not the pool root. A native Btrfs pool may also hold Docker, LXC or libvirt data bound into system paths: an unrelated private sibling is eligible, while those service-data directories, their ancestors and bind aliases remain excluded. A pool's name does not identify its filesystem; `/mnt/user` can be a native Btrfs pool. Other supported filesystem types retain a conservative whole-device exclusion when also mounted under system paths, because their case-folding or remote naming rules need stronger identity checks. Boot and root devices remain excluded in full.

For example, if the host confirms `/mnt/user` is a mounted Btrfs pool, a new `/mnt/user/binary-manager` directory can be the managed storage root. Saving validates the path; the manager creates its private directories when an instance starts. Do not change the pool root's ownership or permissions to satisfy the managed-HOME check. If an existing chosen directory has incompatible ownership or permissions, select a new dedicated directory instead.

The managed layout is `<storage-root>/<instance-id>/home`. New directories are private to the service user. An instance ID stays stable when renamed, so renaming does not change its HOME. Changing the storage root selects new homes for managed instances; it does not move existing data. Stop and back up applications before manually migrating their state, and only then apply the new root. Removing an instance does not delete its HOME or executable.

## Opt an instance in

1. Open **Edit** and enable runtime settings
2. Choose a HOME mode: inherit the service HOME, use the managed per-instance HOME, or select an existing persistent custom HOME
3. Choose whether this instance uses the shared PATH/environment defaults
4. Leave the working directory blank to retain the executable-folder default, or set it explicitly. HOME and working directory are different settings
5. Apply the change. An affected instance with **Keep running** enabled is stopped and restarted; a stopped instance stays stopped

Leaving runtime settings disabled preserves the inherited legacy environment. Shared defaults alone never opt old instances in. Optional XDG paths are only set when explicitly supplied; the manager does not force applications to adopt a new XDG layout.

## PATH and ordinary environment variables

PATH entries are absolute directories, one per line. The resulting order is instance directories, then shared directories when enabled, then the inherited service PATH. No shell startup files are sourced, and `$HOME`, `~` or command substitutions are not expanded. Adding a directory does not install any software or change the executable path selected in the app configuration.

Ordinary environment entries use one literal `KEY=value` per line. `KEY=` sets an empty string. `!KEY` removes an inherited or shared value (stored as JSON null). Duplicate keys and invalid variable names are rejected. Precedence is the inherited service environment, shared values when enabled, instance values, then the protected file. HOME, PATH, XDG variables and `BINARY_MANAGER_*` are reserved for the manager and dedicated fields.

**Do not put secrets in these settings.** Ordinary environment values and executable arguments are visible in the browser and persisted in settings. Use the protected file mechanism for secret variables.

## Protected environment files

Create the file yourself on the host, in a protected location. It must be a regular, root-owned, single-link file with owner-only permissions (0600 or 0400). Symlinks in the file path and hard-linked files are rejected. Enter only its absolute path in the editor. Never paste its contents into chat, settings or arguments.

The file format is at most 64 KiB, with up to 256 literal `KEY=value` lines and values at most 16 KiB. Blank lines and lines beginning with `#` are ignored. Values may include spaces or `=`. An empty value stays empty. There is no `export`, shell sourcing, interpolation, quoting syntax or command execution. Do not wrap a value in shell quotes unless the quotes are part of the intended value. The same reserved-variable rules apply. Parse errors identify the problem without echoing secret contents.

The manager reads the file when starting the instance and sends the effective environment through a private launch pipe after recording process ownership. It does not return those values in status or place them in the child command line. Editing the file does not automatically restart a running program: use **Restart** for that instance to reload it.

This is protection against accidental disclosure, not a security sandbox. Root and appropriately privileged host processes can inspect process environments. A child program may print secrets into its logs, so inspect and share logs carefully.

## Duplicate an instance

**Duplicate** copies the executable, literal arguments and ordinary configuration to a new secure ID. The copy starts disabled, with **Start on boot** off and its own managed HOME. Its name identifies it as a copy. Explicit custom HOME/XDG paths and the protected-file reference are cleared, so you must intentionally reconnect any required credentials or state. Existing instances are not restarted.

The executable path can be shared by multiple entries. Each entry has separate controls, logs, process ownership and boot selection. Applications can still conflict on ports, lock files or state referenced in ordinary arguments/environment; review those values before starting the copy. A separate HOME is not a container or privilege boundary.

## Applying changes safely

Apply buttons are explicit. Name, boot selection and unrelated defaults do not restart unaffected instances. Execution-affecting edits, including effective HOME/PATH/environment changes, restart only affected instances whose current session intent is enabled. If safe shutdown cannot be verified, the manager refuses the unsafe replacement and reports the issue. Config writes retain revision checks to prevent two editors overwriting each other.

Do not downgrade to 0.1.x after enabling runtime settings or duplicate entries: those versions do not implement these settings and may reject duplicate executable paths. Keep a pre-update settings backup if you need an intentional rollback plan.

Use MOS's plugin **update** flow to preserve the existing configuration. This release does not require a NAS permission change, new daemon service, Prism installation or Selkies session. Real MOS boot/pool behavior must still be verified on an authorized host before relying on unattended startup.
