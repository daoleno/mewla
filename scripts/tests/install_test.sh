#!/usr/bin/env bash

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
INSTALLER="$ROOT/install.sh"
BASE_PATH=/usr/bin:/bin
TEST_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/mewla-installer-tests.XXXXXX")"
PASS=0

cleanup() {
  rm -rf "$TEST_ROOT"
}
trap cleanup EXIT

fail() {
  printf 'not ok - %s\n' "$1" >&2
  if [[ -f "${CASE_DIR:-}/output" ]]; then
    sed 's/^/  | /' "$CASE_DIR/output" >&2
  fi
  exit 1
}

pass() {
  PASS=$((PASS + 1))
  printf 'ok %d - %s\n' "$PASS" "$1"
}

assert_contains() {
  local file=$1 text=$2
  grep -Fq "$text" "$file" || fail "expected '$text' in $file"
}

assert_not_contains() {
  local file=$1 text=$2
  if grep -Fq "$text" "$file"; then
    fail "did not expect '$text' in $file"
  fi
}

assert_not_exists() {
  [[ ! -e "$1" ]] || fail "expected no file at $1"
}

new_case() {
  local name=$1
  CASE_DIR="$TEST_ROOT/$name"
  HOME_DIR="$CASE_DIR/home"
  ASSETS="$CASE_DIR/assets"
  FAKE_BIN="$CASE_DIR/fake-bin"
  mkdir -p "$HOME_DIR" "$ASSETS" "$FAKE_BIN" "$CASE_DIR/tmp"
  : > "$CASE_DIR/curl.log"
  cat > "$FAKE_BIN/uname" <<'EOF'
#!/bin/sh
case ${1:-} in
  -s) printf '%s\n' "${FAKE_UNAME_S:-Linux}" ;;
  -m) printf '%s\n' "${FAKE_UNAME_M:-x86_64}" ;;
  *) exit 2 ;;
esac
EOF
  cat > "$FAKE_BIN/curl" <<'EOF'
#!/bin/sh
set -eu
output=
url=
write_out=
while [ "$#" -gt 0 ]; do
  case $1 in
    --proto|--proto-redir|--header|--output|--write-out)
      option=$1
      value=$2
      shift 2
      [ "$option" = --output ] && output=$value
      [ "$option" = --write-out ] && write_out=$value
      ;;
    --fail|--silent|--show-error|--location|--head)
      shift
      ;;
    *)
      url=$1
      shift
      ;;
  esac
done
printf '%s\n' "$url" >> "$FAKE_CURL_LOG"
case $url in
  https://github.com/daoleno/mewla/releases/latest)
    [ "$write_out" = '%{url_effective}' ] || exit 91
    [ -n "${FAKE_LATEST_URL:-}" ] || exit 22
    printf '%s' "$FAKE_LATEST_URL"
    ;;
  https://github.com/daoleno/mewla/releases/download/*)
    /bin/cp "$FAKE_ASSETS/${url##*/}" "$output"
    ;;
  *)
    printf 'unexpected URL: %s\n' "$url" >&2
    exit 90
    ;;
esac
EOF
  chmod +x "$FAKE_BIN/uname" "$FAKE_BIN/curl"
}

make_archive() {
  local archive_name=${1:-mewla-linux-amd64.tar.gz}
  local package="$CASE_DIR/package"
  rm -rf "$package"
  mkdir -p "$package"
  cat > "$package/mewla" <<'EOF'
#!/bin/sh
case ${1:-} in
  --help) printf 'Mewla fixture help\n'; exit "${FAKE_HELP_STATUS:-0}" ;;
  doctor) printf 'fixture doctor\n'; exit "${FAKE_DOCTOR_STATUS:-0}" ;;
  deps)
    [ -z "${FAKE_NO_DEPS:-}" ] || exit 1
    [ "${2:-}" = --help ] && exit 0
    printf 'fixture deps %s\n' "$*"
    exit "${FAKE_DEPS_STATUS:-0}"
    ;;
  update) printf 'fixture update\n'; exit 0 ;;
  *) exit 2 ;;
esac
EOF
  chmod +x "$package/mewla"
  printf 'license\n' > "$package/LICENSE"
  printf 'notice\n' > "$package/NOTICE"
  printf 'trademarks\n' > "$package/TRADEMARKS.md"
  tar -C "$package" -czf "$ASSETS/$archive_name" LICENSE NOTICE TRADEMARKS.md mewla
  write_sums "$archive_name"
}

