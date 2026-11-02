package connections

import (
	"fmt"
	"net/http"

	"golang.org/x/oauth2"
)

// Do not redirect an unknown state: another Mewla computer may own it.
// Read the address in the browser rather than reflecting an untrusted Host or
// code into HTML. No network request, history replacement, storage or logging.
func foreignOAuthReturn(w http.ResponseWriter) {
	nonce := oauth2.GenerateVerifier()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'nonce-"+nonce+"'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	w.WriteHeader(http.StatusBadRequest)
	fmt.Fprintf(w, `<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>Return to your Mewla computer</title>
<style>body{font:16px system-ui;max-width:640px;margin:48px auto;padding:0 20px;line-height:1.5}h1{font-size:24px}textarea{box-sizing:border-box;width:100%%;min-height:140px;padding:12px;font:14px monospace}button{font:inherit;padding:10px 24px;margin-top:12px}p{color:#444}</style>
<h1>This sign-in belongs to another Mewla computer</h1><p>Copy this address and paste it into the Mewla page where you started signing in. If that sign-in has ended, start again there.</p>
<label for="callback">Full callback address</label><textarea id="callback" readonly spellcheck="false"></textarea><button id="copy" type="button">Copy</button><p id="status" role="status"></p><noscript>Copy the full address from your browser's address bar.</noscript>
<script nonce="%s">const field=document.getElementById('callback');field.value=window.location.href;document.getElementById('copy').addEventListener('click',async()=>{try{await navigator.clipboard.writeText(field.value);document.getElementById('status').textContent='Copied. Return to the Mewla page where you started.';}catch{field.focus();field.select();document.getElementById('status').textContent='Select and copy the full address, then return to the Mewla page where you started.';}});</script></html>`, nonce)
}
