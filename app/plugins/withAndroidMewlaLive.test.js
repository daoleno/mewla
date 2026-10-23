const { expect, test } = require('bun:test');
const { addMewlaLive } = require('./withAndroidMewlaLive');

const EXPO = 'expo.modules.notifications.service.ExpoFirebaseMessagingService';
const MEWLA = 'expo.modules.mewlalive.MewlaFirebaseMessagingService';

test('removes Expo FCM service and registers Mewla own, once', () => {
  const input = { manifest: { $: {}, application: [{ $: {} }] } };
  const output = addMewlaLive(addMewlaLive(input));
  const services = output.manifest.application[0].service;
  expect(services.filter((s) => s.$['android:name'] === EXPO)).toEqual([
    { $: { 'android:name': EXPO, 'tools:node': 'remove' } },
  ]);
  const mewla = services.filter((s) => s.$['android:name'] === MEWLA);
  expect(mewla).toHaveLength(1);
  expect(mewla[0].$['android:exported']).toBe('false');
  expect(mewla[0]['intent-filter'][0].action[0].$['android:name']).toBe('com.google.firebase.MESSAGING_EVENT');
  expect(output.manifest.$['xmlns:tools']).toBe('http://schemas.android.com/tools');
});

test('drops an Expo service entry already present in the app manifest', () => {
  const input = {
    manifest: { $: {}, application: [{ $: {}, service: [{ $: { 'android:name': EXPO } }] }] },
  };
  const services = addMewlaLive(input).manifest.application[0].service;
  expect(services.some((s) => s.$['android:name'] === EXPO && !s.$['tools:node'])).toBe(false);
});

test('fails loudly without an application entry', () => {
  expect(() => addMewlaLive({ manifest: {} })).toThrow('application manifest entry not found');
});
