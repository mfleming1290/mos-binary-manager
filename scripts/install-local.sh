#!/bin/bash
# Explicit, user-run, offline FIRST installation. Not executed during the build.
# Stores the selected .deb for MOS boot restoration; does not publish a Hub entry.
set -euo pipefail
if [[ ${1:-} == --help || $# -ne 1 ]]; then
  printf '%s\n' 'Usage: sudo bash scripts/install-local.sh /path/to/binary-manager_0.2.2-1+mos-plugin_<arch>.deb' 'First install only; requires an actual MOS host. Review README.md before running.'
  [[ ${1:-} == --help ]] && exit 0 || exit 2
fi
[[ $EUID -eq 0 ]] || { echo 'Run this installer as root on your MOS test host.' >&2; exit 1; }
[[ -f /usr/local/bin/mos-start && -d /boot/optional/plugins ]] || { echo 'MOS host layout not found. Refusing installation.' >&2; exit 1; }
deb=$(realpath -- "$1")
[[ -f $deb && ! -L $1 ]] || { echo 'Expected a regular Debian package file.' >&2; exit 1; }
[[ $(dpkg-deb -f "$deb" Package) == binary-manager-plugin ]] || { echo 'Wrong package.' >&2; exit 1; }
[[ $(dpkg-deb -f "$deb" Version) == 0.2.2-1+mos-plugin ]] || { echo 'This installer is for version 0.2.2 only.' >&2; exit 1; }
[[ $(dpkg-deb -f "$deb" Architecture) == "$(dpkg --print-architecture)" ]] || { echo 'Package architecture does not match this NAS.' >&2; exit 1; }
base=/boot/optional/plugins/binary-manager
[[ ! -e $base && ! -L $base ]] || { echo 'Binary Manager already has persistent files. Refusing to overwrite them; see upgrade notes.' >&2; exit 1; }
[[ ! -e /usr/bin/plugins/binary-manager ]] || { echo 'Binary Manager runtime already exists. Refusing to replace it.' >&2; exit 1; }
script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
source_dir=$(cd -- "$script_dir/.." && pwd)
for file in functions settings.json page/plugin.config.js; do
  [[ -f "$source_dir/$file" && ! -L "$source_dir/$file" ]] || { echo "Missing source bundle file: $file" >&2; exit 1; }
done
checksum="$deb.sha256"
if [[ ! -f $checksum ]]; then checksum="$source_dir/checksums/$(basename -- "$deb").sha256"; fi
[[ -f $checksum ]] || { echo 'Matching .sha256 file is missing (expected beside the .deb or in the source bundle checksums folder).' >&2; exit 1; }
expected=$(awk 'NR==1 {print $1}' "$checksum")
[[ $expected =~ ^[0-9a-f]{64}$ && $(sha256sum -- "$deb" | cut -d' ' -f1) == "$expected" ]] || { echo 'Package checksum does not match.' >&2; exit 1; }
echo 'Installing Binary Manager 0.2.2. It can run selected programs as root; nothing is enabled by default.'
umask 077
mkdir -- "$base" # Atomic first-install claim; does not overwrite a concurrent installer.
mkdir -- "$base/0.2.2"
cp -- "$deb" "$checksum" "$base/0.2.2/"
cp -- "$source_dir/functions" "$source_dir/page/plugin.config.js" "$base/0.2.2/"
cp -- "$source_dir/settings.json" "$base/settings.json"
cat > "$base/template.json" <<'JSON'
{"name":"binary-manager","displayName":"Binary Manager","tag":"0.2.2","version":"0.2.2","repository":"","localInstall":true,"driver":false,"settings":true}
JSON
dpkg -i -- "$deb"
/usr/bin/plugins/binary-manager ensure
echo 'Installed. Open MOS → Plugins → Binary Manager (reload MOS if needed). No applications have been enabled.'
echo 'This is a local test install. Hub updates are unavailable until a real release/catalog entry exists.'
