/**
 * Expo config plugin: route Android FCM data messages through Mewla's service so a
 * live_state push updates the Live Update notification and widgets with the app closed.
 *
 * Exactly one FirebaseMessagingService may own MESSAGING_EVENT, so Expo's registration is
 * removed from the merged manifest and replaced by a subclass that passes every message
 * that is not a live_state straight back to expo-notifications.
 */
const { createRunOncePlugin, withAndroidManifest } = require('@expo/config-plugins');

const EXPO_SERVICE = 'expo.modules.notifications.service.ExpoFirebaseMessagingService';
const MEWLA_SERVICE = 'expo.modules.mewlalive.MewlaFirebaseMessagingService';
const MESSAGING_EVENT = 'com.google.firebase.MESSAGING_EVENT';

function addMewlaLive(androidManifest) {
  const manifest = androidManifest?.manifest;
  const application = manifest?.application?.[0];
  if (!application) {
    throw new Error('withAndroidMewlaLive: Android application manifest entry not found');
  }
  manifest.$ = manifest.$ || {};
  manifest.$['xmlns:tools'] = 'http://schemas.android.com/tools';
  application.service = application.service || [];
  const name = (item) => item.$?.['android:name'];
  if (!application.service.some((item) => name(item) === EXPO_SERVICE && item.$?.['tools:node'] === 'remove')) {
    application.service = application.service.filter((item) => name(item) !== EXPO_SERVICE);
    application.service.push({ $: { 'android:name': EXPO_SERVICE, 'tools:node': 'remove' } });
  }
  if (!application.service.some((item) => name(item) === MEWLA_SERVICE)) {
    application.service.push({
      // Lint cannot see through the compileOnly expo-notifications supertype in the app module and
      // reports the service as not extending Service; it does (FirebaseMessagingService).
      $: { 'android:name': MEWLA_SERVICE, 'android:exported': 'false', 'tools:ignore': 'Instantiatable' },
      'intent-filter': [{ action: [{ $: { 'android:name': MESSAGING_EVENT } }] }],
    });
  }
  return androidManifest;
}

function withAndroidMewlaLive(config) {
  return withAndroidManifest(config, (cfg) => {
    cfg.modResults = addMewlaLive(cfg.modResults);
    return cfg;
  });
}

module.exports = createRunOncePlugin(withAndroidMewlaLive, 'withAndroidMewlaLive');
module.exports.addMewlaLive = addMewlaLive;
