#!/bin/sh
# Bootstrap the Mewla daemon from official GitHub release assets.

set -eu
set -f

REPOSITORY="daoleno/mewla"
GITHUB_WEB="https://github.com/$REPOSITORY"
PATH_MARKER="# >>> mewla installer PATH >>>"
DRY_RUN=${MEWLA_DRY_RUN:-0}
NO_PATH_UPDATE=${MEWLA_NO_PATH_UPDATE:-0}
VERSION=${MEWLA_VERSION:-}
INSTALL_DIR=${MEWLA_INSTALL_DIR:-}
WORK_DIR=
INSTALL_TEMP=

say() {
  printf '%s\n' "$*"
}

die() {
  printf 'mewla installer: error: %s\n' "$*" >&2
  exit 1
}

warn() {
  printf 'mewla installer: warning: %s\n' "$*" >&2
}

usage() {
  cat <<'EOF'
Usage: install.sh [options]

Options:
  --version vX.Y.Z[-beta.N]  install an exact release
  --install-dir DIR          install into this user-writable directory
  --no-path-update           do not edit a shell profile
  --dry-run                  print the planned action without changing files
  -h, --help                 show this help

The same settings may be supplied as MEWLA_VERSION, MEWLA_INSTALL_DIR,
MEWLA_NO_PATH_UPDATE=1, and MEWLA_DRY_RUN=1.

Without MEWLA_VERSION, the installer follows GitHub's releases/latest redirect to
the newest stable release (no GitHub API call, so API rate limits do not apply).
MEWLA_VERSION pins an exact release, including a beta; no version is embedded.
EOF
}

cleanup() {
  if [ -n "$INSTALL_TEMP" ]; then
    rm -f "$INSTALL_TEMP"
  fi
  if [ -n "$WORK_DIR" ]; then
    rm -rf "$WORK_DIR"
  fi
}

trap cleanup EXIT
trap 'exit 1' HUP INT TERM
umask 077

while [ "$#" -gt 0 ]; do
  case $1 in
    --version)
      [ "$#" -ge 2 ] || die "--version requires a value"
      VERSION=$2
      shift 2
      ;;
    --install-dir)
      [ "$#" -ge 2 ] || die "--install-dir requires a value"
      INSTALL_DIR=$2
      shift 2
      ;;
    --no-path-update)
      NO_PATH_UPDATE=1
      shift
      ;;
    --dry-run)
      DRY_RUN=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      die "unknown option: $1"
      ;;
  esac
done

case $DRY_RUN in 0|1) ;; *) die "MEWLA_DRY_RUN must be 0 or 1" ;; esac
case $NO_PATH_UPDATE in 0|1) ;; *) die "MEWLA_NO_PATH_UPDATE must be 0 or 1" ;; esac

valid_version() {
  printf '%s\n' "$1" | grep -Eq '^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-beta\.(0|[1-9][0-9]*))?$'
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || die "required command '$1' was not found on PATH"
}

stat_uid() {
  stat -c '%u' "$1" 2>/dev/null || stat -f '%u' "$1" 2>/dev/null
}

