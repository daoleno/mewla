package agentuse

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
)

// Chrome's native messaging frames each message with its length as four bytes
// in the machine's byte order (little-endian on every Mac and PC Chrome runs
// on), then the JSON. Chrome allows 1 MB from the host and 64 MiB to it.
const maxNativeMessage = 1 << 20

func writeNative(w io.Writer, raw []byte) error {
	if len(raw) > maxNativeMessage {
		return errors.New("message too large for Chrome")
	}
	var size [4]byte
	binary.LittleEndian.PutUint32(size[:], uint32(len(raw)))
	if _, err := w.Write(size[:]); err != nil {
		return err
	}
	_, err := w.Write(raw)
	return err
}

func readNative(r io.Reader) ([]byte, error) {
	var size [4]byte
	if _, err := io.ReadFull(r, size[:]); err != nil {
		return nil, err
	}
	n := binary.LittleEndian.Uint32(size[:])
	if n > 64<<20 {
		return nil, fmt.Errorf("message of %d bytes", n)
	}
	raw := make([]byte, n)
	_, err := io.ReadFull(r, raw)
	return raw, err
}

// RunNativeHost is what Chrome runs for the extension: it joins Chrome's
// stdio to the daemon's bridge until either side ends. in and out are the
// process's stdin and stdout.
func RunNativeHost(stateDir string, in io.Reader, out io.Writer) error {
	conn, err := DialBridgeHost(stateDir + "/agent-use")
	if err != nil {
		return err
	}
	return joinNative(conn, in, out)
}

func joinNative(conn net.Conn, in io.Reader, out io.Writer) error {
	defer conn.Close()
	var once sync.Once
	var first error
	done := make(chan struct{})
	finish := func(err error) {
		once.Do(func() { first = err; close(done) })
	}
	// Chrome -> daemon: unframe, one JSON message per line.
	go func() {
		r := bufio.NewReader(in)
		for {
			raw, err := readNative(r)
			if err != nil {
				finish(err)
				return
			}
			if !json.Valid(raw) {
				continue
			}
			if _, err := conn.Write(append(raw, '\n')); err != nil {
				finish(err)
				return
			}
		}
	}()
	// Daemon -> Chrome: one JSON message per line, framed.
	go func() {
		dec := json.NewDecoder(conn)
		for {
			var raw json.RawMessage
			if err := dec.Decode(&raw); err != nil {
				finish(err)
				return
			}
			if err := writeNative(out, raw); err != nil {
				finish(err)
				return
			}
		}
	}()
	<-done
	if errors.Is(first, io.EOF) {
		return nil
	}
	return first
}
