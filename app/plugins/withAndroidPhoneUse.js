/**
 * Expo config plugin: phone use belongs to the GitHub APK. It starts a helper through the phone's
 * own Wireless debugging and reads the pairing code with an accessibility service, which Google
 * Play does not allow an app like this to do, so any other channel leaves its manifest entries out.
 */
const { createRunOncePlugin, withAndroidManifest } = require('@expo/config-plugins');

const SERVICES = [
  'expo.modules.mewlaphone.PairingAccessibilityService',
  'expo.modules.mewlaphone.PhoneUseService',
];
const RECEIVERS = ['expo.modules.mewlaphone.PhoneActionReceiver'];
const PERMISSIONS = ['android.permission.FOREGROUND_SERVICE_SPECIAL_USE'];

function removePhoneUse(androidManifest) {
  const manifest = androidManifest?.manifest;
  const application = manifest?.application?.[0];
  if (!application) {
    throw new Error('withAndroidPhoneUse: Android application manifest entry not found');
  }
  manifest.$ = manifest.$ || {};
  manifest.$['xmlns:tools'] = 'http://schemas.android.com/tools';
  const name = (item) => item.$?.['android:name'];
  const remove = (list, names) => {
    const kept = (list || []).filter((item) => !names.includes(name(item)));
    for (const entry of names) {
      if (!kept.some((item) => name(item) === entry && item.$?.['tools:node'] === 'remove')) {
        kept.push({ $: { 'android:name': entry, 'tools:node': 'remove' } });
      }
    }
    return kept;
  };
  application.service = remove(application.service, SERVICES);
  application.receiver = remove(application.receiver, RECEIVERS);
  manifest['uses-permission'] = remove(manifest['uses-permission'], PERMISSIONS);
  return androidManifest;
}

function withAndroidPhoneUse(config, { channel } = {}) {
  if (channel === 'github-apk') return config;
  return withAndroidManifest(config, (cfg) => {
    cfg.modResults = removePhoneUse(cfg.modResults);
    return cfg;
  });
}

module.exports = createRunOncePlugin(withAndroidPhoneUse, 'withAndroidPhoneUse');
module.exports.removePhoneUse = removePhoneUse;