is_protected_dir() {
  case $1 in
    /|/bin|/bin/*|/sbin|/sbin/*|/usr|/usr/*|/etc|/etc/*|/opt|/opt/*|/var|/var/*|/boot|/boot/*|/dev|/dev/*|/proc|/proc/*|/sys|/sys/*|/run|/run/*|/root|/root/*|/System|/System/*|/Library|/Library/*|/Applications|/Applications/*|/private/etc|/private/etc/*|/private/var|/private/var/*)
      return 0
      ;;
  esac
  return 1
}

path_has_dir() {
  wanted=$1
  old_ifs=$IFS
  IFS=:
  for entry in ${PATH:-}; do
    if [ "$entry" = "$wanted" ]; then
      IFS=$old_ifs
      return 0
    fi
  done
  IFS=$old_ifs
  return 1
}

platform_archive() {
  os=$(uname -s 2>/dev/null || die "could not detect the operating system")
  arch=$(uname -m 2>/dev/null || die "could not detect the CPU architecture")
  case "$os/$arch" in
    Linux/x86_64|Linux/amd64)
      PLATFORM=linux-amd64
      ;;
    Linux/aarch64|Linux/arm64)
      PLATFORM=linux-arm64
      ;;
    Darwin/arm64|Darwin/aarch64)
      PLATFORM=darwin-arm64
      ;;
    Darwin/x86_64|Darwin/amd64)
      die "Intel macOS is not supported. See $GITHUB_WEB/blob/main/docs/install-daemon.md#supported-platforms"
      ;;
    MINGW*/*|MSYS*/*|CYGWIN*/*|Windows_NT/*)
      die "native Windows is not supported. Use WSL2, which installs the Linux build: $GITHUB_WEB/blob/main/docs/install-daemon.md#supported-platforms"
      ;;
    *)
      die "unsupported platform $os/$arch. Supported hosts: Linux amd64, Linux arm64, and Apple Silicon macOS. See $GITHUB_WEB/blob/main/docs/install-daemon.md#supported-platforms"
      ;;
  esac
  ARCHIVE="mewla-$PLATFORM.tar.gz"
}

# safe_existing_binary NAME: a user-owned regular executable NAME on PATH in a
# writable, non-system directory that answers --help.
safe_existing_binary() {
  candidate=$(command -v "$1" 2>/dev/null || true)
  case $candidate in
    /*) ;;
    *) return 1 ;;
  esac
  [ -f "$candidate" ] && [ -x "$candidate" ] && [ ! -L "$candidate" ] || return 1
  candidate_dir=$(dirname "$candidate")
  [ -d "$candidate_dir" ] && [ -w "$candidate_dir" ] || return 1
  candidate_dir=$(cd -P "$candidate_dir" 2>/dev/null && pwd) || return 1
  is_protected_dir "$candidate_dir" && return 1
  [ "$(stat_uid "$candidate" 2>/dev/null || printf unknown)" = "$(id -u)" ] || return 1
  "$candidate" --help >/dev/null 2>&1 || return 1
  EXISTING_BINARY=$candidate
  return 0
}

safe_existing_mewla() {
  safe_existing_binary mewla || return 1
  EXISTING_MEWLA=$EXISTING_BINARY
}

latest_release() {
  # releases/latest is a web redirect to /releases/tag/<newest stable tag>;
  # unlike the REST API it has no unauthenticated rate limit.
  latest_url=$GITHUB_WEB/releases/latest
  effective=$(curl --proto '=https' --proto-redir '=https' --fail --silent --show-error --location \
    --head --output /dev/null --write-out '%{url_effective}' "$latest_url") ||
    die "could not reach $latest_url to find the newest Mewla release"
  case $effective in
    "$GITHUB_WEB/releases/tag/"*) VERSION=${effective##*/} ;;
    *) die "$latest_url did not lead to a release tag (got $effective); set MEWLA_VERSION=vX.Y.Z to pin one" ;;
  esac
}

curl_https() {
  url=$1
  output=$2
  accept=${3:-application/octet-stream}
  case $url in
    https://github.com/daoleno/mewla/releases/download/*) ;;
    *) die "refusing non-official download URL: $url" ;;
  esac
  curl --proto '=https' --proto-redir '=https' --fail --silent --show-error --location \
    --header "Accept: $accept" --output "$output" "$url" ||
    die "could not download $url; check the network and run the same command again"
}

checksum_archive() {
  sums=$1
  archive_path=$2
  archive_name=$3
  expected_file=$WORK_DIR/expected-sha256
  if ! awk -v name="$archive_name" '
    $2 == name {
      count++
      if (NF != 2 || length($1) != 64 || $1 !~ /^[0-9a-f]+$/) bad = 1
      digest = $1
    }
    END {
      if (count != 1 || bad) exit 1
      print digest
    }
  ' "$sums" > "$expected_file"; then
    die "SHA256SUMS must contain exactly one well-formed lowercase SHA-256 entry for $archive_name"
  fi
  expected=$(sed -n '1p' "$expected_file")
  if command -v sha256sum >/dev/null 2>&1; then
    actual=$(sha256sum "$archive_path" | awk '{print $1}')
  elif command -v shasum >/dev/null 2>&1; then
    actual=$(shasum -a 256 "$archive_path" | awk '{print $1}')
  else
    die "SHA-256 verification requires sha256sum (Linux) or shasum (macOS)"
  fi
  [ "$actual" = "$expected" ] || die "SHA-256 mismatch for $archive_name"
}

