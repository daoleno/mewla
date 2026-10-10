const { describe, expect, it } = require('bun:test');
const fs = require('fs');
const path = require('path');

const workflow = fs.readFileSync(
  path.join(__dirname, '..', '.github', 'workflows', 'release-artifacts.yml'),
  'utf8',
);
const identityVerifier = fs.readFileSync(
  path.join(__dirname, '..', 'scripts', 'verify-release-identity.sh'),
  'utf8',
);
const nativeVerifier = fs.readFileSync(
  path.join(__dirname, '..', 'scripts', 'verify-libghostty.sh'),
  'utf8',
);
const appPackage = fs.readFileSync(path.join(__dirname, 'package.json'), 'utf8');
const androidNative = fs.readFileSync(
  path.join(__dirname, '..', '.github', 'actions', 'android-native', 'action.yml'),
  'utf8',
);
const nativeCache = fs.readFileSync(
  path.join(__dirname, '..', '.github', 'workflows', 'native-cache.yml'),
  'utf8',
);

describe('release asset workflow contract', () => {
  it('prepares native release inputs before compilation in release and ordinary CI', () => {
    const ci = fs.readFileSync(path.join(__dirname, '..', '.github', 'workflows', 'ci.yml'), 'utf8');
    expect(ci).toContain(':app:assembleRelease');
    expect(ci).toContain('./scripts/build-daemon-linux.sh --out-dir');
    expect(androidNative).toMatch(/uses: android-actions\/setup-android@v3\s+with:\s+packages: platform-tools/);
    for (const source of [workflow, ci, nativeCache]) {
      expect(source).toContain('uses: ./.github/actions/android-native');
    }
  });
  it('rejects RN-normalized asset collisions before native and signed builds', () => {
    const gate = workflow.indexOf('run: bun test androidAssetNames.test.js');
    expect(gate).toBeGreaterThan(0);
    expect(gate).toBeLessThan(workflow.indexOf('uses: ./.github/actions/android-native'));
    const preparation = fs.readFileSync(
      path.join(__dirname, '..', '.github', 'workflows', 'release-next-beta.yml'), 'utf8',
    );
    expect(preparation).toContain('androidAssetNames.test.js');
    expect(preparation.indexOf('androidAssetNames.test.js')).toBeLessThan(
      preparation.indexOf('- name: Commit and annotate exact prepared release'),
    );
  });
  it('uses immutable stable or beta tag pushes as the only automatic publication path', () => {
    expect(workflow).toMatch(
      /push:\s*\n\s*tags:\s*\n\s*- "v\*\.\*\.\*"\s*\n\s*- "v\*\.\*\.\*-beta\.\*"/,
    );
    expect(workflow).not.toMatch(/release:\s*\n\s*types:/);
    expect(workflow).toContain('type: boolean');
    expect(workflow).toContain("needs.validate.outputs.publish == 'true'");
    expect(workflow).toContain('./scripts/verify-release-identity.sh --tag');
    expect(identityVerifier).toContain('release tag $RELEASE_TAG does not match tracked version');
    expect(identityVerifier).toContain('checked-out release tag does not resolve to HEAD');
    expect(identityVerifier).toContain('release tag commit is not on origin/main');
    expect(workflow).toContain('RELEASE_IS_PRERELEASE=true');
    expect(workflow).toContain('RELEASE_IS_PRERELEASE=false');
    expect(workflow).toContain('--prerelease="$RELEASE_IS_PRERELEASE"');
    expect(workflow).toContain('--latest="$RELEASE_IS_STABLE"');
    expect(workflow).toContain('gh release create "$TAG" --verify-tag --draft');
    expect(workflow).toContain('gh release upload "$TAG" "${assets[@]}" --clobber');
    expect(workflow).toContain('gh release edit "$TAG" --draft=false');
  });

  it('publishes desktop archives first and attaches the signed APK when it is ready', () => {
    expect(workflow).toContain('daemon:');
    expect(workflow).toContain('android:');
    expect(workflow).toMatch(/desktop-stage:\s*\n\s*name: [^\n]+\n\s*needs: \[validate, daemon\]\n/);
    expect(workflow).toMatch(/desktop-publish:\s*\n\s*name: [^\n]+\n\s*needs: \[validate, desktop-stage\]\n/);
    expect(workflow).toContain('needs: [validate, daemon, android, desktop-stage]');
    expect(workflow).toContain('needs: [validate, desktop-publish, complete-stage]');
    expect(workflow).toContain('./scripts/stage-release.sh --skip-build\n');
    // The complete set must reuse the already-public archive bytes.
    expect(workflow).toContain('diff -u <(daemon_sums inputs/desktop/SHA256SUMS) <(daemon_sums "$STAGE/SHA256SUMS")');
    expect(workflow).toContain('sha256sum -c --ignore-missing SHA256SUMS');
    expect(workflow).toContain("grep -Eq 'ELF 64-bit.*(x86-64|x86_64)'");
    expect(workflow).toContain("grep -Eq 'Mach-O 64-bit.*arm64'");
    expect(workflow).toContain('--out-dir "$GITHUB_WORKSPACE/dist-download/staging/bin"');
    expect(workflow).toContain('./scripts/stage-release.sh --skip-build --apk "$APK"');
    expect(workflow).toContain('SOURCE_DATE_EPOCH');
  });

  it('keeps recovery reviewed and caches no signing material or signed output', () => {
    expect(workflow).toContain('workflow_dispatch:');
    expect(workflow).toContain('publish:');
    expect(appPackage).toContain('--build-cache');
    expect(workflow).toContain('-Dorg.gradle.jvmargs=-Xmx6g');
    expect(androidNative).toContain('~/.gradle/caches');
    expect(androidNative).toContain('android-native-inputs-');
    expect(androidNative).toContain('android-ghostty-output-v2-arm64-');
    expect(androidNative).toContain('app/modules/terminal-vt/android/src/main/cpp/ghostty');
    expect(androidNative).toContain('app/modules/terminal-vt/patches/android/**');
    expect(androidNative).toContain('scripts/verify-android-native-symbols.py');
    expect(androidNative).toContain("steps.ghostty-output-cache.outputs.cache-hit != 'true'");
    expect(androidNative).toContain('run: ./scripts/verify-libghostty.sh --release');
    expect(nativeVerifier).toContain('bad "missing pinned header $HEADERS_DIR/vt.h"');
    // ccache: pinned binary, content-checked compiler, saved only from main.
    expect(androidNative).toContain('7766991b91b3a5a177ab33fa043fe09e72c68586d5a86d20a563a05b74f119c0');
    expect(androidNative).toContain('CCACHE_COMPILERCHECK=content');
    expect(androidNative).toContain('android-ccache-v1-');
    // Tag runs only restore; the main-branch warmer is the one cache writer.
    expect(workflow).not.toContain('save-caches');
    expect(workflow).not.toMatch(/uses: actions\/cache(\/save)?@/);
    expect(nativeCache).toContain('save-caches: "true"');
    expect(nativeCache).not.toContain('secrets.');
    const cacheBlocks = [workflow, androidNative, nativeCache]
      .flatMap((source) => [...source.matchAll(/uses: actions\/cache(?:\/restore)?@v4[\s\S]*?(?=\n\s*- name:|$)/g)])
      .map((match) => match[0])
      .join('\n');
    expect(cacheBlocks).toContain('~/.gradle/caches');
    for (const forbidden of ['keystore', '.p12', '.jks', '.apk', 'dist-download']) {
      expect(cacheBlocks.toLowerCase()).not.toContain(forbidden);
    }
  });
});
