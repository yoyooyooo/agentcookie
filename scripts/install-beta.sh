#!/usr/bin/env bash
#
# install-beta.sh - Verified release installer for agentcookie.
#
# Run this script with `--as source` (on the workstation that owns the source
# browser) or `--as sink` (on the agent host). It verifies the release archive,
# places the binary, and starts the wizard interactively.
#
# Usage:
#   ./install-beta.sh --as source
#   ./install-beta.sh --as sink
#
# Optional flags:
#   --peer <hostname>          Tailscale hostname of the OTHER machine.
#                              If omitted, the script prompts interactively.
#   --code <code>              [sink] Pairing code printed by the source's
#                              wizard install. Forwarded to wizard install.
#   --pair-url <url>           [sink] Source's pairing URL (e.g.
#                              http://<source>:9998/pair). Forwarded to wizard install.
#   --skip-keychain-prompt     [sink] Forwarded to wizard install. Auto-set
#                              when no TTY is attached (e.g. SSH non-pty).
#   --extra-binary <path>      Repeatable. PP CLI binaries to grant
#                              Chrome Safe Storage access. Sink-side only.
#   --bin-dir <dir>            Where to place the agentcookie binary.
#                              Default: /usr/local/bin if writable,
#                              else $HOME/bin.
#   --tarball <path>           Use a local tarball instead of fetching a release.
#   --version <fork-tag>       Download this exact release tag. Recommended.
#   --checksum-file <path>     checksums.txt for a local tarball.
#
# Environment:
#   AGENTCOOKIE_REPO             Release repository. Defaults to this maintained
#                               fork (`yoyooyooo/agentcookie`).
#   AGENTCOOKIE_VERSION          Exact release tag; overridden by --version.
#   AGENTCOOKIE_EXPECTED_TEAM_ID Optional Developer ID TeamIdentifier. When
#                               unset, checksum verification remains mandatory
#                               and ad-hoc Darwin signatures are reported.
#
# Design notes:
#   - Bash, not Go, so operators can audit the install path without reading the
#     compiled program.
#   - No sudo. If a step needs elevated privileges, we print the command
#     and ask the user to run it themselves.
#   - Idempotent. Re-running on a healthy install reports state and
#     exits 0 without re-running the wizard.
#   - Fails loud. Every step that can fail prints a remediation pointer.

set -euo pipefail

ROLE=""
PEER=""
CODE=""
PAIR_URL=""
SKIP_KEYCHAIN_PROMPT=""
EXTRA_WIZARD_ARGS=()
EXTRA_BINS=()
BIN_DIR=""
TARBALL=""
VERSION="${AGENTCOOKIE_VERSION:-}"
CHECKSUM_FILE=""

REPO="${AGENTCOOKIE_REPO:-yoyooyooo/agentcookie}"

# ---- helpers ----

die() {
  echo "install-beta.sh: $*" >&2
  echo "install-beta.sh: see README.md for help" >&2
  exit 1
}

ok() { echo "install-beta.sh: [ok]   $*"; }
warn() { echo "install-beta.sh: [warn] $*" >&2; }
step() { echo "install-beta.sh: [step] $*"; }

prompt() {
  local var="$1" question="$2"
  local val
  read -rp "    $question: " val
  printf -v "$var" '%s' "$val"
}

# ---- argument parsing ----

while [[ $# -gt 0 ]]; do
  case "$1" in
    --as)
      ROLE="$2"; shift 2 ;;
    --peer)
      PEER="$2"; shift 2 ;;
    --code)
      CODE="$2"; shift 2 ;;
    --pair-url)
      PAIR_URL="$2"; shift 2 ;;
    --skip-keychain-prompt)
      SKIP_KEYCHAIN_PROMPT="1"; shift ;;
    --skip-chrome-sqlite)
      EXTRA_WIZARD_ARGS+=("--skip-chrome-sqlite"); shift ;;
    --write-chrome-sqlite)
      EXTRA_WIZARD_ARGS+=("--write-chrome-sqlite"); shift ;;
    --no-cdp)
      EXTRA_WIZARD_ARGS+=("--no-cdp"); shift ;;
    --extra-binary)
      EXTRA_BINS+=("$2"); shift 2 ;;
    --bin-dir)
      BIN_DIR="$2"; shift 2 ;;
    --tarball)
      TARBALL="$2"; shift 2 ;;
    --version)
      VERSION="$2"; shift 2 ;;
    --checksum-file)
      CHECKSUM_FILE="$2"; shift 2 ;;
    -h|--help)
      sed -n '1,35p' "$0" >&2
      exit 0 ;;
    *)
      die "unknown argument: $1" ;;
  esac
