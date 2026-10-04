# Binary Manager 0.1.1

Fixes adding binaries when MOS is opened through a LAN HTTP address and the browser does not expose `crypto.randomUUID`.

- Generate secure UUID v4 app IDs with `crypto.getRandomValues` when `randomUUID` is unavailable
- Keep the native `randomUUID` path when supported; no weak randomness fallback
- Add regression coverage for both Add from Folder and Add from File flows without `randomUUID`

Upgrade an existing installation using MOS's plugin update flow, wait for completion, then reload MOS to refresh its cached plugin module. Do not reinstall over the existing installation: the update flow preserves settings.

The automated frontend tests cover the reported failure and both add flows. Actual MOS-browser, upgrade, reboot and native arm64 behavior still require verification on an authorized MOS host. Programs run as root on standard MOS; adding a binary does not start it.
