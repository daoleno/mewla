// The person's sign-ins for every site live in the browser agents share, so
// an agent may remove cookies and site data only of the sites it works on:
// pages it opened or was given, and cookies it set itself. Wiping every site
// at once (Playwright's context.clearCookies() without a filter) is refused.

const WIPE_EVERY_SITE = new Set(['Storage.clearCookies', 'Network.clearBrowserCookies']);

export const refusalHint =
  "The person's sign-ins for every site are in this shared browser. Remove only a site you work on: " +
  "page.context().clearCookies({ domain: 'example.com' }), or localStorage.clear() on its page. " +
  'For a first visit or signed-out check, clear just that site this way.';

export function hostOf(value) {
  if (typeof value !== 'string' || value === '')
    return '';
  try {
    const url = new URL(value);
    if (!/^(https?|wss?):$/.test(url.protocol))
      return '';
    return url.hostname.toLowerCase();
  } catch {
    return '';
  }
}

function cookieHost(cookie) {
  if (!cookie || typeof cookie !== 'object')
    return '';
  if (typeof cookie.domain === 'string' && cookie.domain !== '')
    return cookie.domain.replace(/^\./, '').toLowerCase();
  return hostOf(cookie.url);
}

// related says whether a cookie for one host can affect a site on the other:
// the same host, or one a subdomain of the other.
function related(a, b) {
  return a === b || a.endsWith('.' + b) || b.endsWith('.' + a);
}

export class SiteDataGuard {
  constructor(now = () => Date.now()) {
    this._now = now;
    this.sites = new Set();
  }

  workedOn(url) {
    const host = hostOf(url);
    if (host)
      this.sites.add(host);
  }

  _worked(host) {
    if (!host)
      return false;
    for (const site of this.sites) {
      if (related(host, site))
        return true;
    }
    return false;
  }

  _deletes(cookie) {
    const expires = cookie?.expires;
    return typeof expires === 'number' && expires !== -1 && expires * 1000 <= this._now();
  }

  // check says why a CDP command an agent sends must not run, or '' when it
  // may. What it reads of a command that runs (a page it opens, a cookie it
  // sets) counts as a site the agent works on.
  check(method, params = {}) {
    if (WIPE_EVERY_SITE.has(method))
      return `Mewla refused ${method}: it signs the person out of every site. ${refusalHint}`;
    switch (method) {
      case 'Page.navigate':
        this.workedOn(params.url);
        return '';
      case 'Storage.setCookies':
      case 'Network.setCookies':
      case 'Network.setCookie': {
        const cookies = method === 'Network.setCookie' ? [params] : (params.cookies ?? []);
        for (const cookie of cookies) {
          if (this._deletes(cookie) && !this._worked(cookieHost(cookie)))
            return `Mewla refused deleting ${cookieHost(cookie) || 'a site'}'s cookies: this agent has not worked on that site. ${refusalHint}`;
        }
        for (const cookie of cookies) {
          if (!this._deletes(cookie)) {
            const host = cookieHost(cookie);
            if (host)
              this.sites.add(host);
          }
        }
        return '';
      }
      case 'Network.deleteCookies': {
        const host = params.domain ? String(params.domain).replace(/^\./, '').toLowerCase() : hostOf(params.url);
        if (!this._worked(host))
          return `Mewla refused deleting ${host || 'every site'}'s cookies: this agent has not worked on that site. ${refusalHint}`;
        return '';
      }
      case 'Storage.clearDataForOrigin':
      case 'Storage.clearDataForStorageKey': {
        const host = hostOf(method === 'Storage.clearDataForOrigin' ? params.origin : params.storageKey);
        if (!this._worked(host))
          return `Mewla refused clearing ${host || 'every site'}'s data: this agent has not worked on that site. ${refusalHint}`;
        return '';
      }
    }
    return '';
  }
}
