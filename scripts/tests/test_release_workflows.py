"""Release and CI workflow contracts: what each workflow must keep doing, read from the YAML text.

Ported from the Expo app's bun tests (releaseArtifactsWorkflow, iosReleaseWorkflow and ciWorkflow),
keeping the assertions that still hold for the NSH client; release-next-beta.yml runs this file again on
the prepared tree.
"""

from pathlib import Path
import re
import unittest

ROOT = Path(__file__).resolve().parents[2]


def read(path):
    return (ROOT / path).read_text(encoding="utf-8")


CI = read(".github/workflows/ci.yml")
CLIENT = read(".github/workflows/client.yml")
RELEASE = read(".github/workflows/release-artifacts.yml")
IOS_RELEASE = read(".github/workflows/ios-release.yml")
NATIVE_CACHE = read(".github/workflows/native-cache.yml")
NATIVE_LIBS = read(".github/workflows/native-libs.yml")
POSTPROCESS = read(".github/workflows/ios-testflight-postprocess.yml")
AVAILABILITY = read(".github/workflows/ios-testflight-available.yml")
REVIEW = read(".github/workflows/ios-testflight-review.yml")
PREPARATION = read(".github/workflows/release-next-beta.yml")
ANDROID_NATIVE = read(".github/actions/android-native/action.yml")
IOS_NATIVE = read(".github/actions/ios-native/action.yml")
IDENTITY_VERIFIER = read("scripts/verify-release-identity.sh")
NATIVE_VERIFIER = read("scripts/verify-libghostty.sh")
APK_SCRIPT = read("scripts/android-release-apk.sh")
STAGE_SCRIPT = read("scripts/stage-release.sh")
PROJECT_YML = read("client/ios/project.yml")
ROOT_PACKAGE = read("package.json")

TAG_PUSH = re.compile(r'push:\s*\n\s*tags:\s*\n\s*- "v\*\.\*\.\*"\s*\n\s*- "v\*\.\*\.\*-beta\.\*"')
RELEASE_ONLY = "if: github.event_name == 'workflow_dispatch' || github.ref_type == 'tag'"


def between(text, start, end=None):
    """The text from `start` up to `end` (or the end)."""
    begin = text.index(start)
    return text[begin:text.index(end, begin)] if end else text[begin:]


def cache_blocks(*sources):
    pattern = re.compile(r"uses: actions/cache(?:/restore)?@v4[\s\S]*?(?=\n\s*- name:|$)")
    return "\n".join(match.group(0) for source in sources for match in pattern.finditer(source))


