/**
 * Expo config plugin: phone use belongs to the GitHub APK. It starts a helper through the phone's
 * own Wireless debugging and reads the pairing code with an accessibility service, which Google
 * Play does not allow an app like this to do, so any other channel leaves its manifest entries out
 * (including the boot receiver and the secure-settings permission that keep it set up).
 * The GitHub APK also lets the main activity float the agent's screen (picture-in-picture).
 */
const { createRunOncePlugin, withAndroidManifest } = require('@expo/config-plugins');

const SERVICES = [
  'expo.modules.mewlaphone.PairingAccessibilityService',
  'expo.modules.mewlaphone.PhoneUseService',
  'expo.modules.mewlaphone.PhoneLinkTaskService',
];
const RECEIVERS = ['expo.modules.mewlaphone.PhoneActionReceiver', 'expo.modules.mewlaphone.PhoneBootReceiver'];
// RECEIVE_BOOT_COMPLETED stays: the live surfaces use it in every channel.
const PERMISSIONS = ['android.permission.FOREGROUND_SERVICE_SPECIAL_USE', 'android.permission.WRITE_SECURE_SETTINGS'];

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

function allowPictureInPicture(androidManifest) {
  const application = androidManifest?.manifest?.application?.[0];
  const main = (application?.activity || []).find((item) => item.$?.['android:name'] === '.MainActivity');
  if (!main) {
    throw new Error('withAndroidPhoneUse: MainActivity not found');
  }
  main.$['android:supportsPictureInPicture'] = 'true';
  return androidManifest;
}

function withAndroidPhoneUse(config, { channel } = {}) {
  return withAndroidManifest(config, (cfg) => {
    cfg.modResults = channel === 'github-apk' ? allowPictureInPicture(cfg.modResults) : removePhoneUse(cfg.modResults);
    return cfg;
  });
}

module.exports = createRunOncePlugin(withAndroidPhoneUse, 'withAndroidPhoneUse');
module.exports.removePhoneUse = removePhoneUse;
module.exports.allowPictureInPicture = allowPictureInPicture;