make_symlink_archive() {
  local archive_name=mewla-linux-amd64.tar.gz
  local package="$CASE_DIR/package"
  rm -rf "$package"
  mkdir -p "$package"
  printf 'license\n' > "$package/LICENSE"
  printf 'notice\n' > "$package/NOTICE"
  printf 'trademarks\n' > "$package/TRADEMARKS.md"
  ln -s /bin/sh "$package/mewla"
  tar -C "$package" -czf "$ASSETS/$archive_name" LICENSE NOTICE TRADEMARKS.md mewla
  write_sums "$archive_name"
}

make_traversal_archive() {
  local archive_name=mewla-linux-amd64.tar.gz
  local package="$CASE_DIR/package"
  rm -rf "$package"
  mkdir -p "$package"
  printf 'license\n' > "$package/LICENSE"
  printf 'notice\n' > "$package/NOTICE"
  printf 'trademarks\n' > "$package/TRADEMARKS.md"
  printf '#!/bin/sh\nexit 0\n' > "$package/mewla"
  tar -C "$package" --transform='s|^mewla$|../mewla|' -czf "$ASSETS/$archive_name" LICENSE NOTICE TRADEMARKS.md mewla
  write_sums "$archive_name"
}

write_sums() {
  local archive_name=$1 digest
  digest=$(sha256sum "$ASSETS/$archive_name" | awk '{print $1}')
  printf '%s  %s\n' "$digest" "$archive_name" > "$ASSETS/SHA256SUMS"
}

run_installer() {
  local run_path=${RUN_PATH_OVERRIDE:-$FAKE_BIN:$BASE_PATH}
  env \
    HOME="$HOME_DIR" \
    PATH="$run_path" \
    SHELL=/bin/zsh \
    TMPDIR="$CASE_DIR/tmp" \
    FAKE_CURL_LOG="$CASE_DIR/curl.log" \
    FAKE_ASSETS="$ASSETS" \
    "$@" "$INSTALLER" > "$CASE_DIR/output" 2>&1
}

expect_failure() {
  if run_installer "$@"; then
    fail "installer unexpectedly succeeded"
  fi
}

test_platform_mapping() {
  local spec os arch archive
  for spec in 'Linux x86_64 mewla-linux-amd64.tar.gz' 'Linux aarch64 mewla-linux-arm64.tar.gz' 'Darwin arm64 mewla-darwin-arm64.tar.gz'; do
    read -r os arch archive <<< "$spec"
    new_case "platform-$os-$arch"
    run_installer FAKE_UNAME_S="$os" FAKE_UNAME_M="$arch" MEWLA_VERSION=v1.2.3-beta.4 \
      MEWLA_INSTALL_DIR="$HOME_DIR/install" MEWLA_DRY_RUN=1
    assert_contains "$CASE_DIR/output" "$archive"
  done
  pass "maps every supported desktop platform to its exact archive"
}

test_unsupported_platforms() {
  new_case unsupported-macos
  expect_failure FAKE_UNAME_S=Darwin FAKE_UNAME_M=x86_64 MEWLA_VERSION=v1.2.3 MEWLA_DRY_RUN=1
  assert_contains "$CASE_DIR/output" "Intel macOS is not supported"
  new_case unsupported-windows
  expect_failure FAKE_UNAME_S=MINGW64_NT FAKE_UNAME_M=x86_64 MEWLA_VERSION=v1.2.3 MEWLA_DRY_RUN=1
  assert_contains "$CASE_DIR/output" "native Windows is not supported"
  assert_contains "$CASE_DIR/output" "WSL2"
  pass "fails clearly on Intel macOS and native Windows"
}

test_unconfigured_version_is_dynamic() {
  new_case latest-redirect
  make_archive
  run_installer MEWLA_INSTALL_DIR="$HOME_DIR/install" \
    FAKE_LATEST_URL=https://github.com/daoleno/mewla/releases/tag/v4.5.6
  [[ -x "$HOME_DIR/install/mewla" ]] || fail "latest release was not installed"
  assert_contains "$CASE_DIR/output" "Installed Mewla v4.5.6"
  assert_contains "$CASE_DIR/curl.log" "https://github.com/daoleno/mewla/releases/latest"
  assert_contains "$CASE_DIR/curl.log" "https://github.com/daoleno/mewla/releases/download/v4.5.6/mewla-linux-amd64.tar.gz"
  assert_not_contains "$CASE_DIR/curl.log" "api.github.com"
  pass "follows the releases/latest redirect without the GitHub API"
}

