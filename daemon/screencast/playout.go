package screencast

import (
	"github.com/pion/interceptor"
	"github.com/pion/rtp"
)

// playoutDelayURI is the RTP header extension that tells the viewer how long
// it may hold a picture before showing it. A screen has no audio to keep in
// step with and its pictures come when it changes, not at a steady rate, which
// a browser's jitter buffer otherwise smooths with 100 ms and more of delay;
// min = max = 0 asks it to show each picture as soon as it is decoded.
const playoutDelayURI = "http://www.webrtc.org/experiments/rtp-hdrext/playout-delay"

type playoutDelayFactory struct{}

func (playoutDelayFactory) NewInterceptor(string) (interceptor.Interceptor, error) {
	return &playoutDelay{}, nil
}

type playoutDelay struct{ interceptor.NoOp }

func (*playoutDelay) BindLocalStream(info *interceptor.StreamInfo, writer interceptor.RTPWriter) interceptor.RTPWriter {
	id := 0
	for _, ext := range info.RTPHeaderExtensions {
		if ext.URI == playoutDelayURI {
			id = ext.ID
		}
	}
	if id == 0 {
		return writer
	}
	value, err := (&rtp.PlayoutDelayExtension{MinDelay: 0, MaxDelay: 0}).Marshal()
	if err != nil {
		return writer
	}
	return interceptor.RTPWriterFunc(func(header *rtp.Header, payload []byte, attributes interceptor.Attributes) (int, error) {
		_ = header.SetExtension(uint8(id), value)
		return writer.Write(header, payload, attributes)
	})
}
