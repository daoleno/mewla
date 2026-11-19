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


def between(text, start, end=None):
    """The text from `start` up to `end` (or the end)."""
    begin = text.index(start)
    return text[begin:text.index(end, begin)] if end else text[begin:]


def cache_blocks(*sources):
    pattern = re.compile(r"uses: actions/cache(?:/restore)?@v4[\s\S]*?(?=\n\s*- name:|$)")
    return "\n".join(match.group(0) for source in sources for match in pattern.finditer(source))


class ReleaseArtifactsWorkflowTests(unittest.TestCase):
    def test_prepares_native_release_inputs_before_compilation(self):
        self.assertIn("./scripts/build-daemon-linux.sh --out-dir", RELEASE)
        self.assertRegex(ANDROID_NATIVE, r"uses: android-actions/setup-android@v3\s+with:\s+packages: platform-tools")
        for source in (RELEASE, NATIVE_CACHE, CLIENT):
            self.assertIn("uses: ./.github/actions/android-native", source)

    def test_builds_the_signed_apk_from_the_nsh_client(self):
        android = between(RELEASE, "  android:\n", "  android-launch:")
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

    def test_publishes_once_and_only_after_every_check_passed(self):
        self.assertIn("daemon:", RELEASE)
        self.assertIn("android:", RELEASE)
        gate = between(RELEASE, "  release-ready:\n", "\n  publish:\n    name:")
        self.assertIn("needs: [validate, daemon, daemon-linux-arm64, daemon-windows, daemon-darwin, android, android-launch, stage]", gate)
        self.assertIn("if: always()", gate)
        # Cancelled or skipped is not passed; only a candidate's unsigned stage may be skipped.
        self.assertIn('ok = result == "success" or (job == "stage" and result == "skipped" and mode == "candidate")', gate)
        publish = between(RELEASE, "\n  publish:\n    name:", "  ios-testflight:\n")
        self.assertIn("needs: [validate, release-ready]\n    if: needs.validate.outputs.publish == 'true'\n", publish)
        # Nothing lets publication run past a red or missing gate.
        for override in ("always()", "failure()", "cancelled()", "!cancelled", "success() ||"):
            self.assertNotIn(override, publish)
        self.assertRegex(RELEASE, r"stage:\s*\n\s*name: [^\n]+\n\s*needs: \[validate, daemon, daemon-linux-arm64, daemon-windows, daemon-darwin, android\]\n")
        for needle in (
            'gh release create "$TAG" --verify-tag --draft',
            'gh release upload "$TAG" "${assets[@]}" --clobber',
            'gh release edit "$TAG" --draft=false',
            "is already public with other bytes; refusing to replace them",
            './scripts/stage-release.sh --skip-build --apk "$APK"',
            "sha256sum -c SHA256SUMS",
            "grep -Eq 'ELF 64-bit.*(x86-64|x86_64).*statically linked'",
            "grep -Eq 'Mach-O 64-bit.*arm64'",
            '--out-dir "$GITHUB_WORKSPACE/dist-download/staging/bin"',
            "SOURCE_DATE_EPOCH",
        ):
            self.assertIn(needle, RELEASE)
        # No hold: a stable tag is Latest, a beta tag a prerelease, decided by the tag alone.
        self.assertNotIn("RELEASE_HOLD", RELEASE)
        self.assertIn('if [[ "$TAG" == *-beta.* ]]; then', publish)
        for gone in ("desktop-stage:", "desktop-publish:", "complete-stage:", "android-attach:"):
            self.assertNotIn(gone, RELEASE)

    def test_the_release_launches_the_exact_apk_and_runs_no_suite(self):
        launch = between(RELEASE, "  android-launch:\n", "  stage:\n")
        self.assertIn("needs: validate\n", launch)
        for needle in ("ARTIFACT: mewla-android-apk-${{ needs.validate.outputs.tag }}",
                       "BUILD_JOB: Build and verify signed Android", "script: client/scripts/release-launch.sh"):
            self.assertIn(needle, launch)
        android_name = re.search(r"\n  android:\n    needs: validate\n    name: ([^\n]+)\n", RELEASE).group(1)
        self.assertEqual(android_name, "Build and verify signed Android")
        script = read("client/scripts/release-launch.sh")
        for step in ("versionName", "pidof", "-b crash"):
            self.assertIn(step, script)
        # The suites ran on main: the release checks that and runs none itself.
        self.assertIn('python3 scripts/release-green.py --sha "$(git rev-parse HEAD)"', between(RELEASE, "  validate:\n", "  web-ui:\n"))
        self.assertIn("python3 scripts/release-green.py", PREPARATION)
        for suite in ("release-upgrade.sh", "nsh test", "bun test", "go test ./..."):
            self.assertNotIn(suite, RELEASE)
        self.assertNotIn("go test", PREPARATION)

    def test_the_upgrade_from_the_latest_release_runs_on_mains_schedule(self):
        upgrade = read(".github/workflows/upgrade.yml")
        for needle in ("schedule:", "script: client/scripts/release-upgrade.sh", "./scripts/android-release-apk.sh",
                       "sha256sum -c -", "gh issue create"):
            self.assertIn(needle, upgrade)
        # The start and the target are inputs: main (built) or a release tag (its published APK), and the start
        # defaults to the release before the target. Only a main target may equal the latest release's versionCode.
        for needle in ("      from:\n", "      to:\n", 'default: "main"', "if: env.TARGET != 'main'", "if: env.TARGET == 'main'",
                       "tests a release against itself",
                       "MEWLA_UPGRADE_SAME_VERSION_OK: ${{ (inputs.to || 'main') == 'main' && '1' || '' }}"):
            self.assertIn(needle, upgrade)
        script = read("client/scripts/release-upgrade.sh")
        for step in ("install -r", "is not an upgrade", "same device", "the draft is not in storage", "qr-camera", "MEWLA_NO_AUTOUPDATE=1"):
            self.assertIn(step, script)

    def test_notes_are_written_at_release_and_released_is_checked(self):
        publish = between(RELEASE, "  publish:\n", "  released:\n")
        self.assertIn('python3 scripts/release-body.py --tag "$TAG"', publish)
        self.assertNotIn("docs/releases/", publish)
        released = between(RELEASE, "  released:\n", "  ios-testflight:\n")
        self.assertIn("needs: [validate, publish]", released)
        self.assertIn('python3 scripts/released.py --tag "$TAG"', released)

    def test_the_iphone_build_follows_publication(self):
        ios = between(RELEASE, "  ios-testflight:\n")
        self.assertIn("needs: [validate, publish]", ios)
        self.assertIn("actions: write", ios)
        for needle in ("gh workflow run ios-release.yml", '-f "app_identity=preview"', '-f "destination=testflight"',
                       "python3 scripts/lib/release_identity.py ios_build_number"):
            self.assertIn(needle, ios)
        # Only the publish job can write the repository, and only this one can dispatch.
        self.assertEqual(RELEASE.count("contents: write"), 1)
        self.assertEqual(RELEASE.count("actions: write"), 1)
        self.assertNotIn("ios-release.yml", PREPARATION)

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
        self.assertEqual(RELEASE.count("mewla-windows-amd64.zip"), 1)
        self.assertEqual(len(re.findall(r"go test -count=1 -v \./releasesmoke/", RELEASE)), 3)

    def test_the_daemons_inputs_come_from_mains_caches_or_are_built_here(self):
        cache = read(".github/actions/desktop-release-cache/action.yml")
        warmer = read(".github/workflows/native-cache.yml")
        self.assertNotIn("  engine:\n", RELEASE)
        # Restore-only in a release; main's native-cache.yml is the one writer.
        for job, end, uses in (
            ("web-ui", "daemon-build", 'web: "true"'),
            ("daemon-build", "daemon", "go-target: ${{ matrix.target }}"),
            ("daemon-darwin", "android", "lib: darwin_arm64"),
        ):
            block = between(RELEASE, f"  {job}:\n", f"  {end}:\n")
            self.assertIn("uses: ./.github/actions/desktop-release-cache", block, job)
            self.assertIn(uses, block, job)
            self.assertNotIn("save-caches", block, job)
            self.assertIn("ref: ${{ needs.validate.outputs.checkout }}", block, job)
        self.assertEqual(warmer.count('save-caches: "true"'), 2 + 3)
        for job in ("desktop", "web", "darwin"):
            self.assertIn(f"  {job}:\n    name:", warmer)
        # Keys hash what derives each output, never the app's version or sources.
        self.assertIn("ghostty-release-v1-${{ inputs.lib }}-${{ hashFiles('native/terminal-vt/native.lock.json', 'scripts/build-libghostty-desktop.sh'", cache)
        self.assertIn("ghostty-web-v1-${{ hashFiles('native/terminal-vt/native.lock.json', 'scripts/build-libghostty-web.sh'", cache)
        self.assertIn("path: .cache/mewla-tools/*.tar.xz", cache)
        self.assertNotIn("version.go", cache)
        # A restored engine is used only when its manifest matches: the builds still run the scripts.
        self.assertIn('export MEWLA_GHOSTTY_DESKTOP_CACHE="$HOME/.cache/mewla/ghostty-release/${{ matrix.lib }}"', RELEASE)
        self.assertIn("run: ./scripts/build-web-ui.sh", between(RELEASE, "  web-ui:\n", "  daemon-build:\n"))
        # The release APK skips the lint the test track runs; the Gradle home is saved without
        # transforms or other Gradle versions (v1 restored 4 GB of them in 56 s for nothing).
        self.assertIn("SKIP=(-x :app:lintVitalProductionRelease)", APK_SCRIPT)
        self.assertIn("assemblePreviewRelease", read(".github/workflows/client.yml"))
        self.assertIn("rm -rf ~/.gradle/caches/*/transforms", warmer)
        self.assertNotIn("android-gradle-v1-", read(".github/actions/android-native/action.yml"))
        # Each platform runs its own binary as soon as it is built.
        for job, target in (("daemon-linux-arm64", "linux-arm64"), ("daemon-windows", "windows-amd64")):
            block = between(RELEASE, f"  {job}:\n", "\n  daemon-")
            self.assertIn("needs: [validate, daemon-build]", block)
            self.assertIn(f"name: daemon-part-bin-{target}-${{{{ needs.validate.outputs.tag }}}}", block)
        # validate checks the tag once; signing and publication check it again before acting on it.
        for job, end in (("stage", "release-ready"), ("publish", "ios-testflight")):
            self.assertIn('./scripts/verify-release-identity.sh --tag "${{ needs.validate.outputs.tag }}"',
                          between(RELEASE, f"  {job}:\n", f"  {end}:\n"), job)
        # Intermediates never match the published pattern; the merged names stay what they were.
        self.assertNotRegex(RELEASE, r"name: mewla-daemon-binaries-[^\n]*matrix")
        self.assertIn("name: mewla-daemon-binaries-linux-${{ needs.validate.outputs.tag }}\n          pattern: daemon-part-bin-*-", RELEASE)
        self.assertIn("name: mewla-daemon-binaries-darwin-${{ needs.validate.outputs.tag }}", RELEASE)
        self.assertIn('== "./mewla-linux-amd64 ./mewla-linux-arm64 ./mewla-windows-amd64.exe " ]]', RELEASE)

    def test_candidates_are_built_without_signing_staging_or_publishing(self):
        self.assertIn("candidate_version:", RELEASE)
        self.assertIn("only a tag can be published", RELEASE)
        block = between(RELEASE, "  stage:\n")
        self.assertIn("if: needs.validate.outputs.mode == 'release'", block[:block.index("steps:")])
        self.assertEqual(len(re.findall(r"ZEN_UPDATE_SIGNING_KEY_BASE64 \}\}", RELEASE)), 1)

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

    def test_preview_builds_carry_the_train_and_the_real_release(self):
        identity = between(IOS_RELEASE, "      - name: Resolve closed iOS identity contract\n", "      - name: Verify tracked release identity\n")
        preview = between(identity, "            preview)\n", ";;")
        production = between(identity, "            production)\n", ";;")
        self.assertIn('MARKETING="$(python3 scripts/lib/release_identity.py testflight_version)"', preview)
        self.assertIn('MARKETING="$(python3 scripts/lib/release_identity.py marketing_version)"', production)
        self.assertIn('echo "MEWLA_APP_VERSION=$(python3 scripts/lib/release_identity.py version)"', identity)
        self.assertIn("Print :MewlaVersion", IOS_RELEASE)
        upload = between(IOS_RELEASE, "      - name: Upload verified IPA to TestFlight\n", "      - name: Submit to Beta App Review or wait behind the build in review\n")
        self.assertIn('--version "$MEWLA_IOS_MARKETING_VERSION"', upload)

    def test_the_testflight_pipeline_runs_on_app_store_connect_events_with_a_daily_sweep(self):
        self.assertIn('    - cron: "47 3 * * *"', REVIEW)
        self.assertEqual(REVIEW.count("cron:"), 1)
        self.assertIn("      asc_event:", REVIEW)
        self.assertNotIn("schedule:", AVAILABILITY)
        self.assertIn("  workflow_call:", AVAILABILITY)
        available = REVIEW[REVIEW.index("  available:\n"):]
        self.assertIn("uses: ./.github/workflows/ios-testflight-available.yml", available)
        self.assertIn("needs: review", available)
        self.assertIn("inputs.dry_run != true", available)
        worker = read("scripts/asc-webhook/receiver.mjs")
        wrangler = read("scripts/asc-webhook/wrangler.toml")
        self.assertIn('GITHUB_WORKFLOW = "ios-testflight-review.yml"', wrangler)
        self.assertIn('GITHUB_REF = "main"', wrangler)
        self.assertIn("inputs: { asc_event: eventType }", worker)

    def test_testflight_review_reads_with_the_preview_key_and_writes_nothing_to_the_repo(self):
        # Only the called availability job publishes (contents: write); the review job reads.
        review_job = REVIEW[:REVIEW.index("  available:\n")]
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
            self.assertIn(needle, review_job)
        self.assertIn("permissions:\n  contents: read", review_job)
        self.assertNotIn("contents: write", review_job)
        self.assertNotIn("ZEN_UPDATE_SIGNING_KEY_BASE64", REVIEW)
        self.assertNotRegex(REVIEW, r"\b67\d{8}\b")

    def test_a_preview_upload_joins_the_review_queue_instead_of_restarting_review(self):
        upload = between(IOS_RELEASE, "      - name: Upload verified IPA to TestFlight\n", "      - name: Submit to Beta App Review or wait behind the build in review\n")
        self.assertNotIn("--submit-beta-review", upload)
        step = between(IOS_RELEASE, "      - name: Submit to Beta App Review or wait behind the build in review\n", "      - name: Remove temporary signing material")
        for needle in (
            "if: ${{ env.MEWLA_IOS_DESTINATION == 'testflight' && env.MEWLA_IOS_APP_VARIANT == 'preview' }}",
            "./.release-automation/scripts/testflight-review-queue.py",
            '--app-id "$MEWLA_ASC_APP_ID"',
            '--beta-group-name "Mewla Preview"',
        ):
            self.assertIn(needle, step)
        self.assertNotIn("--dry-run", step)
        self.assertIn("scripts/testflight-review-queue.py\n", between(IOS_RELEASE, "path: .release-automation", "sparse-checkout-cone-mode"))
        self.assertNotIn("testflight-expire-waiting", IOS_RELEASE)
        # The hourly review job submits the newest waiting build once a review ends; dry_run only reads.
        queue = REVIEW[REVIEW.index("      - name: Submit the newest waiting build once no review is open"):]
        for needle in ("./scripts/testflight-review-queue.py", '--beta-group-name "Mewla Preview"', "dry_run:",
                       'if [[ "$DRY_RUN" == "true" ]]; then EXTRA_ARGS=(--dry-run); fi'):
            self.assertIn(needle, queue if needle != "dry_run:" else REVIEW)
        self.assertNotIn("--submit-beta-review", POSTPROCESS)
        self.assertIn("./scripts/testflight-review-queue.py", POSTPROCESS)

    def test_postprocess_resumes_against_the_configured_preview_app(self):
        self.assertIn("MEWLA_ASC_APP_ID: ${{ vars.MEWLA_ASC_PREVIEW_APP_ID }}", POSTPROCESS)
        self.assertIn('--app-id "$MEWLA_ASC_APP_ID"', POSTPROCESS)
        self.assertNotIn("6790486708", POSTPROCESS)

    def test_reviewed_tag_pushes_publish_as_preview(self):
        # A bare tag push never reaches TestFlight: the published release dispatches it.
        self.assertNotRegex(IOS_RELEASE, TAG_PUSH)
        self.assertNotRegex(IOS_RELEASE, r"\n  push:")
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
            'MARKETING_VERSION="$MEWLA_IOS_MARKETING_VERSION"',
            'CURRENT_PROJECT_VERSION="$MEWLA_IOS_BUILD_NUMBER"',
            'MEWLA_APP_VERSION="$MEWLA_APP_VERSION"',
            'expected_bundle = os.environ["MEWLA_IOS_BUNDLE_ID"]',
            'os.environ["MEWLA_IOS_BUNDLE_ID"]: sys.argv[3]',
            "${MEWLA_IOS_ARTIFACT_NAME}-v${MEWLA_IOS_VERSION}",
            "MEWLA_IOS_VERSION=$(python3 scripts/lib/release_identity.py marketing_version)",
            'MEWLA_IOS_VERSION="$MEWLA_IOS_MARKETING_VERSION" ./scripts/verify-ios-artifact.sh ipa "${IPAS[0]}"',
            '"display_name": os.environ["MEWLA_IOS_DISPLAY_NAME"]',
            '"marketing_version": os.environ["MEWLA_IOS_MARKETING_VERSION"]',
            '"version": os.environ["MEWLA_APP_VERSION"]',
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
            'EXTRA_ARGS=(--reuse-existing-build --beta-group-name "Mewla Preview")',
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