test_latest_redirect_failures() {
  new_case latest-unreachable
  make_archive
  expect_failure MEWLA_INSTALL_DIR="$HOME_DIR/install"
  assert_contains "$CASE_DIR/output" "could not reach https://github.com/daoleno/mewla/releases/latest"
  assert_not_exists "$HOME_DIR/install/mewla"

  new_case latest-not-a-tag
  make_archive
  expect_failure MEWLA_INSTALL_DIR="$HOME_DIR/install" FAKE_LATEST_URL=https://github.com/daoleno/mewla/releases
  assert_contains "$CASE_DIR/output" "did not lead to a release tag"
  assert_contains "$CASE_DIR/output" "MEWLA_VERSION"

  new_case latest-bad-tag
  make_archive
  expect_failure MEWLA_INSTALL_DIR="$HOME_DIR/install" \
    FAKE_LATEST_URL='https://github.com/daoleno/mewla/releases/tag/v01.2.3'
  assert_contains "$CASE_DIR/output" "invalid release version"
  assert_not_exists "$HOME_DIR/install/mewla"
  pass "fails clearly when the latest redirect is unreachable or names no valid tag"
}

test_fixed_version_and_atomic_replacement() {
  new_case fixed-version
  make_archive
  mkdir -p "$HOME_DIR/install"
  printf 'old binary\n' > "$HOME_DIR/install/mewla"
  run_installer MEWLA_VERSION=v3.4.5 MEWLA_INSTALL_DIR="$HOME_DIR/install"
  "$HOME_DIR/install/mewla" --help >/dev/null || fail "installed fixture does not run"
  [[ $(stat -c '%a' "$HOME_DIR/install/mewla") == 755 ]] || fail "installed mode is not 0755"
  [[ -z $(find "$HOME_DIR/install" -name '.mewla-install.*' -print -quit) ]] || fail "atomic temp file remained"
  assert_contains "$CASE_DIR/curl.log" "/download/v3.4.5/mewla-linux-amd64.tar.gz"
  assert_not_contains "$CASE_DIR/curl.log" "releases/latest"
  pass "pins exact release URLs and atomically replaces the target with mode 0755"
}

test_checksum_failures() {
  local mode
  for mode in mismatch missing duplicate malformed; do
    new_case "checksum-$mode"
    make_archive
    case $mode in
      mismatch) printf '%064d  mewla-linux-amd64.tar.gz\n' 0 > "$ASSETS/SHA256SUMS" ;;
      missing) printf '%064d  another.tar.gz\n' 0 > "$ASSETS/SHA256SUMS" ;;
      duplicate) cat "$ASSETS/SHA256SUMS" >> "$ASSETS/SHA256SUMS.copy"; cat "$ASSETS/SHA256SUMS" "$ASSETS/SHA256SUMS.copy" > "$ASSETS/sums.new"; mv "$ASSETS/sums.new" "$ASSETS/SHA256SUMS" ;;
      malformed) printf 'xyz  mewla-linux-amd64.tar.gz extra\n' > "$ASSETS/SHA256SUMS" ;;
    esac
    expect_failure MEWLA_VERSION=v1.2.3-beta.1 MEWLA_INSTALL_DIR="$HOME_DIR/install"
    assert_not_exists "$HOME_DIR/install/mewla"
  done
  pass "rejects mismatched, missing, duplicate, and malformed checksum entries"
}

test_unsafe_archives() {
  new_case unsafe-symlink
  make_symlink_archive
  expect_failure MEWLA_VERSION=v1.2.3-beta.1 MEWLA_INSTALL_DIR="$HOME_DIR/install"
  assert_contains "$CASE_DIR/output" "non-regular entry"
  assert_not_exists "$HOME_DIR/install/mewla"

  new_case unsafe-traversal
  make_traversal_archive
  expect_failure MEWLA_VERSION=v1.2.3-beta.1 MEWLA_INSTALL_DIR="$HOME_DIR/install"
  assert_contains "$CASE_DIR/output" "unsafe or unexpected archive layout"
  assert_not_exists "$CASE_DIR/mewla"
  assert_not_exists "$HOME_DIR/install/mewla"
  pass "rejects symlink and traversal archive layouts before extraction"
}