class ReleaseArtifactsWorkflowTests(unittest.TestCase):
    def test_prepares_native_release_inputs_before_compilation(self):
        self.assertIn("./scripts/build-daemon-linux.sh --out-dir", CI)
        self.assertRegex(ANDROID_NATIVE, r"uses: android-actions/setup-android@v3\s+with:\s+packages: platform-tools")
        for source in (RELEASE, NATIVE_CACHE, CLIENT):
            self.assertIn("uses: ./.github/actions/android-native", source)

    def test_builds_the_signed_apk_from_the_nsh_client(self):
        android = between(RELEASE, "  android:\n", "  desktop-stage:")
        self.assertIn("run: client/scripts/fetch-nsh.sh", android)
        self.assertLess(android.index("run: client/scripts/fetch-nsh.sh"), android.index("./scripts/android-release-apk.sh"))
        self.assertNotIn("bun ", android)
        self.assertNotIn("working-directory: app", android)
        self.assertIn(":app:assembleProductionRelease", APK_SCRIPT)
        self.assertIn(":app:bundleProductionRelease", APK_SCRIPT)
        self.assertIn('-Pmewla.channel="$CHANNEL" -Pmewla.abis=arm64-v8a', APK_SCRIPT)
        self.assertNotIn("expo prebuild", APK_SCRIPT)
        # One version source: client/ui/nsh.json through scripts/lib/release_identity.py.
        for source in (RELEASE, STAGE_SCRIPT, APK_SCRIPT):
            self.assertIn("release_identity.py", source)
            self.assertNotIn("app.base.json", source)
        self.assertNotIn("working-directory: app", PREPARATION)
        self.assertNotIn("bun test", PREPARATION)

    def test_immutable_tag_pushes_are_the_only_automatic_publication_path(self):
        self.assertRegex(RELEASE, TAG_PUSH)
        self.assertNotRegex(RELEASE, r"release:\s*\n\s*types:")
        for needle in (
            "type: boolean",
            "needs.validate.outputs.publish == 'true'",
            "./scripts/verify-release-identity.sh --tag",
            "RELEASE_IS_PRERELEASE=true",
            "RELEASE_IS_PRERELEASE=false",
            '--prerelease="$RELEASE_IS_PRERELEASE"',
            '--latest="$RELEASE_IS_STABLE"',
            'gh release create "$TAG" --verify-tag --draft',
            'gh release upload "$TAG" "${assets[@]}" --clobber',
            'gh release edit "$TAG" --draft=false',
        ):
            self.assertIn(needle, RELEASE)
        for needle in (
            "release tag $RELEASE_TAG does not match tracked version",
            "checked-out release tag does not resolve to HEAD",
            "release tag commit is not on origin/main",
        ):
            self.assertIn(needle, IDENTITY_VERIFIER)

    def test_publishes_desktop_archives_first_and_attaches_the_apk_when_ready(self):
        self.assertIn("daemon:", RELEASE)
        self.assertIn("android:", RELEASE)
        self.assertRegex(RELEASE, r"desktop-stage:\s*\n\s*name: [^\n]+\n\s*needs: \[validate, daemon, daemon-linux-arm64, daemon-windows, daemon-darwin\]\n")
        self.assertRegex(RELEASE, r"desktop-publish:\s*\n\s*name: [^\n]+\n\s*needs: \[validate, desktop-stage\]\n")
        for needle in (
            "needs: [validate, daemon, android, desktop-stage]",
            "needs: [validate, desktop-publish, complete-stage]",
            "./scripts/stage-release.sh --skip-build\n",
            # The complete set must reuse the already-public archive bytes.
            'diff -u <(daemon_sums inputs/desktop/SHA256SUMS) <(daemon_sums "$STAGE/SHA256SUMS")',
            "sha256sum -c --ignore-missing SHA256SUMS",
            "grep -Eq 'ELF 64-bit.*(x86-64|x86_64).*statically linked'",
            "grep -Eq 'Mach-O 64-bit.*arm64'",
            '--out-dir "$GITHUB_WORKSPACE/dist-download/staging/bin"',
            './scripts/stage-release.sh --skip-build --apk "$APK"',
            "SOURCE_DATE_EPOCH",
        ):
            self.assertIn(needle, RELEASE)

    def test_builds_every_daemon_with_the_terminal_engine_on_its_own_platform(self):
        build = between(RELEASE, "  daemon-build:\n", "  daemon:\n")
        for target, lib in (("linux-amd64", "linux_amd64_musl"), ("linux-arm64", "linux_arm64_musl"), ("windows-amd64", "windows_amd64")):
            self.assertIn(f"- {{ target: {target}, lib: {lib}, ", build)
        self.assertIn('--targets "${{ matrix.target }}" --web-ui prebuilt', build)
        for needle in (
            "runs-on: windows-latest",
            "./scripts/tests/release_smoke_windows.ps1",
            "grep -Eq 'PE32\\+ executable.*x86-64'",
            "--targets darwin-arm64",
            "runs-on: macos-26",
            "runs-on: ubuntu-24.04-arm",
            ".*statically linked",
            "pattern: mewla-daemon-binaries-*-${{ needs.validate.outputs.tag }}",
        ):
            self.assertIn(needle, RELEASE)
        self.assertEqual(RELEASE.count("mewla-windows-amd64.zip"), 3)
        self.assertEqual(len(re.findall(r"go test -count=1 -v \./releasesmoke/", RELEASE)), 3)

    def test_the_daemons_inputs_are_built_fresh_in_this_run_at_the_checked_tag(self):
        engine = between(RELEASE, "  engine:\n", "  web-ui:\n")
        for lib, os_ in (("linux_amd64_musl", "ubuntu-latest"), ("linux_arm64_musl", "ubuntu-latest"),
                         ("windows_amd64", "ubuntu-latest"), ("darwin_arm64", "macos-26")):
            self.assertIn(f"- {{ lib: {lib}, os: {os_} }}", engine)
        self.assertIn('./scripts/build-libghostty-desktop.sh --out "$RUNNER_TEMP/ghostty-desktop" --targets "${{ matrix.lib }}"', engine)
        self.assertIn("run: ./scripts/build-web-ui.sh", between(RELEASE, "  web-ui:\n", "  daemon-build:\n"))
        for job, end in (("engine", "web-ui"), ("web-ui", "daemon-build"), ("daemon-build", "daemon"), ("daemon-darwin", "android")):
            block = between(RELEASE, f"  {job}:\n", f"  {end}:\n")
            self.assertIn("ref: ${{ needs.validate.outputs.checkout }}", block, job)
            self.assertIn("if: needs.validate.outputs.mode == 'release'\n        run: |\n          git fetch --no-tags origin main\n"
                          '          ./scripts/verify-release-identity.sh --tag "${{ needs.validate.outputs.tag }}"', block, job)
        # Intermediates never match the published pattern; the merged names stay what they were.
        self.assertNotRegex(RELEASE, r"name: mewla-daemon-binaries-[^\n]*matrix")
        self.assertIn("name: mewla-daemon-binaries-linux-${{ needs.validate.outputs.tag }}\n          pattern: daemon-part-bin-*-", RELEASE)
        self.assertIn("name: mewla-daemon-binaries-darwin-${{ needs.validate.outputs.tag }}", RELEASE)
        self.assertIn('== "./mewla-linux-amd64 ./mewla-linux-arm64 ./mewla-windows-amd64.exe " ]]', RELEASE)

    def test_candidates_are_built_without_signing_staging_or_publishing(self):
        self.assertIn("candidate_version:", RELEASE)
        self.assertIn("only a tag can be published", RELEASE)
        for job in ("desktop-stage", "complete-stage"):
            block = between(RELEASE, f"  {job}:")
            self.assertIn("if: needs.validate.outputs.mode == 'release'", block[:block.index("steps:")])
        self.assertEqual(len(re.findall(r"ZEN_UPDATE_SIGNING_KEY_BASE64 \}\}", RELEASE)), 2)

    def test_recovery_is_reviewed_and_caches_hold_no_signing_material(self):
        self.assertIn("workflow_dispatch:", RELEASE)
        self.assertIn("publish:", RELEASE)
        self.assertIn("--build-cache", APK_SCRIPT)
        self.assertIn("-Dorg.gradle.jvmargs=-Xmx6g", RELEASE)
        for needle in (
            "~/.gradle/caches",
            "android-native-inputs-",
            "android-ghostty-output-v2-arm64-",
            "native/terminal-vt/libs/android",
            "native/terminal-vt/include/ghostty",
            "scripts/lib/ghostty_release.py",
            "scripts/lib/minisign_verify.py",
            "scripts/verify-android-native-symbols.py",
            "steps.ghostty-output-cache.outputs.cache-hit != 'true'",
            "run: ./scripts/verify-libghostty.sh --release",
            "./scripts/build-libghostty.sh --abis arm64-v8a",
            # ccache: pinned binary, content-checked compiler, saved only from main.
            "7766991b91b3a5a177ab33fa043fe09e72c68586d5a86d20a563a05b74f119c0",
            "CCACHE_COMPILERCHECK=content",
            "android-ccache-v1-",
        ):
            self.assertIn(needle, ANDROID_NATIVE)
        self.assertIn('bad "missing pinned header $HEADERS_DIR/vt.h"', NATIVE_VERIFIER)
        # Tag runs only restore; the main-branch warmer is the one cache writer.
        self.assertNotIn("save-caches", RELEASE)
        self.assertNotRegex(RELEASE, r"uses: actions/cache(/save)?@")
        self.assertIn('save-caches: "true"', NATIVE_CACHE)
        self.assertNotIn("secrets.", NATIVE_CACHE)
        blocks = cache_blocks(RELEASE, ANDROID_NATIVE, NATIVE_CACHE)
        self.assertIn("~/.gradle/caches", blocks)
        for forbidden in ("keystore", ".p12", ".jks", ".apk", "dist-download"):
            self.assertNotIn(forbidden, blocks.lower())


