# Optional offline test installation

Use MOS Hub for the supported distribution flow described in the main README. This helper is retained only for an explicit first installation on a MOS test host. It is not an upgrade or a migration tool. It writes a local template with no release repository, so MOS cannot discover Hub updates for it.

1. Unzip the source bundle and copy the matching `.deb` and `.sha256` to the MOS test host
2. Check `dpkg --print-architecture` and choose `amd64` or `arm64`
3. Review `scripts/install-local.sh`, then run from the extracted source folder:

   ```bash
   sudo bash scripts/install-local.sh /absolute/path/binary-manager_0.1.0-1+mos-plugin_amd64.deb
   ```

   Substitute the arm64 filename when applicable. The script validates the MOS layout, package identity, version, architecture and checksum, and refuses an existing installation. A matching checksum can be beside the package or in the source bundle's `checksums/` directory
4. Reload MOS, open Plugins → Binary Manager, add a harmless foreground fixture, and explicitly enable it

The script creates MOS's numeric boot directory, stores the package and source hook, and initializes empty settings. Installing the `.deb` by itself does not do this. Programs run as root on standard MOS; only run trusted programs.

If installation stops after persistent files have been written, inspect the first error. Do not delete settings or force a reinstall. Do not use the Hub's initial-install route to migrate this local installation: an equal tag may be rejected, while a different tag may replace settings. Back up the persistent Binary Manager directory and plan that migration separately.
