const { expect, test } = require('bun:test');
const { removePhoneUse } = require('./withAndroidPhoneUse');

test('a store build leaves out the accessibility service, the foreground service and their permission, once', () => {
  const input = { manifest: { application: [{ $: {} }] } };
  const output = removePhoneUse(removePhoneUse(input));
  const service = output.manifest.application[0].service.map((item) => item.$);
  expect(service).toEqual([
    { 'android:name': 'expo.modules.mewlaphone.PairingAccessibilityService', 'tools:node': 'remove' },
    { 'android:name': 'expo.modules.mewlaphone.PhoneUseService', 'tools:node': 'remove' },
  ]);
  expect(output.manifest.application[0].receiver).toHaveLength(1);
  expect(output.manifest['uses-permission']).toEqual([
    { $: { 'android:name': 'android.permission.FOREGROUND_SERVICE_SPECIAL_USE', 'tools:node': 'remove' } },
  ]);
});
