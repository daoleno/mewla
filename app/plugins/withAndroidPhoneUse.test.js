const { expect, test } = require('bun:test');
const { allowPictureInPicture, removePhoneUse } = require('./withAndroidPhoneUse');

test('a store build leaves out the accessibility service, the foreground service and their permissions, once', () => {
  const input = { manifest: { application: [{ $: {} }] } };
  const output = removePhoneUse(removePhoneUse(input));
  const service = output.manifest.application[0].service.map((item) => item.$);
  expect(service).toEqual([
    { 'android:name': 'expo.modules.mewlaphone.PairingAccessibilityService', 'tools:node': 'remove' },
    { 'android:name': 'expo.modules.mewlaphone.PhoneUseService', 'tools:node': 'remove' },
    { 'android:name': 'expo.modules.mewlaphone.PhoneLinkTaskService', 'tools:node': 'remove' },
  ]);
  expect(output.manifest.application[0].receiver.map((item) => item.$['android:name'])).toEqual([
    'expo.modules.mewlaphone.PhoneActionReceiver',
    'expo.modules.mewlaphone.PhoneBootReceiver',
  ]);
  expect(output.manifest['uses-permission']).toEqual([
    { $: { 'android:name': 'android.permission.FOREGROUND_SERVICE_SPECIAL_USE', 'tools:node': 'remove' } },
    { $: { 'android:name': 'android.permission.WRITE_SECURE_SETTINGS', 'tools:node': 'remove' } },
  ]);
});

test('the GitHub APK lets the main activity float the agent screen', () => {
  const input = { manifest: { application: [{ activity: [{ $: { 'android:name': '.MainActivity' } }] }] } };
  const output = allowPictureInPicture(input);
  expect(output.manifest.application[0].activity[0].$['android:supportsPictureInPicture']).toBe('true');
});
