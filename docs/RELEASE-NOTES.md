# Binary Manager 0.2.1

Fixes an overly broad storage check that could reject a native Btrfs pool when MOS bind-mounts Docker, LXC or other system data from that pool.

- A separate app-data directory such as `/mnt/user/binary-manager` is now accepted on a verified native Btrfs pool even when unrelated system-data directories share the filesystem
- The backing system-data subtrees, their ancestors, descendants and bind aliases remain blocked; root and boot filesystem devices are still excluded
- Missing-mount, no-follow, private-directory ownership/permission and protected environment-file checks remain enforced
- Corrects the help text so `/mnt/user` is not assumed to be mergerfs; mergerfs-backed paths remain unsupported
- Retains the per-instance settings and multiple-instance support from 0.2.0 without migrating existing settings or moving application data

Use MOS's plugin update flow for an existing installation, wait for completion, and reload MOS. Then choose a dedicated app-data directory on the mounted Btrfs pool. Do not change ownership or permissions on the pool root or system-data folders. Changing folders in 0.2.0 alone does not fix its device-wide rejection.

Full backend race/Unix-socket tests, the mandatory root-owned protected-file test, frontend tests and both package validations are required before this tag is published. Physical MOS upgrade, real host mount behavior and native arm64 execution still need host verification; see docs/VERIFICATION.md.
