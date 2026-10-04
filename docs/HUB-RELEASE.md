# Publish Binary Manager through the MOS Hub

## Prepared destinations

- Plugin release repository: `https://github.com/mfleming1290/mos-binary-manager` (confirmed by the user)
- Existing catalog represented by the supplied archive: `https://github.com/mfleming1290/mos-hub`, branch `main`
- Catalog additions: `plugins/binary-manager.json` and `images/binary-managerIcon.svg`

These destinations were identified from the supplied Hub's maintainer metadata and other plugin entries, then confirmed by the user. No repository, release or remote Hub change has been made by this preparation. Repository URLs in this source are configuration, not evidence that those URLs already exist.

If the intended owner, repository or Hub branch differs, update `page/plugin.config.js`, `hub/plugins/binary-manager.json` and the repository expectation in `tests/release.test.mjs` before committing. Keep the repository basename `mos-binary-manager`: MOS derives its plugin directory from that basename.

## 1. Put the plugin source at the repository root

Extract the source ZIP. Commit the **contents** of its `mos-binary-manager/` folder, including the hidden `.github/` directory. Do not commit the ZIP as the repository's only file, or add an extra enclosing directory.

Required root layout:

```text
.github/workflows/build-plugin.yml
backend/
page/plugin.config.js
page/package.json
page/package-lock.json
scripts/
tests/
docs/
hub/
functions
settings.json
README.md
LICENSE
.gitignore
```

Do not commit `node_modules`, `page/dist`, `artifacts`, `checksums`, preview output or compiled executables. `.gitignore` excludes them. GitHub's source archive must stay below MOS's 10 MiB limit; the workflow checks the tracked archive size. The generated source ZIP may include checksums for convenience, but its ignored checksums directory is not part of the tagged Git source.

MOS downloads the tag's source separately to read `page/plugin.config.js`, `functions` and `settings.json`. Attaching only a Debian package does not satisfy this contract.

## 2. Build and release numeric tag 0.2.0

Commit the source, then create and push tag `0.2.0` on that exact reviewed commit. The workflow starts automatically for numeric tags. In an already configured local checkout:

```bash
git tag 0.2.0
git push origin 0.2.0
```

Pushing `main` or opening a pull request runs build/tests without publishing. GitHub Actions → **Build and Release** → **Run workflow** can retry a numeric tag that already exists; it deliberately does not invent a tag on a different/default-branch commit. For the first run, push the tag as above. The workflow must also exist on the default branch for the manual button to appear.

The committed version in `page/plugin.config.js`, `page/package.json`, and both root version entries of `page/package-lock.json` must all equal the tag. Use `0.2.0`, never `v0.2.0`. The workflow fails instead of changing these files only inside CI, because MOS reads the original tagged source. Update `docs/RELEASE-NOTES.md` for every release as well.

The workflow uses GitHub-hosted Ubuntu, Node 22, Go 1.27.1, `npm ci` with the committed lockfile, and no external publish token. Build jobs have read access; only the release job has `contents: write`. Repository/organization policy must permit that workflow permission. Do not create a personal token just for this workflow.

It requires all of these before publication:

- Version, repository, Hub metadata and tracked source checks
- Full backend `go test -race`, including the previously cloud-blocked Unix-socket tests
- Frontend tests, federation build, release-contract and transport checks
- Static native Go builds for both architectures, Debian contents/ELF/checksum verification and the harmless launch-gate check

It creates a draft with all assets and only then publishes it. It refuses an existing release rather than silently replacing its assets. If publishing stops with a draft, inspect its failed Actions step and assets before completing/removing that draft manually; do not force a tag or delete a published release to retry. Workflow preparation is locally validated, but its actual GitHub run remains unverified until run in that repository.

Expected release assets:

```text
binary-manager_0.2.0-1+mos-plugin_amd64.deb
binary-manager_0.2.0-1+mos-plugin_amd64.deb.md5
binary-manager_0.2.0-1+mos-plugin_amd64.deb.sha256
binary-manager_0.2.0-1+mos-plugin_arm64.deb
binary-manager_0.2.0-1+mos-plugin_arm64.deb.md5
binary-manager_0.2.0-1+mos-plugin_arm64.deb.sha256
mos-binary-manager-0.2.0-source.zip
```

GitHub also provides its automatic source ZIP/tarball. MOS uses that tagged source and the one `.deb` matching the NAS architecture. Two architectures in the same release are supported; multiple `.deb` files for one architecture are not. `all` is inappropriate because the supervisor is native code. MOS reads the first hash from the matching `.deb.md5`; SHA-256 is additionally supplied for manual verification.

## 3. Add the Hub entry after the release succeeds

Copy the two files from this source's `hub/` directory into the corresponding paths at the root of `mfleming1290/mos-hub`:

```text
hub/plugins/binary-manager.json  → plugins/binary-manager.json
hub/images/binary-managerIcon.svg → images/binary-managerIcon.svg
```

The supplied catalog has no generated global plugin index. Its `plugins/*.json` files are the list. `MANIFEST.txt` is a human-readable inventory; the prepared Hub patch appends the two new paths there but does not change other entries or `maintainer.json`.

The entry follows the supplied GoSHS/Kubernetes shapes: Utilities category, native amd64/arm64 support, GitHub repository/readme/homepage/support URLs, author and raw Hub image URL. It also declares `settings: true` explicitly so install/update metadata agree. This does not change the plugin's custom revision-checked settings transport; never use MOS's generic settings POST to edit its active configuration.

The installed plugin uses its packaged local SVG, so loading its icon does not depend on GitHub. The Hub card uses the raw SVG in the Hub repository. Neither icon is downloaded during a package build.

Review and commit the small additive patch. Do not replace newer unrelated Hub files with a full archive based on an older snapshot. After publication, confirm the JSON/icon are reachable and the GitHub release has all expected assets, then refresh the already-configured Hub in MOS.

## 4. Verify on a MOS test host

Install from the actual discovered Hub entry, wait for completion, reload MOS, and test the harmless fixtures before real applications. Check the expected native architecture, UI authentication, stop/descendant safety, persistence and an actual reboot. Test native arm64 separately.

For a future version, use MOS's **plugin update** flow, not a fresh Hub installation over an existing configuration. Update preserves settings in the examined MOS source; initial installation can overwrite defaults. MOS's update check compares the installed tag with the first returned release, not semantic version order, and can include prereleases. Publish stable numeric releases in order and review the selected version before updating.

The previously supplied offline helper remains first-install-only and has no Hub repository in its template. Migrating an already-installed local test build needs a separate settings-preserving plan; do not overwrite or delete its persistent directory.

## Reference basis

- Supplied `mos-hub.zip`: `maintainer.json`, `plugins/distrobuilder.json`, `plugins/goshs.json`, `plugins/kubernetes.json`
- Supplied `mos-distrobuilder.zip`: `.github/workflows/build-plugin.yml`, root layout, `page/plugin.config.js`
- MOS contract snapshot: mos-api `7281c01150b081b0e0e9e1d44048e0e85a0eacb2`, especially `plugins.service.js` install/update and architecture filtering
- Official action usage: [checkout](https://github.com/actions/checkout), [setup-node](https://github.com/actions/setup-node), [setup-go](https://github.com/actions/setup-go), [upload-artifact](https://github.com/actions/upload-artifact), [download-artifact](https://github.com/actions/download-artifact), [gh release create](https://cli.github.com/manual/gh_release_create)
