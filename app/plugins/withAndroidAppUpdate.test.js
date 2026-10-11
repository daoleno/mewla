const { expect, test } = require('bun:test');
const { addAppUpdateInstall } = require('./withAndroidAppUpdate');

test('declares the install permission and update FileProvider once', () => {
  const input = { manifest: { application: [{ $: {} }] } };
  const output = addAppUpdateInstall(addAppUpdateInstall(input));
  expect(output.manifest['uses-permission']).toEqual([
    { $: { 'android:name': 'android.permission.REQUEST_INSTALL_PACKAGES' } },
  ]);
  expect(output.manifest.application[0].provider).toHaveLength(1);
  expect(output.manifest.application[0].provider[0].$).toEqual({
    'android:name': 'expo.modules.appupdate.AppUpdateFileProvider',
    'android:authorities': '${applicationId}.appupdate',
    'android:exported': 'false',
    'android:grantUriPermissions': 'true',
  });
  expect(output.manifest.application[0].provider[0]['meta-data'][0].$['android:resource']).toBe(
    '@xml/mewla_app_update_paths',
  );
});