done

if [[ -z "$ROLE" ]]; then
  echo "install-beta.sh: which role is this Mac?"
  echo "  source  = the Mac you browse Chrome on"
  echo "  sink    = the Mac your AI agents run on"
  prompt ROLE "role (source/sink)"
fi
case "$ROLE" in
  source|sink) ;;
  *) die "invalid role: $ROLE (expected 'source' or 'sink')" ;;
esac

# ---- prereqs ----

step "checking prereqs"

if ! command -v tailscale >/dev/null 2>&1 && ! command -v /Applications/Tailscale.app/Contents/MacOS/Tailscale >/dev/null 2>&1; then
  die "Tailscale not found. Install from https://tailscale.com/download/mac first."
fi
TS_CLI="$(command -v tailscale 2>/dev/null || true)"
TS_CLI="${TS_CLI:-/Applications/Tailscale.app/Contents/MacOS/Tailscale}"

if ! "$TS_CLI" status >/dev/null 2>&1; then
  die "Tailscale daemon not running. Run 'tailscale up' (or open the Tailscale app) and try again."
fi
ok "Tailscale is up"

if ! ls /Applications/Google\ Chrome.app >/dev/null 2>&1 && \
   ! ls "$HOME/Applications/Google Chrome.app" >/dev/null 2>&1 && \
   ! ls /Applications/Dia.app >/dev/null 2>&1 && \
   ! ls "$HOME/Applications/Dia.app" >/dev/null 2>&1; then
  warn "No supported Chromium-family source browser was found in Applications."
fi

# ---- locate tarball / fetch release ----

if [[ -n "$VERSION" && ! "$VERSION" =~ ^fork-v[0-9]+\.[0-9]+\.[0-9]+-r[0-9]+$ ]]; then
  die "invalid --version value: $VERSION"
fi

if [[ -z "$TARBALL" ]]; then
  if ! command -v gh >/dev/null 2>&1; then
    die "GitHub CLI (gh) not found, and no --tarball provided. Install gh or pass --tarball and --checksum-file."
  fi
  if ! gh auth status >/dev/null 2>&1; then
    die "gh is not authenticated. Run 'gh auth login' first."
  fi
  if [[ -z "$VERSION" ]]; then
    warn "no exact --version supplied; resolving the repository's latest non-prerelease release"
  fi
  step "downloading ${VERSION:-latest release} from $REPO"
  TMP_DL="$(mktemp -d -t agentcookie-release.XXXXXX)"
  DOWNLOAD_ARGS=(release download)
  if [[ -n "$VERSION" ]]; then
    DOWNLOAD_ARGS+=("$VERSION")
  fi
  DOWNLOAD_ARGS+=(
    --repo "$REPO"
    --pattern '*darwin_arm64.tar.gz'
    --pattern 'checksums.txt'
    --dir "$TMP_DL"
    --clobber
  )
  gh "${DOWNLOAD_ARGS[@]}"
  TARBALL="$(find "$TMP_DL" -maxdepth 1 -type f -name '*darwin_arm64.tar.gz' -print -quit)"
  CHECKSUM_FILE="$TMP_DL/checksums.txt"
  if [[ -z "$TARBALL" || ! -f "$TARBALL" ]]; then
    die "release tarball not found after download (looked in $TMP_DL)"
  fi
  ok "downloaded $(basename "$TARBALL")"
fi

if [[ -z "$CHECKSUM_FILE" ]]; then
  sibling_checksum="$(dirname "$TARBALL")/checksums.txt"
  if [[ -f "$sibling_checksum" ]]; then
    CHECKSUM_FILE="$sibling_checksum"
  fi
fi
[[ -f "$CHECKSUM_FILE" ]] || die "checksums.txt is required; pass --checksum-file for a local tarball"

step "verifying release archive checksum"
archive_name="$(basename "$TARBALL")"
expected_sha="$(awk -v name="$archive_name" '$2 == name {print $1}' "$CHECKSUM_FILE")"
[[ "$expected_sha" =~ ^[0-9a-fA-F]{64}$ ]] || die "no unique SHA-256 entry for $archive_name in $CHECKSUM_FILE"
actual_sha="$(shasum -a 256 "$TARBALL" | awk '{print $1}')"
[[ "$actual_sha" == "$expected_sha" ]] || die "SHA-256 mismatch for $archive_name"
ok "archive SHA-256 matches checksums.txt"