class IOSReleaseWorkflowTests(unittest.TestCase):
    def test_only_preview_testflight_archives_are_the_testflight_channel(self):
        job_env = between(IOS_RELEASE, "    env:\n      MEWLA_IOS_APP_VARIANT")
        self.assertIn(
            "MEWLA_IOS_DISTRIBUTION: ${{ ((github.event_name == 'push' || inputs.app_identity == 'preview') && "
            "(github.event_name == 'push' || inputs.destination == 'testflight')) && 'testflight' || 'none' }}",
            job_env[:job_env.index("    steps:")],
        )
        # A build setting, not an environment variable: only the command line overrides the project's value.
        self.assertIn('MEWLA_IOS_DISTRIBUTION="$MEWLA_IOS_DISTRIBUTION" \\\n            archive', IOS_RELEASE)
        self.assertIn("Print :MewlaDistribution", IOS_RELEASE)
        self.assertIn("MEWLA_IOS_DISTRIBUTION=testflight requires the Preview iOS identity", IOS_RELEASE)

    def test_asc_key_signing_key_and_write_token_live_in_separate_jobs(self):
        discover, sign, publish = re.split(r"\n  (?=discover:|sign:|publish:)", AVAILABILITY)[1:4]
        self.assertIn("environment: app-store-connect-preview", discover)
        self.assertIn("secrets.ZEN_ASC_API_KEY_BASE64", discover)
        self.assertNotIn("ZEN_UPDATE_SIGNING_KEY_BASE64", discover)
        self.assertNotIn("contents: write", discover)
        self.assertIn("secrets.ZEN_UPDATE_SIGNING_KEY_BASE64", sign)
        self.assertNotIn("ZEN_ASC", sign)
        self.assertNotIn("contents: write", sign)
        self.assertIn("contents: write", publish)
        self.assertNotIn("secrets.ZEN_", publish)

    def test_testflight_availability_is_a_rolling_non_latest_prerelease(self):
        for needle in (
            "MEWLA_TESTFLIGHT_TAG: ios-testflight",
            "--prerelease --latest=false",
            "./scripts/sign-release-manifest.sh out/testflight-manifest.json",
            '"distribution": "testflight"',
            "vars.MEWLA_ASC_PREVIEW_APP_ID",
            '--beta-group-name "Mewla Preview"',
        ):
            self.assertIn(needle, AVAILABILITY)
        self.assertNotRegex(AVAILABILITY, r"\b67\d{8}\b")

    def test_testflight_review_reads_with_the_preview_key_and_writes_nothing_to_the_repo(self):
        for needle in (
            "workflow_dispatch:",
            "cron:",
            "environment: app-store-connect-preview",
            "vars.MEWLA_ASC_PREVIEW_APP_ID",
            "secrets.ZEN_ASC_API_KEY_BASE64",
            '--beta-group-name "Mewla Preview"',
            "--enable-public-link",
            '--summary "$GITHUB_STEP_SUMMARY"',
            'rm -f "${{ runner.temp }}/mewla-asc-individual-key.p8"',
        ):
            self.assertIn(needle, REVIEW)
        self.assertIn("permissions:\n  contents: read", REVIEW)
        self.assertNotIn("contents: write", REVIEW)
        self.assertNotIn("ZEN_UPDATE_SIGNING_KEY_BASE64", REVIEW)
        self.assertNotRegex(REVIEW, r"\b67\d{8}\b")

    def test_postprocess_resumes_against_the_configured_preview_app(self):
        self.assertIn("MEWLA_ASC_APP_ID: ${{ vars.MEWLA_ASC_PREVIEW_APP_ID }}", POSTPROCESS)
        self.assertIn('--app-id "$MEWLA_ASC_APP_ID"', POSTPROCESS)
        self.assertNotIn("6790486708", POSTPROCESS)

    def test_reviewed_tag_pushes_publish_as_preview(self):
        self.assertRegex(IOS_RELEASE, TAG_PUSH)
        self.assertNotRegex(IOS_RELEASE, r"release:\s*\n\s*types:")
        for needle in (
            "github.event_name == 'push' && 'preview'",
            "github.event_name == 'push' && 'testflight'",
            "ref: ${{ env.MEWLA_IOS_REF }}",
            "ref: ${{ github.workflow_sha }}",
            './scripts/verify-release-identity.sh --tag "$MEWLA_RELEASE_TAG"',
            "release tag version",
            # The build number follows from the version; a dispatch may override it.
            'DERIVED_BUILD_NUMBER="$(python3 scripts/lib/release_identity.py ios_build_number)"',
            'MEWLA_IOS_BUILD_NUMBER="$DERIVED_BUILD_NUMBER"',
            "build_number must be a positive integer",
        ):
            self.assertIn(needle, IOS_RELEASE)
        self.assertNotIn("ios-build.json", IOS_RELEASE)
        self.assertNotIn("github.run_number", IOS_RELEASE)

    def test_identities_are_production_and_preview_defaulting_to_production(self):
        self.assertIn("app_identity:", IOS_RELEASE)
        self.assertRegex(IOS_RELEASE, r"app_identity:[\s\S]*?default: production[\s\S]*?options:\s*\n\s*- production\s*\n\s*- preview")

    def test_preview_signing_material_is_in_its_own_protected_environment(self):
        for needle in (
            "github.event_name == 'push' || inputs.app_identity == 'preview'",
            "'app-store-connect-preview'",
            "'app-store-connect'",
            "MEWLA_IOS_APP_VARIANT: ${{ github.event_name == 'push' && 'preview' || inputs.app_identity }}",
        ):
            self.assertIn(needle, IOS_RELEASE)

    def test_signing_project_artifact_and_verification_follow_the_closed_identity(self):
        for needle in (
            # The NSH client's iOS host, generated by xcodegen, with the Expo app's identities.
            "run: client/scripts/fetch-nsh.sh --ios",
            "client/scripts/ios-project.sh",
            "-project client/ios/Mewla.xcodeproj",
            "python3 scripts/lib/release_identity.py ios_bundle_id",
            "python3 scripts/lib/release_identity.py ios_preview_bundle_id",
            'MEWLA_APP_ID="$MEWLA_IOS_BUNDLE_ID"',
            'MARKETING_VERSION="$MEWLA_IOS_VERSION"',
            'CURRENT_PROJECT_VERSION="$MEWLA_IOS_BUILD_NUMBER"',
            'expected_bundle = os.environ["MEWLA_IOS_BUNDLE_ID"]',
            'os.environ["MEWLA_IOS_BUNDLE_ID"]: sys.argv[3]',
            "${MEWLA_IOS_ARTIFACT_NAME}-v${MEWLA_IOS_VERSION}",
            "MEWLA_IOS_VERSION=$(python3 scripts/lib/release_identity.py marketing_version)",
            './scripts/verify-ios-artifact.sh ipa "${IPAS[0]}"',
            '"display_name": os.environ["MEWLA_IOS_DISPLAY_NAME"]',
            '"marketing_version": os.environ["MEWLA_IOS_VERSION"]',
        ):
            self.assertIn(needle, IOS_RELEASE)
        for absent in ("expo prebuild", "pod install", '"marketing_version": base["version"]'):
            self.assertNotIn(absent, IOS_RELEASE)
        self.assertNotRegex(IOS_RELEASE, r'expected_bundle\s*=\s*"com\.daoleno\.mewla')

    def test_preview_uploads_with_an_individual_api_key(self):
        for needle in (
            "HD84J3DJ2B",
            "./.release-automation/scripts/app-store-connect-upload.py",
            '--app-id "$MEWLA_ASC_APP_ID"',
            'EXTRA_ARGS=(--reuse-existing-build --beta-group-name "Mewla Preview" --submit-beta-review)',
            '"${EXTRA_ARGS[@]}"',
            "mewla-asc-individual-key.p8",
        ):
            self.assertIn(needle, IOS_RELEASE)
        for absent in ("MEWLA_ASC_ISSUER_ID", "altool", "--apiIssuer", "iTMSTransporter", "ipa.read_bytes()"):
            self.assertNotIn(absent, IOS_RELEASE)

    def test_apple_secrets_stay_out_of_the_archive_job_environment(self):
        job_env = re.search(r"jobs:\s*\n\s*archive:[\s\S]*?\n\s{4}env:\s*\n([\s\S]*?)\n\s{4}steps:", IOS_RELEASE)
        self.assertIsNotNone(job_env)
        self.assertNotIn("secrets.", job_env.group(1))
        self.assertIn("MEWLA_ASC_API_KEY_BASE64: ${{ secrets.ZEN_ASC_API_KEY_BASE64 }}", IOS_RELEASE)
        self.assertIn("if: always()", IOS_RELEASE)
        self.assertIn("${{ runner.temp }}/mewla-asc-individual-key.p8", IOS_RELEASE)

    def test_ios_caches_hold_only_unsigned_native_inputs_and_verified_output(self):
        self.assertIn("ios-native-inputs-", IOS_NATIVE)
        self.assertIn("ios-ghostty-output-", IOS_NATIVE)
        self.assertIn("path: native/terminal-vt/libs/ios", IOS_NATIVE)
        self.assertIn("steps.ghostty-output-cache.outputs.cache-hit != 'true'", IOS_NATIVE)
        self.assertIn("run: ./scripts/verify-libghostty-ios.sh", IOS_NATIVE)
        # The client has no CocoaPods.
        self.assertNotIn("cocoapods", IOS_NATIVE.lower())
        blocks = cache_blocks(IOS_RELEASE, IOS_NATIVE, NATIVE_CACHE)
        for forbidden in (".p12", ".mobileprovision", ".keychain", ".xcarchive", ".ipa"):
            self.assertNotIn(forbidden, blocks)

    def test_tags_and_pull_requests_restore_and_only_the_main_warmer_saves(self):
        for source in (IOS_RELEASE, CLIENT):
            self.assertIn("uses: ./.github/actions/ios-native", source)
            self.assertNotIn("save-caches", source)
        self.assertNotRegex(IOS_RELEASE, r"uses: actions/cache(/save)?@")
        self.assertRegex(NATIVE_CACHE, r'uses: \./\.github/actions/ios-native\s+with:\s+save-caches: "true"')
        self.assertIn("runs-on: xcode-27", NATIVE_CACHE)
        self.assertNotIn("secrets.", NATIVE_CACHE)

    def test_simulator_builds_target_the_architecture_the_xcframework_ships(self):
        self.assertIn("runs-on: xcode-27", CLIENT)
        self.assertIn("runs-on: xcode-27", NATIVE_LIBS)
        self.assertIn("-destination 'generic/platform=iOS Simulator'", NATIVE_LIBS)
        self.assertIn("ARCHS=arm64", NATIVE_LIBS)
        # NativeShell.xcframework and GhosttyVt.xcframework ship an arm64 Simulator slice only.
        self.assertIn("EXCLUDED_ARCHS[sdk=iphonesimulator*]: x86_64", PROJECT_YML)

    def test_the_heavy_native_libs_workflow_is_manual_only(self):
        self.assertIn("workflow_dispatch:", NATIVE_LIBS)
        self.assertNotRegex(NATIVE_LIBS, r"push:\s*\n\s*tags:")

    def test_the_release_always_builds_and_signs_the_live_extension(self):
        self.assertIn('MEWLA_IOS_LIVE: "1"', IOS_RELEASE)
        self.assertNotIn("live_extension", IOS_RELEASE)
        self.assertRegex(IOS_RELEASE, r"for name in \\\n[\s\S]*?MEWLA_APPLE_LIVE_PROVISIONING_PROFILE_BASE64 \\\n[\s\S]*?MEWLA_APPLE_TEAM_ID; do")
        self.assertIn('("extension", sys.argv[2], f"{bundle}.live", "live_")', IOS_RELEASE)
        self.assertIn('group = "group." + bundle', IOS_RELEASE)
        self.assertIn("regenerate it after enabling App Groups", IOS_RELEASE)
        # The client's project embeds the extension in every build, so CI compiles it too.
        self.assertIn("{ target: MewlaLiveWidgets, embed: true }", PROJECT_YML)

    def test_each_target_takes_its_own_profile(self):
        self.assertIn("PROVISIONING_PROFILE_SPECIFIER: $(MEWLA_APP_PROFILE)", PROJECT_YML)
        self.assertIn("PROVISIONING_PROFILE_SPECIFIER: $(MEWLA_LIVE_PROFILE)", PROJECT_YML)
        self.assertIn('MEWLA_APP_PROFILE="$PROFILE_NAME"', IOS_RELEASE)
        self.assertIn('MEWLA_LIVE_PROFILE="$LIVE_PROFILE_NAME"', IOS_RELEASE)
        self.assertNotIn("PROVISIONING_PROFILE_SPECIFIER=", IOS_RELEASE)
        self.assertIn('os.environ["MEWLA_IOS_BUNDLE_ID"] + ".live": sys.argv[4]', IOS_RELEASE)
        self.assertIn("${LIVE_PROFILE_UUID}.mobileprovision", IOS_RELEASE)
        self.assertIn("mewla-live-app-store.mobileprovision", IOS_RELEASE)