validate_and_extract() {
  archive_path=$1
  names=$WORK_DIR/archive-names
  verbose=$WORK_DIR/archive-verbose
  extract_dir=$WORK_DIR/extract
  mkdir "$extract_dir"
  tar -tzf "$archive_path" > "$names" || die "could not list $ARCHIVE"
  if ! awk '
    $0 == "LICENSE" { license++ ; next }
    $0 == "NOTICE" { notice++ ; next }
    $0 == "TRADEMARKS.md" { trademarks++ ; next }
    $0 == "mewla" { mewla++ ; next }
    { bad = 1 }
    END {
      if (bad || NR != 4 || license != 1 || notice != 1 || trademarks != 1 || mewla != 1) exit 1
    }
  ' "$names"; then
    die "$ARCHIVE has an unsafe or unexpected archive layout"
  fi
  tar -tvzf "$archive_path" > "$verbose" || die "could not inspect $ARCHIVE entry types"
  if ! awk '
    length($1) == 0 || substr($1, 1, 1) != "-" { bad = 1 }
    END { if (bad || NR != 4) exit 1 }
  ' "$verbose"; then
    die "$ARCHIVE contains a link, device, directory, or other non-regular entry"
  fi
  tar -xzf "$archive_path" -C "$extract_dir" mewla || die "could not extract mewla from $ARCHIVE"
  [ -f "$extract_dir/mewla" ] && [ ! -L "$extract_dir/mewla" ] || die "extracted mewla is not a regular file"
  EXTRACTED_MEWLA=$extract_dir/mewla
}

prepare_install_dir() {
  requested=$1
  case $requested in
    /*) ;;
    *) die "install directory must be an absolute path: $requested" ;;
  esac
  is_protected_dir "$requested" && die "refusing to install in root/system directory: $requested"
  mkdir -p "$requested" || die "could not create install directory: $requested"
  [ -d "$requested" ] && [ ! -L "$requested" ] && [ -w "$requested" ] || die "install directory is not a writable regular directory: $requested"
  resolved=$(cd -P "$requested" 2>/dev/null && pwd) || die "could not resolve install directory: $requested"
  is_protected_dir "$resolved" && die "refusing install directory that resolves into a root/system directory: $resolved"
  [ "$(stat_uid "$resolved" 2>/dev/null || printf unknown)" = "$(id -u)" ] || die "install directory is not owned by the current user: $resolved"
  INSTALL_DIR=$resolved
}

select_install_dir() {
  if [ -n "$INSTALL_DIR" ]; then
    SELECTED_DIR=$INSTALL_DIR
    return
  fi
  if safe_existing_mewla; then
    SELECTED_DIR=$(dirname "$EXISTING_MEWLA")
    return
  fi

  eligible=
  eligible_count=0
  old_ifs=$IFS
  IFS=:
  for entry in ${PATH:-}; do
    IFS=$old_ifs
    case $entry in
      "$HOME"/*/bin|"$HOME"/bin|"$HOME"/.local/bin)
        if [ -d "$entry" ] && [ ! -L "$entry" ] && [ -w "$entry" ] &&
           [ "$(stat_uid "$entry" 2>/dev/null || printf unknown)" = "$(id -u)" ]; then
          case " $eligible " in
            *" $entry "*) ;;
            *) eligible="$eligible $entry"; eligible_count=$((eligible_count + 1)) ;;
          esac
        fi
        ;;
    esac
    IFS=:
  done
  IFS=$old_ifs
  if [ "$eligible_count" -eq 1 ]; then
    SELECTED_DIR=${eligible# }
  else
    SELECTED_DIR=$HOME/.local/bin
  fi
}

