package screen

import (
	"fmt"
	"io"
	"strings"
)

const (
	securityNone    = 1
	protocolVersion = "RFB 003.008\n"
)

// AcceptClient answers a viewer's RFB handshake as a 3.8 server that offers
// only security type None. The viewer is already authenticated by its device
// signature; the next bytes it sends are ClientInit.
func AcceptClient(rw io.ReadWriter) error {
	if _, err := io.WriteString(rw, protocolVersion); err != nil {
		return err
	}
	var version [12]byte
	if _, err := io.ReadFull(rw, version[:]); err != nil {
		return fmt.Errorf("read viewer version: %w", err)
	}
	if string(version[:]) != protocolVersion {
		return fmt.Errorf("viewer asked for unsupported RFB version %q", strings.TrimSpace(string(version[:])))
	}
	if _, err := rw.Write([]byte{1, securityNone}); err != nil {
		return err
	}
	var chosen [1]byte
	if _, err := io.ReadFull(rw, chosen[:]); err != nil {
		return err
	}
	if chosen[0] != securityNone {
		return fmt.Errorf("viewer chose unsupported security type %d", chosen[0])
	}
	_, err := rw.Write([]byte{0, 0, 0, 0})
	return err
}
