package translate

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// sseEvent is one server-sent event: its event name (may be empty) and its
// data lines joined.
type sseEvent struct {
	Name string
	Data []byte
}

// sseReader reads server-sent events from a stream.
type sseReader struct {
	r *bufio.Reader
}

func newSSEReader(r io.Reader) *sseReader {
	return &sseReader{r: bufio.NewReaderSize(r, 64<<10)}
}

// next returns the next event, or io.EOF at the end of the stream. Comments
// and events without data are skipped.
func (s *sseReader) next() (sseEvent, error) {
	var ev sseEvent
	var data bytes.Buffer
	hasData := false
	for {
		line, err := s.r.ReadBytes('\n')
		if len(line) == 0 && err != nil {
			if hasData {
				ev.Data = data.Bytes()
				return ev, nil
			}
			return sseEvent{}, err
		}
		line = bytes.TrimRight(line, "\r\n")
		if len(line) == 0 {
			if hasData {
				ev.Data = data.Bytes()
				return ev, nil
			}
			ev = sseEvent{}
			continue
		}
		if line[0] == ':' {
			continue
		}
		field, value, _ := bytes.Cut(line, []byte(":"))
		value = bytes.TrimPrefix(value, []byte(" "))
		switch string(field) {
		case "event":
			ev.Name = string(value)
		case "data":
			if hasData {
				data.WriteByte('\n')
			}
			data.Write(value)
			hasData = true
		}
		if err != nil {
			if hasData {
				ev.Data = data.Bytes()
				return ev, nil
			}
			return sseEvent{}, err
		}
	}
}

// sseWriter writes server-sent events and flushes each.
type sseWriter struct {
	w     io.Writer
	flush func()
	err   error
}

func newSSEWriter(w http.ResponseWriter) *sseWriter {
	s := &sseWriter{w: w, flush: func() {}}
	if f, ok := w.(http.Flusher); ok {
		s.flush = f.Flush
	}
	return s
}

// event writes one named event whose data is v as JSON.
func (s *sseWriter) event(name string, v any) {
	if s.err != nil {
		return
	}
	data, err := json.Marshal(v)
	if err != nil {
		s.err = err
		return
	}
	if name != "" {
		_, s.err = fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", name, data)
	} else {
		_, s.err = fmt.Fprintf(s.w, "data: %s\n\n", data)
	}
	s.flush()
}

// raw writes a data line as it is (Chat's "[DONE]").
func (s *sseWriter) raw(data string) {
	if s.err != nil {
		return
	}
	_, s.err = fmt.Fprintf(s.w, "data: %s\n\n", data)
	s.flush()
}

// comment writes a comment line, which keeps an idle stream open.
func (s *sseWriter) comment() {
	if s.err != nil {
		return
	}
	_, s.err = io.WriteString(s.w, ": keep-alive\n\n")
	s.flush()
}
