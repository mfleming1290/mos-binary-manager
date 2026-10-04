# Verification: Binary Manager 0.1.1

## 0.1.1 patch

The frontend fix was independently reviewed and passed all 30 frontend API/model/component tests plus the production federation build before release preparation. Coverage includes the native UUID path, a secure UUID v4 fallback, missing secure randomness, and both add flows with `randomUUID` unavailable. Actual MOS-browser testing remains unverified.

The release workflow must pass its complete backend race/socket suite, frontend tests and dual-architecture package checks on the exact 0.1.1 commit before publication.

## Historical 0.1.0 preparation record

This is a **test build**, compiled and checked in a cloud Linux workspace. It has not been installed on a MOS NAS.

## Passed locally

- Official Go 1.27.1 toolchain archive SHA-256 checked against Go's published release metadata before use
- Go formatting and `go vet`
- Sixteen focused backend unit and real-process engine tests under the Go race detector, with zero race reports; the prior fifteen-test selection also passed three consecutive runs
- Real, harmless foreground process start/stop, literal/Unicode arguments, executable-folder default working directory, clean exit, failure restart/backoff, boot intent and idempotence, and conservative owned-process recovery through the production supervisor engine
- PID/start-time ownership and unrelated stale-PID preservation; bounded memory/disk logs; lock contention; strict settings validation, unknown-field preservation, atomic persistence and revision conflicts
- Independent compiled-helper launch-gate check: invalid/EOF authorization never executes a fixture; a valid gate preserves exact argv and working directory without shell expansion
- Twenty-five frontend API/model/component interaction tests against the actual Vue components in a DOM harness
- Seven independent transport/model regressions, including shell metacharacters, Unicode and literal empty arguments
- Production Vite federation build exposing `./Plugin`, manifest and assets
- Both Debian packages independently extracted and verified: correct static ELF64 architecture, no dynamic interpreter/dependencies, runtime executable permissions, root/root archive ownership, exact frontend asset bytes, hooks/defaults/licenses and matching MD5/SHA-256; the packaged amd64 helper also passes the real launch-gate/argv check
- Four package/source checks: source-inert hook definitions, empty defaults, first-install help path, and built package contents; all shell source files pass syntax checks

Frontend interaction coverage includes inert discovery/addition, explicit start, independent boot selection, failed-checkbox rollback, readonly picker/cancel, concurrent revision conflict, unverified-mutation lockout/recovery, removal confirmation, polling serialization, aborted unmounts, and preservation of untouched empty/multiline arguments.

## Environment-blocked checks

- **Full Unix-socket CLI integration:** this executor rejects `listen unix ...` with `operation not permitted`, including the approved elevated test. The complete CLI/socket test suite is included in `backend/integration_test.go`, and a separate independent CLI harness is in `tests/review_runtime.py`, but neither is claimed to pass here
- **Real browser visual QA:** the local preview server starts, but the cloud browser blocks its loopback URL with `ERR_BLOCKED_BY_CLIENT`. Component interaction tests pass; visual rendering in MOS remains unverified

No alternate transport or weakened access control was added to work around these restrictions. The supervisor keeps a root-private Unix socket for the intended Linux/MOS runtime. Socket-free engine tests execute the same process-control and settings code; they do not prove the missing IPC path.

## Still required on an authorized MOS test host

1. Publish the tested GitHub release and Hub entry, install through the actual MOS Hub card, then load the federated page through MOS's actual admin authentication
2. Select pool-backed executable files and test the included harmless loop, failure and clean-exit fixtures
3. Verify an explicit stop removes owned descendants while unrelated applications remain alive
4. Verify boot checkboxes after an actual reboot, including late pool availability; repeated boot-hook invocation must not override a manual stop in the same boot
5. Check settings persistence on the host's boot filesystem, including FAT if used, plus removal and a later real package upgrade
6. Verify UI layout/responsiveness, actual file permissions, real program readiness and any app-specific shutdown requirements

The arm64 package is cross-compiled. Native arm64 execution requires separate host testing. No NAS was contacted, no plugin was installed, no live service or network/security setting was changed, and nothing was publicly published.

## Hub adaptation verification

Prepared on 2026-10-04 from the supplied mos-hub and mos-distrobuilder archives. Target repositories were confirmed as mfleming1290/mos-binary-manager and mfleming1290/mos-hub. Nothing was published remotely by this local preparation.

- Re-ran the 25 frontend tests, production federation build, 16 focused backend tests under the race detector, Go formatting and vet
- Rebuilt amd64 and arm64 packages with matching MD5/SHA-256; independently checked static ELF architecture, root ownership, packaged local SVG and exact web assets; amd64 launch-gate check passed
- All 16 package/source/transport/release tests passed, including five new metadata/version/repository/architecture guards
- Parsed the GitHub workflow YAML and checked its triggers, read-only build permission, separate publish permission and successful-build dependency
- Prepared the additive Hub JSON/icon and Hub-first instructions, including the initial-install versus update settings distinction
- Applied the Hub patch successfully to a temporary copy of the supplied Hub; verified the two added files byte-for-byte
- Extracted the source ZIP into a separate temporary Git checkout and passed the tracked-source release checks; its archive was about 96 KiB, far below the 10 MiB MOS limit
- Independently reviewed the workflow/metadata/packages with no release blockers; corrected the federation dependency license heading to Mulan PSL v2 and visually checked the new SVG icon

The workflow requires the full backend Unix-socket integration suite on the GitHub runner before it will publish; this has not yet been run there. The previously reported cloud IPC and browser limitations, and all actual MOS-host verification requirements above, still apply.