# ---- extract and verify binary ----

WORK="$(mktemp -d -t agentcookie-install.XXXXXX)"
tar -xzf "$TARBALL" -C "$WORK"
# The release tarball wraps everything in a versioned directory
# (agentcookie-${VERSION}-darwin-arm64/), so the binary is one level
# deep. find tolerates both shapes (wrapped + flat).
NEW_BIN="$(find "$WORK" -name agentcookie -type f -perm -u+x 2>/dev/null | head -n1)"
if [[ -z "$NEW_BIN" || ! -x "$NEW_BIN" ]]; then
  die "agentcookie binary not found inside tarball ($TARBALL)"
fi

archive_version="${archive_name#agentcookie_}"
archive_version="${archive_version%_darwin_arm64.tar.gz}"
binary_version="$("$NEW_BIN" version)"
[[ "$archive_version" == "$binary_version" ]] || die "archive version $archive_version does not match binary version $binary_version"
if [[ -n "$VERSION" && "$binary_version" != "$VERSION" ]]; then
  die "downloaded binary version $binary_version does not match requested version $VERSION"
fi
ok "binary version matches the release archive: $binary_version"

step "verifying code signature"
if ! codesign --verify --strict --verbose=2 "$NEW_BIN" >/dev/null 2>&1; then
  die "codesign verification failed for the release binary"
fi

signature_details="$(codesign -d --verbose=4 "$NEW_BIN" 2>&1 || true)"
team_id="$(awk -F= '/^TeamIdentifier=/{print $2}' <<<"$signature_details")"
expected_team_id="${AGENTCOOKIE_EXPECTED_TEAM_ID:-}"
if [[ -n "$expected_team_id" ]]; then
  [[ "$team_id" == "$expected_team_id" ]] || die "Developer ID TeamIdentifier does not match AGENTCOOKIE_EXPECTED_TEAM_ID"
  ok "Developer ID TeamIdentifier matches the configured fork authority"
elif [[ -n "$team_id" && "$team_id" != "not set" ]]; then
  ok "valid Developer ID signature (TeamIdentifier $team_id)"
else
  warn "binary is ad-hoc signed and not Apple-notarized; archive SHA-256 was verified"
fi

xattr -c "$NEW_BIN" 2>/dev/null || true

# ---- place binary ----

if [[ -z "$BIN_DIR" ]]; then
  if [[ -w /usr/local/bin ]]; then
    BIN_DIR="/usr/local/bin"
  else
    BIN_DIR="$HOME/bin"
  fi
fi
mkdir -p "$BIN_DIR"
TARGET="$BIN_DIR/agentcookie"

step "installing to $TARGET"
cp "$NEW_BIN" "$TARGET"
chmod +x "$TARGET"
ok "installed"

if [[ ":$PATH:" != *":$BIN_DIR:"* ]]; then
  warn "$BIN_DIR is not on your \$PATH. The LaunchAgent uses absolute paths"
  warn "and will work fine, but \`agentcookie\` from a shell will not. To fix,"
  warn "add this line to your shell profile (~/.zshrc on macOS default):"
  warn "    export PATH=\"$BIN_DIR:\$PATH\""
  warn "Then run \`exec \$SHELL -l\` to reload."
fi

# ---- run wizard ----

step "running agentcookie wizard install --as $ROLE"

if [[ -z "$PEER" ]]; then
  echo "    What is the Tailscale hostname of the OTHER machine?"
  echo "    Run 'tailscale status' to list your tailnet hosts."
  prompt PEER "peer hostname"
fi

# Sink-only: collect the pair code and pair URL from the source's
# wizard install output. Both are required (the wizard refuses to
# start without them) so prompt if not passed.
if [[ "$ROLE" == "sink" ]]; then
  if [[ -z "$CODE" ]]; then
    echo "    Paste the pairing code printed by the source's wizard install"
    echo "    (looks like 'XXXX-YYYY-ZZZZ'):"
    prompt CODE "pair code"
  fi
  if [[ -z "$PAIR_URL" ]]; then
    echo "    Paste the pair URL printed by the source's wizard install"
    echo "    (looks like 'http://<source-host>:9998/pair'):"
    prompt PAIR_URL "pair URL"
  fi
