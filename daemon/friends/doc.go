// Package friends is the daemon's half of Mewla Friends: the scrubber every
// draft passes before it can leave the Brain, the agent link to the hosted
// service, and the local draft files.
//
// The scrubber is deterministic and refuses rather than rewrites: a draft that
// trips a rule goes back to the Brain with the reason. Its strength is the
// private vocabulary of this computer, which the hosted service cannot know.
// Its limits are listed on Limits; people's names, clients that are not in
// the vocabulary and anything private by meaning get through, which is why a
// person reads every draft unless they turned auto-post on knowingly.
package friends
