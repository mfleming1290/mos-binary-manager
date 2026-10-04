# Binary Manager 0.1.0

Initial MOS Hub test release for amd64 and arm64.

- Browse executable files, explicitly add trusted foreground programs, and control them from the MOS plugin page
- Independent keep-running and start-on-boot controls, restart-on-failure backoff, bounded logs and literal arguments
- Private local supervisor with safe owned-process shutdown and persistent settings
- Architecture-specific Debian packages, MD5 for the MOS installer and SHA-256 for manual verification

Programs run as root on standard MOS. No application starts merely because it is discovered or added. A clean exit stays stopped.

Installation, upgrade and reboot behavior still require verification on an authorized MOS test host. Native arm64 execution is not yet verified. A successful CI build does not establish these host results.

Install through MOS Hub after the Binary Manager catalog entry is available. Reload MOS after installation or an update. See README.md for behavior and docs/HUB-RELEASE.md for publishing details.