test_existing_update_preference() {
  new_case existing-update
  mkdir -p "$HOME_DIR/bin"
  cat > "$HOME_DIR/bin/mewla" <<EOF
#!/bin/sh
case \${1:-} in
  --help) exit 0 ;;
  update) printf 'updated\n' >> '$CASE_DIR/update.log'; exit 0 ;;
  *) exit 2 ;;
esac
EOF
  chmod +x "$HOME_DIR/bin/mewla"
  FAKE_BIN="$HOME_DIR/bin:$FAKE_BIN"
  run_installer
  [[ $(cat "$CASE_DIR/update.log") == updated ]] || fail "existing updater was not invoked"
  [[ ! -s "$CASE_DIR/curl.log" ]] || fail "bootstrap downloaded despite usable existing Mewla"
  assert_contains "$CASE_DIR/output" "signed release updater"
  pass "prefers a usable user-owned Mewla binary's signed updater"
}

test_install_dir_selection() {
  new_case one-user-bin
  make_archive
  mkdir -p "$HOME_DIR/bin"
  FAKE_BIN="$FAKE_BIN:$HOME_DIR/bin"
  run_installer MEWLA_VERSION=v1.2.3-beta.1
  [[ -x "$HOME_DIR/bin/mewla" ]] || fail "single writable user PATH bin was not selected"

  new_case ambiguous-user-bin
  make_archive
  mkdir -p "$HOME_DIR/bin" "$HOME_DIR/tools/bin"
  FAKE_BIN="$FAKE_BIN:$HOME_DIR/bin:$HOME_DIR/tools/bin"
  run_installer MEWLA_VERSION=v1.2.3-beta.1 MEWLA_NO_PATH_UPDATE=1
  [[ -x "$HOME_DIR/.local/bin/mewla" ]] || fail "ambiguous PATH did not fall back to ~/.local/bin"

  new_case protected-dir
  expect_failure MEWLA_VERSION=v1.2.3 MEWLA_INSTALL_DIR=/usr/local/bin MEWLA_DRY_RUN=1
  assert_contains "$CASE_DIR/output" "root/system directory"
  pass "selects an unambiguous user bin, defaults safely, and refuses system destinations"
}

test_profile_idempotency_and_no_mutation() {
  new_case profile-idempotent
  make_archive
  run_installer MEWLA_VERSION=v1.2.3-beta.1
  assert_contains "$CASE_DIR/output" "for new terminals"
  run_installer MEWLA_VERSION=v1.2.3-beta.1
  [[ $(grep -Fc '# >>> mewla installer PATH >>>' "$HOME_DIR/.zshrc") == 1 ]] || fail "profile marker was duplicated"
  [[ $(tail -n 1 "$CASE_DIR/output") == "  $HOME_DIR/.local/bin/mewla" ]] || fail "last line is not the runnable full path"

  new_case no-path-mutation
  make_archive
  run_installer MEWLA_VERSION=v1.2.3-beta.1 MEWLA_NO_PATH_UPDATE=1
  assert_not_exists "$HOME_DIR/.zshrc"
  [[ $(tail -n 1 "$CASE_DIR/output") == "  $HOME_DIR/.local/bin/mewla" ]] || fail "last line is not the runnable full path"

  new_case unsafe-fish-profile
  make_archive
  mkdir -p "$CASE_DIR/outside"
  ln -s "$CASE_DIR/outside" "$HOME_DIR/.config"
  run_installer MEWLA_VERSION=v1.2.3-beta.1 SHELL=/bin/fish
  assert_contains "$CASE_DIR/output" "parent resolves outside HOME"
  assert_not_exists "$CASE_DIR/outside/fish/config.fish"
  pass "updates profiles idempotently and honors the no-PATH-mutation setting"
}

test_missing_tools() {
  local tool tool_bin command_path command_name
  for tool in curl tar checksum; do
    new_case "missing-$tool"
    tool_bin="$CASE_DIR/tools"
    mkdir -p "$tool_bin"
    for command_name in uname dirname stat id grep awk sed mktemp tr mkdir rm cp chmod mv; do
      command_path=$(command -v "$command_name")
      ln -s "$command_path" "$tool_bin/$command_name"
    done
    if [[ $tool != curl ]]; then ln -s "$FAKE_BIN/curl" "$tool_bin/curl"; fi
    if [[ $tool != tar ]]; then ln -s "$(command -v tar)" "$tool_bin/tar"; fi
    if [[ $tool != checksum ]]; then ln -s "$(command -v sha256sum)" "$tool_bin/sha256sum"; fi
    FAKE_BIN=$tool_bin
    RUN_PATH_OVERRIDE=$tool_bin
    expect_failure MEWLA_VERSION=v1.2.3 MEWLA_INSTALL_DIR="$HOME_DIR/install"
    case $tool in
      checksum) assert_contains "$CASE_DIR/output" "SHA-256 verification requires" ;;
      *) assert_contains "$CASE_DIR/output" "required command '$tool'" ;;
    esac
    unset RUN_PATH_OVERRIDE
  done
  pass "reports missing curl, tar, and SHA-256 tools without network access"
}

