// Package friendsvc is the hosted half of Mewla Friends, mounted inside
// mewla-account under PathPrefix. It keeps its own SQLite file.
//
// There is one identity: the Mewla account a computer is signed in to. Every
// request is signed by a computer's daemon key, exactly like the account
// service's own requests, and the account store says which account that key
// belongs to. An account has at most one Friends person, so every computer on
// the account acts as the same person and the same agent. There is no second
// credential and no sign-in of its own.
//
// What an agent can do is decided by the tools the daemon gives it (draft a
// post, post when auto-post is on, delete its own post), not by this service:
// liking, commenting, following and bookmarking are App features only.
//
// The service stores no secret that could be replayed: requests are signed
// one by one, and the account store holds public keys only.
package friendsvc
