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
while [ "$#" -gt 0 ]; do
  case $1 in
    --proto|--proto-redir|--header|--output)
      option=$1
      value=$2
      shift 2
      [ "$option" = --output ] && output=$value
      ;;
    --fail|--silent|--show-error|--location)
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
  https://api.github.com/repos/daoleno/mewla/releases\?per_page=100)
    /bin/cp "$FAKE_RELEASES_JSON" "$output"
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
  RELEASES_JSON="$CASE_DIR/releases.json"
  printf '[]\n' > "$RELEASES_JSON"
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
    FAKE_RELEASES_JSON="$RELEASES_JSON" \
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
  new_case dynamic-version-first-run
  make_archive
  cat > "$RELEASES_JSON" <<'EOF'
[
 {"tag_name":"not-a-version","draft":false,"prerelease":true},
 {"tag_name":"v9.0.0","draft":true,"prerelease":false},
 {"tag_name":"v8.0.0-beta.1","draft":true,"prerelease":true},
 {"tag_name":"v3.4.5","draft":false,"prerelease":false},
 {"tag_name":"v3.4.5-beta.10","draft":false,"prerelease":true},
 {"tag_name":"v4.0.0-beta.1","draft":false,"prerelease":true}
]
EOF
  run_installer MEWLA_INSTALL_DIR="$HOME_DIR/install"
  [[ -x "$HOME_DIR/install/mewla" ]] || fail "latest release was not installed"
  assert_contains "$CASE_DIR/output" "Installed Mewla v4.0.0-beta.1"
  assert_contains "$CASE_DIR/curl.log" "https://api.github.com/repos/daoleno/mewla/releases?per_page=100"
  assert_contains "$CASE_DIR/curl.log" "https://github.com/daoleno/mewla/releases/download/v4.0.0-beta.1/mewla-linux-amd64.tar.gz"
  assert_not_contains "$CASE_DIR/curl.log" "/download/v9.0.0/"

  new_case dynamic-version-later-run
  make_archive
  cat > "$RELEASES_JSON" <<'EOF'
[
 {"tag_name":"v4.0.0-beta.1","draft":false,"prerelease":true},
 {"tag_name":"v5.0.0","draft":false,"prerelease":false}
]
EOF
  run_installer MEWLA_INSTALL_DIR="$HOME_DIR/install"
  assert_contains "$CASE_DIR/output" "Installed Mewla v5.0.0"
  assert_contains "$CASE_DIR/curl.log" "https://api.github.com/repos/daoleno/mewla/releases?per_page=100"
  assert_contains "$CASE_DIR/curl.log" "https://github.com/daoleno/mewla/releases/download/v5.0.0/mewla-linux-amd64.tar.gz"
  assert_not_contains "$CASE_DIR/curl.log" "/download/v4.0.0-beta.1/"
  pass "ignores drafts and invalid tags, then dynamically selects the SemVer-highest public release on every unpinned fresh run"
}

test_stable_release_precedence() {
  new_case stable-release-precedence
  make_archive
  cat > "$RELEASES_JSON" <<'EOF'
[
 {"tag_name":"v3.4.5-beta.10","draft":false,"prerelease":true},
 {"tag_name":"v3.4.5","draft":false,"prerelease":false}
]
EOF
  run_installer MEWLA_INSTALL_DIR="$HOME_DIR/install"
  assert_contains "$CASE_DIR/output" "Installed Mewla v3.4.5"
  assert_contains "$CASE_DIR/curl.log" "https://github.com/daoleno/mewla/releases/download/v3.4.5/mewla-linux-amd64.tar.gz"
  assert_contains "$CASE_DIR/curl.log" "https://github.com/daoleno/mewla/releases/download/v3.4.5/SHA256SUMS"
  pass "includes stable public releases and ranks stable above beta at the same core"
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
  assert_not_contains "$CASE_DIR/curl.log" "https://api.github.com/repos/daoleno/mewla/releases?per_page=100"
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
  run_installer MEWLA_VERSION=v1.2.3-beta.1
  [[ $(grep -Fc '# >>> mewla installer PATH >>>' "$HOME_DIR/.zshrc") == 1 ]] || fail "profile marker was duplicated"
  assert_contains "$CASE_DIR/output" "This installer cannot change your current shell"
  assert_contains "$CASE_DIR/output" "export PATH=\"$HOME_DIR/.local/bin:\$PATH\""

  new_case no-path-mutation
  make_archive
  run_installer MEWLA_VERSION=v1.2.3-beta.1 MEWLA_NO_PATH_UPDATE=1
  assert_not_exists "$HOME_DIR/.zshrc"
  assert_contains "$CASE_DIR/output" "This installer cannot change your current shell"

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

test_doctor_failure_is_not_corruption() {
  new_case doctor-nonfatal
  make_archive
  run_installer MEWLA_VERSION=v1.2.3 MEWLA_INSTALL_DIR="$HOME_DIR/install" FAKE_DOCTOR_STATUS=1
  assert_contains "$CASE_DIR/output" "Mewla is installed correctly"
  [[ -x "$HOME_DIR/install/mewla" ]] || fail "doctor failure removed the installed binary"
  pass "treats doctor dependency failures as host guidance, not installer corruption"
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
test_stable_release_precedence
test_fixed_version_and_atomic_replacement
test_checksum_failures
test_unsafe_archives
test_existing_update_preference
test_install_dir_selection
test_profile_idempotency_and_no_mutation
test_missing_tools
test_doctor_failure_is_not_corruption
test_executable_failure_keeps_previous_install
printf '1..%d\n' "$PASS"
