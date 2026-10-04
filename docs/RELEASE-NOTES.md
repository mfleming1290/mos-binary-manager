# Binary Manager 0.2.0

Persistent, opt-in per-instance runtime settings and multiple instances of the same binary.

- Managed HOME directories on verified mounted pool storage, with a separate HOME for each instance ID
- Explicit HOME and working-directory choices, additional PATH directories, ordinary environment overrides/unsets and optional XDG paths
- Shared storage/PATH/environment defaults, applied only where the instance opts in
- Protected environment-file references with literal parsing and private child-launch transport; file values are not placed in settings, status or command arguments
- Duplicate configuration with a new ID, separate managed HOME, disabled boot selection and no automatic start; secret-file and XDG references are cleared
- Applying changed settings restarts only affected enabled instances
- Existing settings, IDs, literal arguments, boot choices, unknown fields and revision checks remain compatible; old entries are not migrated automatically
- Retains the 0.1.1 secure UUID fallback for MOS served over LAN HTTP

Use MOS's plugin update flow for an existing installation, wait for completion, and reload MOS. Do not reinstall over existing settings. Back up application data separately before changing HOME; this release does not copy old dotfiles or credentials.

Programs still run as root on standard MOS. Ordinary settings are visible in the browser, and arbitrary application logs can reveal secrets. Protected environment files reduce accidental settings/argv exposure; they do not isolate root processes from one another.

This source is a prepared release. Publication, physical MOS installation, actual pool/boot behavior, and native arm64 runtime verification are separate steps. See docs/VERIFICATION.md for checks completed for this build.
