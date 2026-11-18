// Run by TestSiteDataGuard: node testdata/sitedata_test.mjs <path to extension/sitedata.js>
import assert from 'node:assert/strict';
import { pathToFileURL } from 'node:url';

const { SiteDataGuard } = await import(pathToFileURL(process.argv[2]).href);
const now = Date.UTC(2026, 10, 18, 11, 48);
const refused = (guard, method, params) => guard.check(method, params) !== '';

// 2026-11-18 19:48 CST: a Worker's cleanup ran page.context().clearCookies()
// with no filter. Playwright sends Storage.clearCookies; it wiped every site.
let guard = new SiteDataGuard(() => now);
guard.workedOn('http://127.0.0.1:5191/chat/x');
assert.ok(refused(guard, 'Storage.clearCookies', {}), 'a wipe of every site ran');
assert.ok(refused(guard, 'Network.clearBrowserCookies', {}), 'a wipe of every site ran');
assert.match(guard.check('Storage.clearCookies', {}), /clearCookies\(\{ domain/);

// The same Worker had set a freeride.ai session cookie: removing it, as
// Playwright's clearCookies({ domain }) does (expire it), is its own business.
assert.equal(guard.check('Storage.setCookies', { cookies: [{ name: 's', value: 'v', domain: 'freeride.ai', expires: now / 1000 + 3600 }] }), '');
assert.equal(guard.check('Storage.setCookies', { cookies: [{ name: 's', value: '', domain: 'freeride.ai', expires: 0 }] }), '');
// The person's GitHub sign-in is not, whatever the filter matched.
assert.ok(refused(guard, 'Storage.setCookies', { cookies: [{ name: 'user_session', value: '', domain: '.github.com', expires: 0 }] }));
assert.ok(refused(guard, 'Storage.setCookies', { cookies: [
  { name: 's', value: '', domain: 'freeride.ai', expires: 0 },
  { name: 'user_session', value: '', domain: 'github.com', expires: 0 },
] }), 'one foreign deletion in a batch ran');
// Setting a session cookie (expires -1) or a future one is not a deletion.
assert.equal(guard.check('Network.setCookie', { name: 'a', value: 'b', domain: 'example.org', expires: -1 }), '');
assert.equal(guard.check('Network.setCookies', { cookies: [{ name: 'a', value: 'b', url: 'https://shop.example.net/', expires: now / 1000 + 60 }] }), '');

// Sites the agent opens count; a subdomain counts for its parent and back.
guard = new SiteDataGuard(() => now);
guard.check('Page.navigate', { url: 'https://app.vercel.com/login' });
assert.equal(guard.check('Network.deleteCookies', { name: 'authorization', domain: '.vercel.com' }), '');
assert.equal(guard.check('Storage.clearDataForOrigin', { origin: 'https://app.vercel.com', storageTypes: 'all' }), '');
assert.ok(refused(guard, 'Storage.clearDataForOrigin', { origin: 'https://github.com', storageTypes: 'all' }));
assert.ok(refused(guard, 'Storage.clearDataForStorageKey', { storageKey: 'https://github.com/', storageTypes: 'cookies' }));
assert.ok(refused(guard, 'Network.deleteCookies', { name: 'user_session', url: 'https://github.com/' }));
assert.ok(refused(guard, 'Network.deleteCookies', { name: 'x' }), 'a delete naming no site ran');
assert.ok(refused(guard, 'Storage.clearDataForOrigin', { origin: '*', storageTypes: 'all' }));
// A site's stored data (local storage, IndexedDB, caches, buckets, shared
// storage) follows the same rule as its cookies.
for (const [method, mine, theirs] of [
  ['DOMStorage.clear', { storageId: { securityOrigin: 'https://app.vercel.com', isLocalStorage: true } }, { storageId: { storageKey: 'https://github.com/', isLocalStorage: true } }],
  ['DOMStorage.removeDOMStorageItem', { storageId: { storageKey: 'https://vercel.com/', isLocalStorage: true }, key: 'k' }, { storageId: { securityOrigin: 'https://github.com', isLocalStorage: true }, key: 'k' }],
  ['IndexedDB.deleteDatabase', { securityOrigin: 'https://app.vercel.com', databaseName: 'd' }, { storageKey: 'https://github.com/', databaseName: 'd' }],
  ['IndexedDB.clearObjectStore', { storageBucket: { storageKey: 'https://app.vercel.com/', name: 'b' }, databaseName: 'd', objectStoreName: 's' }, { storageBucket: { storageKey: 'https://github.com/' }, databaseName: 'd', objectStoreName: 's' }],
  ['IndexedDB.deleteObjectStoreEntries', { securityOrigin: 'https://app.vercel.com', databaseName: 'd', objectStoreName: 's', keyRange: {} }, { databaseName: 'd', objectStoreName: 's', keyRange: {} }],
  ['CacheStorage.deleteCache', { cacheId: 'https://app.vercel.com/|v1' }, { cacheId: 'https://github.com/|v1' }],
  ['CacheStorage.deleteEntry', { cacheId: 'https://app.vercel.com/|v1', request: '/' }, { cacheId: 'opaque', request: '/' }],
  ['Storage.deleteStorageBucket', { bucket: { storageKey: 'https://app.vercel.com/', name: 'b' } }, { bucket: { storageKey: 'https://github.com/', name: 'b' } }],
  ['Storage.clearSharedStorageEntries', { ownerOrigin: 'https://app.vercel.com' }, { ownerOrigin: 'https://github.com' }],
  ['Storage.deleteSharedStorageEntry', { ownerOrigin: 'https://app.vercel.com', key: 'k' }, { ownerOrigin: 'https://github.com', key: 'k' }],
]) {
  assert.equal(guard.check(method, mine), '', `${method} on a site the agent works on was refused`);
  assert.ok(refused(guard, method, theirs), `${method} on another site ran`);
}
// about:blank is no site.
guard.workedOn('about:blank');
assert.ok(refused(guard, 'Storage.setCookies', { cookies: [{ name: 'n', value: '', domain: 'blank', expires: 0 }] }));
// Everything else passes untouched.
assert.equal(guard.check('Runtime.evaluate', { expression: '1' }), '');
assert.equal(guard.check('Network.clearBrowserCache', {}), '');
console.log('ok');
