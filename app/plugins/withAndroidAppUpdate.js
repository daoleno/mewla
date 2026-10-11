/**
 * Expo config plugin: let GitHub APK builds install their own verified update.
 * Adds REQUEST_INSTALL_PACKAGES and the app-update FileProvider only for the
 * github-apk channel, so a store build never asks to install other packages.
 */
const { createRunOncePlugin, withAndroidManifest } = require('@expo/config-plugins');

const PERMISSION = 'android.permission.REQUEST_INSTALL_PACKAGES';
const PROVIDER = 'expo.modules.appupdate.AppUpdateFileProvider';

function addAppUpdateInstall(androidManifest) {
  const manifest = androidManifest?.manifest;
  const application = manifest?.application?.[0];
  if (!application) {
    throw new Error('withAndroidAppUpdate: Android application manifest entry not found');
  }
  manifest['uses-permission'] = manifest['uses-permission'] || [];
  if (!manifest['uses-permission'].some((item) => item.$?.['android:name'] === PERMISSION)) {
    manifest['uses-permission'].push({ $: { 'android:name': PERMISSION } });
  }
  application.provider = application.provider || [];
  if (!application.provider.some((item) => item.$?.['android:name'] === PROVIDER)) {
    application.provider.push({
      $: {
        'android:name': PROVIDER,
        'android:authorities': '${applicationId}.appupdate',
        'android:exported': 'false',
        'android:grantUriPermissions': 'true',
      },
      'meta-data': [
        {
          $: {
            'android:name': 'android.support.FILE_PROVIDER_PATHS',
            'android:resource': '@xml/mewla_app_update_paths',
          },
        },
      ],
    });
  }
  return androidManifest;
}

function withAndroidAppUpdate(config, { channel } = {}) {
  if (channel !== 'github-apk') return config;
  return withAndroidManifest(config, (cfg) => {
    cfg.modResults = addAppUpdateInstall(cfg.modResults);
    return cfg;
  });
}

module.exports = createRunOncePlugin(withAndroidAppUpdate, 'withAndroidAppUpdate');
module.exports.addAppUpdateInstall = addAppUpdateInstall;
