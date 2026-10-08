import { describe, expect, test } from 'bun:test';
import {
  injectReleaseSigningGradle,
  injectDebugIdentityGradle,
  enablePrivateNetworkHTTP,
  NOTICE_APK_REL,
  NOTICE_SRC_REL,
} from './withAndroidRelease.js';

const sampleGradle = `
android {
    signingConfigs {
        debug {
            storeFile file('debug.keystore')
            storePassword 'android'
            keyAlias 'androiddebugkey'
            keyPassword 'android'
        }
    }
    buildTypes {
        debug {
            signingConfig signingConfigs.debug
        }
        release {
            // Caution! In production, you need to generate your own keystore file.
            signingConfig signingConfigs.debug
            minifyEnabled false
        }
    }
}
`;

describe('withAndroidRelease signing injection', () => {
  test('injects release signingConfigs from env and is idempotent', () => {
    const once = injectReleaseSigningGradle(sampleGradle);
    expect(once).toContain('def releaseKeystore = System.getenv("MEWLA_ANDROID_KEYSTORE")');
    for (const name of ['KEYSTORE_PASSWORD', 'KEY_ALIAS', 'KEY_PASSWORD']) {
      expect(once).toContain(`System.getenv("MEWLA_ANDROID_${name}")`);
      expect(once).not.toContain(`ZEN_ANDROID_${name}`);
    }
    expect(once).toContain('signingConfigs.release');
    expect(once).toContain('@generated begin android-release-signing');
    expect(once).toContain('@generated begin android-release-buildtype-signing');

    const twice = injectReleaseSigningGradle(once);
    const beginCount = (twice.match(/@generated begin android-release-signing/g) || [])
      .length;
    expect(beginCount).toBe(1);
    expect(twice).toContain('System.getenv("MEWLA_ANDROID_KEYSTORE")');
  });

  test('exports APK notice path contract', () => {
    expect(NOTICE_SRC_REL).toBe('assets/notices/GHOSTTY-MIT.txt');
    expect(NOTICE_APK_REL).toBe('assets/notices/GHOSTTY-MIT.txt');
  });
});

describe('withAndroidRelease debug identity injection', () => {
  test('names debug launcher Mewla Debug and suffixes package .debug', () => {
    const once = injectDebugIdentityGradle(sampleGradle);
    expect(once).toContain('applicationIdSuffix ".debug"');
    expect(once).toContain('resValue "string", "app_name", "Mewla Debug"');
    expect(once).toContain('@generated begin android-debug-identity');

    // Only buildTypes.debug — not signingConfigs.debug
    const buildTypesSlice = once.slice(once.indexOf('buildTypes'));
    expect(buildTypesSlice).toContain('applicationIdSuffix ".debug"');

    const twice = injectDebugIdentityGradle(once);
    const beginCount = (
      twice.match(/@generated begin android-debug-identity/g) || []
    ).length;
    expect(beginCount).toBe(1);
  });
});

describe('withAndroidRelease private-network HTTP config', () => {
  test('enables cleartext HTTP on the production application manifest', () => {
    const manifest = {
      manifest: {
        application: [{ $: { 'android:name': '.MainApplication' } }],
      },
    };

    const configured = enablePrivateNetworkHTTP(manifest);
    expect(configured.manifest.application[0].$['android:usesCleartextTraffic']).toBe('true');
    expect(configured.manifest.application[0].$['android:name']).toBe('.MainApplication');
  });

  test('fails clearly when the application manifest entry is absent', () => {
    expect(() => enablePrivateNetworkHTTP({ manifest: {} })).toThrow(
      'Android application manifest entry not found',
    );
  });
});
