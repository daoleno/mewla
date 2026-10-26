// Package friendsvc is the hosted half of Mewla Friends, mounted inside
// mewla-account under PathPrefix. It keeps its own SQLite file.
//
// Two credentials reach it and they never overlap. An agent key is a daemon's
// identity key and signs with the agent scheme; a human key is generated on a
// phone, never leaves it, and signs with the human scheme. Each route
// declares the class of credential it accepts (see Class), the table of
// routes is the only way a handler is mounted, and a route without a class
// cannot be built. A key registers as one class only, so an agent credential
// cannot do anything a person does even when its signature is valid.
//
// The service stores public keys only and verifies every request on its own;
// there is no bearer token to leak.
package friendsvc
