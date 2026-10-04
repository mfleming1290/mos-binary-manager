# Verification: Binary Manager 0.2.1

## Storage-check patch preparation (2026-10-04)

Prepared from the published 0.2.0 commit `a35d15213fc16f9756ca426ba42248b05908743e`. The storage regression was reproduced with a native Btrfs pool plus MOS-style persistent service-data bind mounts. This fixture identifies the failure class; it does not identify any particular live host's offending bind.

- Eight new backend regression tests cover safe sibling app-data, subvolume and bind roots, system-data overlap/aliases, root/boot exclusions, conservative non-Btrfs handling, unknown roots, other devices and missing-pool fallback
- 41 socket-free backend race tests passed; local root-only protected-file test skipped because the executor is non-root
- 52 frontend API/model/component tests, the production federation build, and 17 package/source/transport/release tests passed
- Both architecture packages were rebuilt and checked for version, static ELF architecture, web assets, icon, documentation, executable modes, root/root ownership and MD5/SHA-256
- Independent security review found no blocker; the changes preserve mount-ID verification, descriptor-anchored no-follow creation, private directory requirements and protected-file controls

Local Unix sockets are restricted, so the seven socket integration tests and the mandatory root-owned protected-file fixture must pass in full release CI. A successful release requires green CI for this exact source commit before tagging, followed by green tag CI and verification of all seven published assets. No tests or guards were weakened for this patch.

Real MOS upgrade, authentication, actual host mount behavior, reboot/late-pool behavior, visual rendering and native arm64 execution remain unverified. No NAS settings, files, permissions or services were changed. The historical sections below describe earlier preparations, not the final publication status of 0.2.1.

# Historical verification: Binary Manager 0.2.0

## 0.2.0 release preparation (2026-10-04)

Prepared in a cloud Linux workspace, without publication or NAS installation. The exact 0.1.1 remote baseline a99f80d7bf5ed424297faee08fb0a05ec9aa091b was independently reconstructed and all 55 source-file Git blob hashes matched before reviewing the feature diff.

### Passed locally

- 33 socket-free backend tests under the Go race detector, including concurrent duplicate instances using the same executable, independent HOME/dotfiles/environment, stopping/removing one without affecting another, defaults affecting only selected instances, stale config revisions, legacy/unknown-field preservation, private launch authorization and secret-marker nondisclosure
- Mount-selection and real no-follow file-descriptor directory-operation tests with injected test mount metadata; missing actual pool and nested-mount tests confirm no fallback directory creation. These simulations do not claim a real pool was mounted in this executor
- Protected-file literal parser, invalid-key/value/size checks, insecure ownership/modes/symlink/FIFO rejection, redacted errors and private-pipe secret delivery. Positive root-owned protected-file reading is not claimed locally
- Go formatting and vet, 51 frontend API/model/component tests, and production federation build
- 17 independent package/source/transport/release tests, plus workflow YAML and shell/Python syntax checks
- Both amd64 and arm64 Debian packages extracted and checked for static ELF architecture, root/root archive ownership, executable modes, exact frontend assets and settings-guide bytes, correct manifest/version, lifecycle hook contents, and matching MD5/SHA-256
- The packaged amd64 helper passed an independent authorization-pipe test: EOF/invalid permits execute nothing, literal argv/environment reach the fixture, and helper argv/errors do not contain the private marker
- Independent code review found and fixed an explicit HOME/XDG validation bypass where the effective value equaled the inherited value. Regression coverage now separates validation-input changes from affected-runtime comparison. Final review found no remaining code blockers

### Not verified here

- The seven full Unix-socket CLI integration tests encounter sandbox `operation not permitted`. Socket controls and auth were not weakened or replaced. The release CI still requires the full race/socket suite
- The accepted root-owned protected-file end-to-end test is skipped under this non-root executor. CI has a separate mandatory root-only step using temporary fixtures, with `BINARY_MANAGER_REQUIRE_ROOT_TEST=1` so it cannot silently skip; it covers secret-file launch and effective-env restart masking
- Mount simulations use a non-system workspace path; CI explicitly sets `BINARY_MANAGER_TEST_STORAGE` so these tests cannot fall back to a skipped `/tmp` path
- Cloud browser preview access was blocked, and a later preview startup hit a Node network-interface error. DOM interaction tests pass; actual visual rendering on MOS remains unverified
- Real MOS upgrade, plugin authentication, actual native/NFS/CIFS mounts, late/offline-pool boot behavior, reboot and native arm64 execution remain host-verification steps

Managed/custom HOME and explicit XDG paths deliberately reject mergerfs virtual pools, including `/mnt/user` when it is mergerfs. Select an underlying mounted persistent pool; the UI and error explain this limitation. Existing entries keep inherited behavior until runtime settings are explicitly enabled. No files, credentials or permissions on a NAS were changed.

The prepared 0.2.0 release must pass its full CI (including root-only and socket paths) after publication is authorized. Passing local package checks alone is not a claim of host readiness.

## Historical 0.1.1 patch

The frontend fix was independently reviewed and passed all 30 frontend API/model/component tests plus the production federation build before release preparation. Coverage includes the native UUID path, a secure UUID v4 fallback, missing secure randomness, and both add flows with `randomUUID` unavailable. Actual MOS-browser testing remains unverified.

The 0.1.1 baseline was published at a99f80d7bf5ed424297faee08fb0a05ec9aa091b with successful CI. This 0.2.0 preparation was compared against that exact remote tree by Git blob hashes; all 55 baseline source files were verified. The older local Git HEAD was not treated as the publication baseline.

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