validate_install_dir_request() {
  requested_dir=$1
  case $1 in
    /*) ;;
    *) die "install directory must be an absolute path: $1" ;;
  esac
  is_protected_dir "$1" && die "refusing to install in root/system directory: $1"
  ancestor=$requested_dir
  while [ ! -d "$ancestor" ]; do
    parent=$(dirname "$ancestor")
    [ "$parent" != "$ancestor" ] || die "could not find an existing parent for install directory: $requested_dir"
    ancestor=$parent
  done
  resolved_ancestor=$(cd -P "$ancestor" 2>/dev/null && pwd) || die "could not resolve install directory parent: $ancestor"
  is_protected_dir "$resolved_ancestor" && die "refusing install directory whose parent resolves into a root/system directory: $resolved_ancestor"
  return 0
}

append_profile_path() {
  [ "$INSTALL_DIR" = "$HOME/.local/bin" ] || return 0
  path_has_dir "$INSTALL_DIR" && return 0
  [ "$NO_PATH_UPDATE" -eq 0 ] || return 0

  case ${SHELL:-} in
    */zsh)
      profile=$HOME/.zshrc
      path_line='export PATH="$HOME/.local/bin:$PATH"'
      ;;
    */bash)
      profile=$HOME/.bashrc
      path_line='export PATH="$HOME/.local/bin:$PATH"'
      ;;
    */fish)
      profile=$HOME/.config/fish/config.fish
      path_line='fish_add_path -g "$HOME/.local/bin"'
      ;;
    *)
      return 0
      ;;
  esac

  profile_dir=$(dirname "$profile")
  profile_ancestor=$profile_dir
  while [ ! -d "$profile_ancestor" ]; do
    parent=$(dirname "$profile_ancestor")
    if [ "$parent" = "$profile_ancestor" ]; then
      warn "not editing shell profile; no existing parent was found for $profile"
      return 0
    fi
    profile_ancestor=$parent
  done
  if ! resolved_profile_ancestor=$(cd -P "$profile_ancestor" 2>/dev/null && pwd); then
    warn "not editing shell profile; could not resolve $profile_ancestor"
    return 0
  fi
  case $resolved_profile_ancestor in
    "$HOME_RESOLVED"|"$HOME_RESOLVED"/*) ;;
    *)
      warn "not editing shell profile because its parent resolves outside HOME: $resolved_profile_ancestor"
      return 0
      ;;
  esac
  if ! mkdir -p "$profile_dir"; then
    warn "not editing shell profile; could not create $profile_dir"
    return 0
  fi
  if ! resolved_profile_dir=$(cd -P "$profile_dir" 2>/dev/null && pwd); then
    warn "not editing shell profile; could not resolve $profile_dir"
    return 0
  fi
  case $resolved_profile_dir in
    "$HOME_RESOLVED"|"$HOME_RESOLVED"/*) ;;
    *)
      warn "not editing shell profile because its directory resolves outside HOME: $resolved_profile_dir"
      return 0
      ;;
  esac
  if [ "$(stat_uid "$resolved_profile_dir" 2>/dev/null || printf unknown)" != "$(id -u)" ]; then
    warn "not editing shell profile because its directory is not user-owned: $resolved_profile_dir"
    return 0
  fi
  if [ -L "$profile" ]; then
    warn "not editing symlinked shell profile: $profile"
    return 0
  fi
  if [ -e "$profile" ]; then
    if [ ! -f "$profile" ] || [ ! -w "$profile" ]; then
      warn "not editing shell profile because it is not a writable regular file: $profile"
      return 0
    fi
    if [ "$(stat_uid "$profile" 2>/dev/null || printf unknown)" != "$(id -u)" ]; then
      warn "not editing shell profile because it is not user-owned: $profile"
      return 0
    fi
  fi
  if [ -f "$profile" ] && grep -Fq "$PATH_MARKER" "$profile"; then
    return 0
  fi
  if ! {
    printf '\n%s\n' "$PATH_MARKER"
    printf '%s\n' "$path_line"
    printf '%s\n' '# <<< mewla installer PATH <<<'
  } >> "$profile"; then
    warn "could not append PATH entry to shell profile: $profile"
    return 0
  fi
  PROFILE_UPDATED=$profile
}

# check_tmux lets the new binary find this computer's official tmux command
# and, in a terminal, offer to run it. Releases before `mewla deps` only say
# that tmux is missing and point to `mewla doctor`.
check_tmux() {
  binary=$1
  if ! "$binary" deps --help >/dev/null 2>&1; then
    if ! command -v tmux >/dev/null 2>&1; then
      say "tmux is missing. Mewla runs every agent inside tmux; see how to install it: $binary doctor"
    fi
    return 0
  fi
  if [ -t 1 ] && (: </dev/tty) 2>/dev/null; then
    "$binary" deps -only tmux </dev/tty || true
  else
    "$binary" deps -only tmux </dev/null || true
  fi
}

platform_archive

current_uid=$(id -u)
[ "$current_uid" != 0 ] || die "do not run this installer as root; install Mewla as the user who will run it"
[ -n "${HOME:-}" ] && [ -d "$HOME" ] || die "HOME must name the current user's home directory"
[ "$(stat_uid "$HOME" 2>/dev/null || printf unknown)" = "$current_uid" ] || die "HOME is not owned by the current user"
HOME_RESOLVED=$(cd -P "$HOME" 2>/dev/null && pwd) || die "could not resolve HOME"
is_protected_dir "$HOME_RESOLVED" && die "HOME resolves into a root/system directory: $HOME_RESOLVED"

safe_existing_mewla || true

if [ -z "$VERSION" ] && [ -z "$INSTALL_DIR" ] && [ -n "${EXISTING_MEWLA:-}" ]; then
  if [ "$DRY_RUN" -eq 1 ]; then
    say "Would use the signed built-in updater: $EXISTING_MEWLA update"
    exit 0
  fi
  say "Using the existing user-owned Mewla installation at $EXISTING_MEWLA."
  say "Delegating to its signed release updater..."
  "$EXISTING_MEWLA" update
  exit 0
fi

select_install_dir
validate_install_dir_request "$SELECTED_DIR"

if [ -n "$VERSION" ]; then
  valid_version "$VERSION" || die "invalid version '$VERSION'; expected vX.Y.Z or vX.Y.Z-beta.N with no leading zeroes"
fi

if [ "$DRY_RUN" -eq 1 ]; then
  requested_version=${VERSION:-latest-stable-release}
  say "Would install $requested_version ($ARCHIVE) from $GITHUB_WEB/releases into $SELECTED_DIR/mewla"
  say "No files were changed."
  exit 0
fi

require_command curl
require_command tar
require_command mktemp
require_command awk
require_command sed
require_command grep

if ! command -v sha256sum >/dev/null 2>&1 && ! command -v shasum >/dev/null 2>&1; then
  die "SHA-256 verification requires sha256sum (Linux) or shasum (macOS)"
fi

WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/mewla-install.XXXXXX") || die "could not create a private temporary directory"
[ -d "$WORK_DIR" ] || die "temporary directory was not created"

if [ -z "$VERSION" ]; then
  latest_release
fi
valid_version "$VERSION" || die "invalid release version from GitHub: $VERSION"

release_base="$GITHUB_WEB/releases/download/$VERSION"
archive_path=$WORK_DIR/$ARCHIVE
sums_path=$WORK_DIR/SHA256SUMS
say "Downloading Mewla $VERSION for $PLATFORM from $REPOSITORY..."
curl_https "$release_base/$ARCHIVE" "$archive_path"
curl_https "$release_base/SHA256SUMS" "$sums_path" "text/plain"
checksum_archive "$sums_path" "$archive_path" "$ARCHIVE"
validate_and_extract "$archive_path"

prepare_install_dir "$SELECTED_DIR"
target=$INSTALL_DIR/mewla
INSTALL_TEMP=$(mktemp "$INSTALL_DIR/.mewla-install.XXXXXX") || die "could not create an installation file beside $target"
cp "$EXTRACTED_MEWLA" "$INSTALL_TEMP" || die "could not stage the Mewla executable"
chmod 0755 "$INSTALL_TEMP" || die "could not set executable permissions"
if ! "$INSTALL_TEMP" --help >"$WORK_DIR/help-check" 2>&1; then
  sed -n '1,4p' "$WORK_DIR/help-check" >&2
  die "the staged executable failed its safe --help check; the previous installation is unchanged"
fi
mv -f "$INSTALL_TEMP" "$target" || die "could not atomically install $target"
INSTALL_TEMP=

PROFILE_UPDATED=
append_profile_path

say "Installed Mewla $VERSION at $target"
if [ -n "$PROFILE_UPDATED" ]; then
  say "Added $HOME/.local/bin to $PROFILE_UPDATED for new terminals (marker: $PATH_MARKER)."
fi
check_tmux "$target"

say ""
say "Next, start Mewla:"
if path_has_dir "$INSTALL_DIR"; then
  say "  mewla"
else
  say "  $target"
fi
