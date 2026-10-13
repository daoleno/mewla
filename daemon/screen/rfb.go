package screen

import (
	"crypto/des"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	securityInvalid = 0
	securityNone    = 1
	securityVNCAuth = 2
	maxReasonLength = 4096
	maxNameLength   = 4096
	protocolVersion = "RFB 003.008\n"
)

// clientHandshake signs in to the RFB server as a client (RFC 6143 §7.1-7.2).
// Apple's "003.889" server follows the 3.8 flow when the client answers 3.8.
func clientHandshake(rw io.ReadWriter, password string) error {
	var version [12]byte
	if _, err := io.ReadFull(rw, version[:]); err != nil {
		return fmt.Errorf("read RFB version: %w", err)
	}
	var major, minor int
	if _, err := fmt.Sscanf(string(version[:]), "RFB %03d.%03d\n", &major, &minor); err != nil || major != 3 {
		return fmt.Errorf("%s did not answer as an RFB server", serverName())
	}
	reply := protocolVersion
	switch {
	case minor >= 8:
		minor = 8
	case minor == 7:
		reply = "RFB 003.007\n"
	default:
		minor = 3
		reply = "RFB 003.003\n"
	}
	if _, err := io.WriteString(rw, reply); err != nil {
		return err
	}
	var chosen byte
	if minor == 3 {
		var kind uint32
		if err := binary.Read(rw, binary.BigEndian, &kind); err != nil {
			return err
		}
		if kind == securityInvalid {
			return refusal(rw)
		}
		if kind != securityNone && kind != securityVNCAuth {
			return unsupportedSecurity([]byte{byte(kind)})
		}
		chosen = byte(kind)
	} else {
		var count [1]byte
		if _, err := io.ReadFull(rw, count[:]); err != nil {
			return err
		}
		if count[0] == 0 {
			return refusal(rw)
		}
		offered := make([]byte, count[0])
		if _, err := io.ReadFull(rw, offered); err != nil {
			return err
		}
		chosen = pickSecurity(offered, password != "")
		if chosen == securityInvalid {
			return unsupportedSecurity(offered)
		}
		if _, err := rw.Write([]byte{chosen}); err != nil {
			return err
		}
	}
	if chosen == securityVNCAuth {
		if password == "" {
			return errors.New("Screen Sharing asks for a VNC password. Run `mewla screen enable` on this computer and enter it")
		}
		var challenge [16]byte
		if _, err := io.ReadFull(rw, challenge[:]); err != nil {
			return err
		}
		response, err := vncAuthResponse(password, challenge)
		if err != nil {
			return err
		}
		if _, err := rw.Write(response[:]); err != nil {
			return err
		}
	}
	// 3.3 and 3.7 send no SecurityResult after None.
	if chosen == securityNone && minor < 8 {
		return nil
	}
	var result uint32
	if err := binary.Read(rw, binary.BigEndian, &result); err != nil {
		return fmt.Errorf("read sign-in result: %w", err)
	}
	if result == 0 {
		return nil
	}
	if minor >= 8 {
		if reason, err := readString(rw, maxReasonLength); err == nil && reason != "" {
			return fmt.Errorf("Screen Sharing refused the sign-in: %s. Check the VNC password with `mewla screen enable`", reason)
		}
	}
	return errors.New("Screen Sharing refused the VNC password. Run `mewla screen enable` again with the password set in Screen Sharing → Computer Settings")
}

// pickSecurity prefers the VNC password when one is stored, then no sign-in.
func pickSecurity(offered []byte, havePassword bool) byte {
	has := func(kind byte) bool { return strings.IndexByte(string(offered), kind) >= 0 }
	switch {
	case has(securityVNCAuth) && (havePassword || !has(securityNone)):
		return securityVNCAuth
	case has(securityNone):
		return securityNone
	}
	return securityInvalid
}

func unsupportedSecurity(offered []byte) error {
	kinds := make([]string, len(offered))
	for i, kind := range offered {
		kinds[i] = fmt.Sprint(kind)
	}
	return fmt.Errorf("Screen Sharing offers no sign-in Mewla can use (types %s). Turn on \"VNC viewers may control screen with password\" in Screen Sharing → Computer Settings", strings.Join(kinds, ","))
}

func refusal(r io.Reader) error {
	reason, err := readString(r, maxReasonLength)
	if err != nil || reason == "" {
		return errors.New("Screen Sharing refused the connection")
	}
	return fmt.Errorf("Screen Sharing refused the connection: %s", reason)
}

func readString(r io.Reader, limit uint32) (string, error) {
	var length uint32
	if err := binary.Read(r, binary.BigEndian, &length); err != nil {
		return "", err
	}
	if length > limit {
		return "", fmt.Errorf("RFB string of %d bytes exceeds %d", length, limit)
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", err
	}
	return string(buf), nil
}

// vncAuthResponse is DES-ECB of the challenge keyed by the password with each
// key byte's bits mirrored, as every VNC server expects.
func vncAuthResponse(password string, challenge [16]byte) ([16]byte, error) {
	var key [8]byte
	copy(key[:], password)
	for i, b := range key {
		var mirrored byte
		for bit := range 8 {
			if b&(1<<bit) != 0 {
				mirrored |= 0x80 >> bit
			}
		}
		key[i] = mirrored
	}
	block, err := des.NewCipher(key[:])
	if err != nil {
		return [16]byte{}, err
	}
	var out [16]byte
	block.Encrypt(out[:8], challenge[:8])
	block.Encrypt(out[8:], challenge[8:])
	return out, nil
}

type serverInit struct {
	width, height int
	name          string
}

// readServerInit reads ServerInit (§7.3.2): size, pixel format and name.
func readServerInit(r io.Reader) (serverInit, error) {
	var head [20]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return serverInit{}, fmt.Errorf("read screen size: %w", err)
	}
	name, err := readString(r, maxNameLength)
	if err != nil {
		return serverInit{}, err
	}
	return serverInit{
		width:  int(binary.BigEndian.Uint16(head[0:2])),
		height: int(binary.BigEndian.Uint16(head[2:4])),
		name:   name,
	}, nil
}

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