fi

WIZARD_ARGS=(wizard install --as "$ROLE" --peer "$PEER")
if [[ "$ROLE" == "sink" ]]; then
  WIZARD_ARGS+=(--code "$CODE" --pair-url "$PAIR_URL")
fi
for b in "${EXTRA_BINS[@]:-}"; do
  [[ -z "$b" ]] && continue
  WIZARD_ARGS+=(--extra-binary "$b")
done
# v0.12.0-beta.3: forward --skip-chrome-sqlite, --write-chrome-sqlite,
# and --no-cdp explicitly if the operator passed them. The wizard
# itself auto-detects headless context when none are passed.
if [[ ${#EXTRA_WIZARD_ARGS[@]} -gt 0 ]]; then
  WIZARD_ARGS+=("${EXTRA_WIZARD_ARGS[@]}")
fi

# v0.12.0-beta.3: when there's no controlling TTY on a sink install,
# the wizard now auto-detects headless context and writes
# skip_chrome_sqlite + cdp.enabled into sink.yaml. No GUI Keychain
# prompt fires (the wizard skips the prompt step too, mirroring the
# v0.12.0-beta.2 behavior). The "Screen Share to click Always Allow"
# step is no longer required for the install to complete.
#
# Operators on a GUI session see the legacy default and can opt into
# headless mode explicitly with --skip-chrome-sqlite.
if [[ -z "$SKIP_KEYCHAIN_PROMPT" ]] && [[ "$ROLE" == "sink" ]] && ! [[ -t 0 ]]; then
  warn "no TTY detected; defaulting headless sink install."
  warn "  - sink.yaml will set skip_chrome_sqlite: true and cdp.enabled: true"
  warn "  - sink daemon will NOT read Chrome Safe Storage"
  warn "  - CDP injection will push cookies into ~/.agentcookie/chrome-profile each sync"
  warn "  - --skip-keychain-prompt is added to the wizard call so it does not block on GUI prompts"
  SKIP_KEYCHAIN_PROMPT="1"
fi
if [[ -n "$SKIP_KEYCHAIN_PROMPT" ]]; then
  WIZARD_ARGS+=(--skip-keychain-prompt)
fi

"$TARGET" "${WIZARD_ARGS[@]}"

# ---- final doctor check ----

step "running agentcookie doctor to confirm install state"

DOCTOR_EXIT=0
"$TARGET" doctor || DOCTOR_EXIT=$?

# ---- next steps hint (sink role only) ----
#
# A common operator pitfall after install is invoking a PP CLI before one has
# been installed. agentcookie ships independently from those consumers, so make
# the next step explicit.
if [[ "$ROLE" == "sink" ]]; then
  echo
  echo "==============================================================="
  echo "  Next step: install at least one PP CLI on this sink."
  echo "==============================================================="
  echo
  echo "  agentcookie syncs cookies; the PP CLIs are what consume them."
  echo "  Two of the five built-in-adapter PP CLIs are go-installable today:"
  echo
  echo "    GOPRIVATE='github.com/mvanhorn/*' go install github.com/mvanhorn/instacart-pp-cli@latest"
  echo "    GOPRIVATE='github.com/mvanhorn/*' go install github.com/mvanhorn/airbnb-vrbo-pp-cli@latest"
  echo
  echo "  The remaining three (ebay, pagliacci-pizza, table-reservation-goat)"
  echo "  ship via the printing-press meta tool; see"
  echo "    https://github.com/mvanhorn/printing-press-library"
  echo
  echo "  After installing instacart-pp-cli, verify over SSH from your laptop:"
  echo
  echo "    ssh $(hostname -s) 'instacart-pp-cli carts'"
  echo
  echo "  Cookie delivery paths to PP CLIs (no env vars needed for the five"
  echo "  with built-in adapters; sidecar env var is the fallback):"
  echo "    - adapter session files (v0.11) -- auto-populated by the sink"
  echo "    - sidecar -- export AGENTCOOKIE_PLAIN_COOKIES=~/.agentcookie/cookies-plain.db"
  echo "==============================================================="
  echo
fi

if [[ $DOCTOR_EXIT -eq 0 ]]; then
  ok "install complete; doctor reports all-green"
  ok "next: install one or more PP CLIs (above) and verify over SSH"
else
  warn "doctor reports issues; see output above and follow the [Remediation] lines"
  exit 1
fi
