import { describe, expect, test } from 'bun:test';
import fs from 'fs';
import os from 'os';
import path from 'path';
import {
  injectReleaseSigningGradle,
  injectDebugIdentityGradle,
  enablePrivateNetworkHTTP,
  networkSecurityConfigXml,
  writeNetworkSecurityConfig,
  NETWORK_SECURITY_CONFIG_REL,
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
  test('points the production manifest at the network security config, not a global flag', () => {
    const manifest = {
      manifest: {
        application: [
          {
            $: {
              'android:name': '.MainApplication',
              'android:usesCleartextTraffic': 'true',
            },
          },
        ],
      },
    };

    const configured = enablePrivateNetworkHTTP(manifest);
    const attributes = configured.manifest.application[0].$;
    expect(attributes['android:networkSecurityConfig']).toBe('@xml/network_security_config');
    expect(attributes['android:usesCleartextTraffic']).toBeUndefined();
    expect(attributes['android:name']).toBe('.MainApplication');
  });

  test('fails clearly when the application manifest entry is absent', () => {
    expect(() => enablePrivateNetworkHTTP({ manifest: {} })).toThrow(
      'Android application manifest entry not found',
    );
  });

  test('allows cleartext by default for LAN and tailnet IPs but never for Mewla domains', () => {
    const xml = networkSecurityConfigXml();
    expect(xml).toContain('<base-config cleartextTrafficPermitted="true" />');
    expect(xml).toContain(
      '<domain-config cleartextTrafficPermitted="false">\n        <domain includeSubdomains="true">mewla.ai</domain>',
    );
    // Android matches hosts by name; an address range cannot be expressed.
    expect(xml).not.toMatch(/\d+\.\d+\.\d+\.\d+/);
  });

  test('writes the config into the generated project', () => {
    const root = fs.mkdtempSync(path.join(os.tmpdir(), 'mewla-nsc-'));
    try {
      const dest = writeNetworkSecurityConfig(root);
      expect(dest).toBe(path.join(root, NETWORK_SECURITY_CONFIG_REL));
      expect(fs.readFileSync(dest, 'utf8')).toBe(networkSecurityConfigXml());
    } finally {
      fs.rmSync(root, { recursive: true, force: true });
    }
  });
});