test_tmux_check_and_next_step() {
  new_case deps-tmux
  make_archive
  mkdir -p "$HOME_DIR/install"
  FAKE_BIN="$FAKE_BIN:$HOME_DIR/install"
  run_installer MEWLA_VERSION=v1.2.3 MEWLA_INSTALL_DIR="$HOME_DIR/install" FAKE_DEPS_STATUS=1
  assert_contains "$CASE_DIR/output" "fixture deps deps -only tmux"
  [[ -x "$HOME_DIR/install/mewla" ]] || fail "a missing dependency removed the installed binary"
  [[ $(tail -n 1 "$CASE_DIR/output") == "  mewla" ]] || fail "last line is not mewla when the install dir is on PATH"

  new_case deps-unsupported
  make_archive
  local tool_bin="$CASE_DIR/tools" command_name
  mkdir -p "$tool_bin"
  for command_name in uname dirname stat id grep awk sed mktemp tr mkdir rm cp chmod mv tar gzip sha256sum env; do
    ln -s "$(command -v "$command_name")" "$tool_bin/$command_name"
  done
  ln -s "$FAKE_BIN/curl" "$tool_bin/curl"
  ln -s "$FAKE_BIN/uname" "$tool_bin/uname" 2>/dev/null || true
  RUN_PATH_OVERRIDE=$tool_bin
  run_installer MEWLA_VERSION=v1.2.3 MEWLA_INSTALL_DIR="$HOME_DIR/install" FAKE_NO_DEPS=1 || fail "installer failed without tmux on PATH"
  unset RUN_PATH_OVERRIDE
  assert_contains "$CASE_DIR/output" "tmux is missing. Mewla runs every agent inside tmux; see how to install it: $HOME_DIR/install/mewla doctor"
  assert_not_contains "$CASE_DIR/output" "fixture deps"
  [[ $(tail -n 1 "$CASE_DIR/output") == "  $HOME_DIR/install/mewla" ]] || fail "last line is not the runnable full path"

  new_case deps-unsupported-tmux-present
  make_archive
  printf '#!/bin/sh\nexit 0\n' > "$FAKE_BIN/tmux"
  chmod +x "$FAKE_BIN/tmux"
  run_installer MEWLA_VERSION=v1.2.3 MEWLA_INSTALL_DIR="$HOME_DIR/install" FAKE_NO_DEPS=1
  assert_not_contains "$CASE_DIR/output" "tmux is missing"
  pass "checks tmux through mewla deps, falls back for older releases, and ends with the start command"
}

test_executable_failure_keeps_previous_install() {
  new_case executable-failure
  make_archive
  mkdir -p "$HOME_DIR/install"
  printf 'previous healthy binary\n' > "$HOME_DIR/install/mewla"
  expect_failure MEWLA_VERSION=v1.2.3 MEWLA_INSTALL_DIR="$HOME_DIR/install" FAKE_HELP_STATUS=127
  assert_contains "$HOME_DIR/install/mewla" "previous healthy binary"
  assert_contains "$CASE_DIR/output" "previous installation is unchanged"
  assert_not_exists "$HOME_DIR/.zshrc"
  pass "executable failure retains the previous binary"
}

if [[ ${1:-} == --smoke ]]; then
  printf 'TAP version 13\n'
  test_fixed_version_and_atomic_replacement
  printf '1..%d\n' "$PASS"
  exit 0
fi

printf 'TAP version 13\n'
test_platform_mapping
test_unsupported_platforms
test_unconfigured_version_is_dynamic
test_latest_redirect_failures
test_fixed_version_and_atomic_replacement
test_checksum_failures
test_unsafe_archives
test_existing_update_preference
test_install_dir_selection
test_profile_idempotency_and_no_mutation
test_missing_tools
test_tmux_check_and_next_step
test_executable_failure_keeps_previous_install
printf '1..%d\n' "$PASS"
