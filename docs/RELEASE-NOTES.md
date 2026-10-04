# Binary Manager 0.2.2

Fixes text fields losing focus when the three-second background status poll runs.

- Folder, executable, instance settings and runtime-default fields stay editable during a healthy background refresh
- Poll responses preserve unsaved text, cursor selection and each existing draft's original configuration revision
- Save, Add and process-changing actions remain serialized with status requests; stale status, failed revisions and active mutations still block unsafe changes
- Adds regression coverage for all four editing flows, unchanged and newer snapshots, keyboard submission during polling and request mutual exclusion
- Retains the Btrfs storage correction from 0.2.1 and all existing storage, process and protected-file safeguards

Use MOS's plugin update flow for an existing installation, wait for completion, and reload MOS so the cached frontend is refreshed. No configuration migration or application-data move is required.

The local component tests cover effective editability, draft identity, selection and concurrency. Actual MOS-browser focus behavior still requires host verification because the cloud browser cannot open the local preview. Full backend race/Unix-socket tests, the mandatory root-owned protected-file test and both package validations remain required before publication; see docs/VERIFICATION.md.