class ClientWorkflowTests(unittest.TestCase):
    def test_the_upgrade_gate_waits_for_this_runs_preview_by_the_android_jobs_name(self):
        android_name = re.search(r"\n  android:\n    name: ([^\n]+)\n", CLIENT).group(1)
        gate = between(CLIENT, "  upgrade-gate:\n")
        self.assertIn(f'select(.name == "{android_name}")', gate)
        self.assertIn("artifacts?name=mewla-preview-android", gate)
        self.assertLess(gate.index("name: Wait for this run's Preview"), gate.index("-n mewla-preview-android"))
        # The same upgrade path as before: Expo release → NSH, and the last Preview → this one.
        for needle in ("client/scripts/upgrade-gate.sh", "client/scripts/preview-upgrade.sh", "assembleProductionRelease"):
            self.assertIn(needle, gate)


class CIWorkflowTests(unittest.TestCase):
    def test_the_offline_installer_suite_runs_in_a_bounded_linux_job(self):
        self.assertIn('"installer:test": "./scripts/tests/install_test.sh"', ROOT_PACKAGE)
        self.assertRegex(CI, r"installer:\s*[\s\S]*?runs-on: ubuntu-latest[\s\S]*?timeout-minutes: 5[\s\S]*?run: bun run installer:test")

    def test_ci_is_the_test_track_main_runs_finish_and_releases_do_not_rerun_it(self):
        self.assertRegex(CI, r'push:\s*\n\s*branches: \[main\]\s*\n(\s*#.*\n)*\s*paths-ignore:')
        self.assertRegex(CI, r"pull_request:\s*\n\s*workflow_dispatch:")
        self.assertNotIn("tags:", between(CI, "on:\n", "jobs:\n"))
        self.assertIn("cancel-in-progress: ${{ github.ref != 'refs/heads/main' }}", CI)
        self.assertIn("cancel-in-progress: ${{ github.ref != 'refs/heads/main' }}", CLIENT)
        # Every job has its own timeout: a stuck runner or vendor never holds main's checks for hours.
        jobs = CI.split("\njobs:\n", 1)[1]
        for block in re.split(r"\n(?=  [a-z][a-z-]*:\n)", "\n" + jobs):
            if block.strip():
                self.assertIn("timeout-minutes:", block, block.strip().splitlines()[0])

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

    def test_daemon_tests_and_vet_run_on_every_push(self):
        daemon = between(CI, "  daemon:\n", "  daemon-engine:\n")
        self.assertRegex(daemon, r"run: go test \./\.\.\.[\s\S]*run: go vet \./\.\.\.[\s\S]*run: python3 scripts/test-plugin-publisher-flags\.py")
        self.assertIn("run: ./scripts/test-ghosttyvt.sh -race", between(CI, "  daemon-engine:\n", "  daemon-macos:\n"))

    def test_the_native_contract_runs_on_every_push_without_native_libraries(self):
        contract = between(CI, "  native-contract:\n", "  release-identity:")
        self.assertIn("run: ./scripts/verify-libghostty.sh --contract", contract)
        self.assertNotIn("if:", contract)
        self.assertNotRegex(CI, r"python3 <<'PY'[\s\S]*?zig")


if __name__ == "__main__":
    unittest.main()
