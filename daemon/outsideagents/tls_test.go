package outsideagents

import "crypto/tls"

func tlsConnectionState() tls.ConnectionState { return tls.ConnectionState{HandshakeComplete: true} }