class CIWorkflowTests(unittest.TestCase):
    def test_the_offline_installer_suite_runs_in_a_bounded_linux_job(self):
        self.assertIn('"installer:test": "./scripts/tests/install_test.sh"', ROOT_PACKAGE)
        self.assertRegex(CI, r"installer:\s*[\s\S]*?runs-on: ubuntu-latest[\s\S]*?timeout-minutes: 5[\s\S]*?run: bun run installer:test")

    def test_ci_runs_on_main_pushes_release_tags_pull_requests_and_dispatch(self):
        self.assertRegex(CI, r'push:\s*\n\s*branches: \[main\]\s*\n\s*tags:\s*\n\s*- "v\*\.\*\.\*"\s*\n\s*- "v\*\.\*\.\*-beta\.\*"')
        self.assertRegex(CI, r"pull_request:\s*\n\s*workflow_dispatch:")

    def test_the_client_job_runs_core_ui_license_and_viewer_checks_on_every_push(self):
        client = between(CI, "  client:\n", "  client-ui:")
        for needle in (
            "run: bun test core",
            "run: ${NSH_HOME:-$HOME/.nsh}/bin/nsh check",
            "run: python3 client/ui/more/settings/licenses/generate.py --check",
            "run: bun install --frozen-lockfile",
            "run: bun client/ui/more/screen/runtime/generate.mjs --check",
        ):
            self.assertIn(needle, client)
        self.assertNotIn("if:", client)
        # The UI suite runs in shards, one runner each; every shard of the count, nothing else.
        ui = between(CI, "  client-ui:\n", "  windows:")
        self.assertNotIn("if:", ui)
        count = int(re.search(r"--shard \$\{\{ matrix\.shard \}\}/(\d+)", ui).group(1))
        shards = re.search(r"matrix:\s*\n\s*shard: \[([\d, ]+)\]", ui).group(1)
        self.assertEqual([int(n) for n in shards.split(",")], list(range(1, count + 1)))
        self.assertIn(f"name: Client UI tests (${{{{ matrix.shard }}}}/{count})", ui)
        self.assertIn("fail-fast: false", ui)

    def test_no_job_builds_the_expo_app(self):
        for source in (CI, CLIENT, NATIVE_CACHE, NATIVE_LIBS, RELEASE, IOS_RELEASE, PREPARATION):
            self.assertNotIn("working-directory: app", source)
            self.assertNotRegex(source, r"\bexpo (prebuild|export)\b")
            self.assertNotRegex(source, r"(^|[\s'\"(])app/", "a workflow still reads the Expo app's directory")

    def test_daemon_tests_and_vet_run_on_every_push_and_release_targets_are_gated(self):
        daemon = between(CI, "  daemon:\n", "  client:\n")
        self.assertRegex(daemon, r"run: go test \./\.\.\.[\s\S]*run: go vet \./\.\.\.[\s\S]*run: python3 scripts/test-plugin-publisher-flags\.py")
        self.assertIn(f"name: Build release targets\n        {RELEASE_ONLY}", daemon)
        self.assertIn(f"name: Install NSH for the web UI\n        {RELEASE_ONLY}", daemon)

    def test_the_native_contract_runs_on_every_push_without_native_libraries(self):
        contract = between(CI, "  native-contract:\n", "  release-identity:")
        self.assertIn("run: ./scripts/verify-libghostty.sh --contract", contract)
        self.assertNotIn("if:", contract)
        self.assertNotRegex(CI, r"python3 <<'PY'[\s\S]*?zig")


if __name__ == "__main__":
    unittest.main()
