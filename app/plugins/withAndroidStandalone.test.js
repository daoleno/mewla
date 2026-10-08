import { expect, test } from 'bun:test';
import { injectStandaloneGradle, injectStandaloneMainApplication } from './withAndroidRelease.js';

test('standalone is an explicit build property, not a change to normal dev mode', () => {
  const generated = injectStandaloneGradle('android {\n defaultConfig {\n }\n}');
  expect(generated).toContain("findProperty('mewlaStandalone') ?: 'false'");
  expect(generated).toContain('"boolean", "MEWLA_STANDALONE"');
  expect(injectStandaloneGradle(generated)).toBe(generated);
});

test('standalone debug uses the embedded bundle without dev support', () => {
  const input = 'ExpoReactHostFactory.getDefaultReactHost(\n context = applicationContext\n)';
  const generated = injectStandaloneMainApplication(input);
  expect(generated).toContain('useDevSupport = BuildConfig.DEBUG && !BuildConfig.MEWLA_STANDALONE,');
  expect(injectStandaloneMainApplication(generated)).toBe(generated);
});

test('unexpected host factory and existing dev-support overrides fail closed', () => {
  expect(() => injectStandaloneMainApplication('OtherFactory.create()')).toThrow();
  expect(() => injectStandaloneMainApplication('ExpoReactHostFactory.getDefaultReactHost(useDevSupport = true)')).toThrow();
  expect(() => injectStandaloneGradle('unknown layout')).toThrow();
});
