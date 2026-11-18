//go:build windows && !screenfake

package screen

import (
	"errors"
	"fmt"
	"image"
	"runtime"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The Windows H.264 encoder is Media Foundation's: the Microsoft H.264
// Encoder MFT that Windows itself ships (not on N editions without the Media
// Feature Pack), reached through COM like UI Automation, so the build stays
// pure Go. It runs synchronously in low-latency mode, constrained baseline,
// no B-frames: one picture in, that picture out. It takes NV12, which this
// file makes from the captured RGBA (BT.601, limited range, as ffmpeg does).
//
// Slot numbers and GUIDs are from the Windows SDK headers (mingw-w64
// mftransform.h, mfobjects.h, icodecapi.h, mfapi.h, codecapi.h).

func init() { defaultVideo, defaultVideoName = newMFEncoder, "Media Foundation" }

var (
	mfplat                = windows.NewLazySystemDLL("mfplat.dll")
	procMFStartup         = mfplat.NewProc("MFStartup")
	procMFCreateMediaType = mfplat.NewProc("MFCreateMediaType")
	procMFCreateSample    = mfplat.NewProc("MFCreateSample")
	procMFCreateMemBuffer = mfplat.NewProc("MFCreateMemoryBuffer")

	clsidMSH264EncoderMFT = windows.GUID{Data1: 0x6ca50344, Data2: 0x051a, Data3: 0x4ded, Data4: [8]byte{0x97, 0x79, 0xa4, 0x33, 0x05, 0x16, 0x5e, 0x35}}
	iidIMFTransform       = windows.GUID{Data1: 0xbf94c121, Data2: 0x5b05, Data3: 0x4e6f, Data4: [8]byte{0x80, 0x00, 0xba, 0x59, 0x89, 0x61, 0x41, 0x4d}}
	iidICodecAPI          = windows.GUID{Data1: 0x901db4c7, Data2: 0x31ce, Data3: 0x41a2, Data4: [8]byte{0x85, 0xdc, 0x8f, 0xa0, 0xbf, 0x41, 0xb8, 0xda}}

	mfMediaTypeVideo      = mediaSubtype(0x73646976) // 'vids'
	mfVideoFormatH264     = mediaSubtype(0x34363248) // 'H264'
	mfVideoFormatNV12     = mediaSubtype(0x3231564e) // 'NV12'
	mfMTMajorType         = windows.GUID{Data1: 0x48eba18e, Data2: 0xf8c9, Data3: 0x4687, Data4: [8]byte{0xbf, 0x11, 0x0a, 0x74, 0xc9, 0xf9, 0x6a, 0x8f}}
	mfMTSubtype           = windows.GUID{Data1: 0xf7e34c9a, Data2: 0x42e8, Data3: 0x4714, Data4: [8]byte{0xb7, 0x4b, 0xcb, 0x29, 0xd7, 0x2c, 0x35, 0xe5}}
	mfMTAvgBitrate        = windows.GUID{Data1: 0x20332624, Data2: 0xfb0d, Data3: 0x4d9e, Data4: [8]byte{0xbd, 0x0d, 0xcb, 0xf6, 0x78, 0x6c, 0x10, 0x2e}}
	mfMTFrameSize         = windows.GUID{Data1: 0x1652c33d, Data2: 0xd6b2, Data3: 0x4012, Data4: [8]byte{0xb8, 0x34, 0x72, 0x03, 0x08, 0x49, 0xa3, 0x7d}}
	mfMTFrameRate         = windows.GUID{Data1: 0xc459a2e8, Data2: 0x3d2c, Data3: 0x4e44, Data4: [8]byte{0xb1, 0x32, 0xfe, 0xe5, 0x15, 0x6c, 0x7b, 0xb0}}
	mfMTInterlaceMode     = windows.GUID{Data1: 0xe2724bb8, Data2: 0xe676, Data3: 0x4806, Data4: [8]byte{0xb4, 0xb2, 0xa8, 0xd6, 0xef, 0xb4, 0x4c, 0xcd}}
	mfMTPixelAspectRatio  = windows.GUID{Data1: 0xc6376a1e, Data2: 0x8d0a, Data3: 0x4027, Data4: [8]byte{0xbe, 0x45, 0x6d, 0x9a, 0x0a, 0xd3, 0x9b, 0xb6}}
	mfMTMPEG2Profile      = windows.GUID{Data1: 0xad76a80b, Data2: 0x2d5c, Data3: 0x4e0b, Data4: [8]byte{0xb3, 0x75, 0x64, 0xe5, 0x20, 0x13, 0x70, 0x36}}
	mfMTSequenceHeader    = windows.GUID{Data1: 0x3c036de7, Data2: 0x3ad0, Data3: 0x4c9e, Data4: [8]byte{0x92, 0x16, 0xee, 0x6d, 0x6a, 0xc2, 0x1c, 0xb3}}
	mfLowLatency          = windows.GUID{Data1: 0x9c27891a, Data2: 0xed7a, Data3: 0x40e1, Data4: [8]byte{0x88, 0xe8, 0xb2, 0x27, 0x27, 0xa0, 0x24, 0xee}}
	codecapiLowLatency    = mfLowLatency // CODECAPI_AVLowLatencyMode is the same GUID
	codecapiForceKey      = windows.GUID{Data1: 0x398c1b98, Data2: 0x8353, Data3: 0x475a, Data4: [8]byte{0x9e, 0xf2, 0x8f, 0x26, 0x5d, 0x26, 0x03, 0x45}}
	codecapiMeanBitRate   = windows.GUID{Data1: 0xf7222374, Data2: 0x2144, Data3: 0x4815, Data4: [8]byte{0xb5, 0x50, 0xa3, 0x7f, 0x8e, 0x12, 0xee, 0x52}}
	codecapiRateControl   = windows.GUID{Data1: 0x1c0608e9, Data2: 0x370c, Data3: 0x4710, Data4: [8]byte{0x8a, 0x58, 0xcb, 0x61, 0x81, 0xc4, 0x24, 0x23}}
	codecapiBPictureCount = windows.GUID{Data1: 0x8d390aac, Data2: 0xdc5c, Data3: 0x4200, Data4: [8]byte{0xb5, 0x7f, 0x81, 0x4d, 0x04, 0xba, 0xba, 0xb2}}
	codecapiGOPSize       = windows.GUID{Data1: 0x95f31b26, Data2: 0x95a4, Data3: 0x41aa, Data4: [8]byte{0x93, 0x03, 0x24, 0x6a, 0x7f, 0xc6, 0xee, 0xf1}}
)

// mediaSubtype is a FOURCC's media subtype GUID (DEFINE_MEDIATYPE_GUID).
func mediaSubtype(fourcc uint32) windows.GUID {
	return windows.GUID{Data1: fourcc, Data2: 0x0000, Data3: 0x0010, Data4: [8]byte{0x80, 0x00, 0x00, 0xaa, 0x00, 0x38, 0x9b, 0x71}}
}

// COM slots.
const (
	slotQueryInterface = 0

	// IMFAttributes (and IMFMediaType, IMFSample, which extend it).
	attrGetUINT32 = 7
	attrGetBlobSz = 14
	attrGetBlob   = 15
	attrSetUINT32 = 21
	attrSetUINT64 = 22
	attrSetGUID   = 24

	// IMFSample.
	sampleSetTime     = 36
	sampleSetDuration = 38
	sampleGetBuffer   = 40
	sampleAddBuffer   = 42

	// IMFMediaBuffer.
	bufferLock      = 3
	bufferUnlock    = 4
	bufferGetLength = 5
	bufferSetLength = 6

	// IMFTransform.
	mftGetOutputStreamInfo = 7
	mftGetAttributes       = 8
	mftSetInputType        = 15
	mftSetOutputType       = 16
	mftGetOutputType       = 18
	mftProcessMessage      = 23
	mftProcessInput        = 24
	mftProcessOutput       = 25

	// ICodecAPI.
	codecSetValue = 9
)

const (
	mfVersion                  = 0x00020070 // MF_SDK_VERSION << 16 | MF_API_VERSION
	mfStartupLite              = 1          // MFSTARTUP_NOSOCKET
	mftMessageBeginStreaming   = 0x10000000
	mftMessageStartOfStream    = 0x10000003
	mftOutputProvidesSamples   = 0x100
	mfVideoInterlaceProgress   = 2
	avEncH264ProfileBase       = 66
	avEncH264ProfileCBase      = 256
	avEncRateControlCBR        = 0
	mfENeedMoreInput           = 0xC00D6D72
	mfEStreamChange            = 0xC00D6D61
	vtUI4                      = 19
	mfEncodeTimeout            = 3 * time.Second
	mfKeyInterval              = 900 // pictures: a key frame at least every 30 s
	hundredNanosecondsPerMilli = 10_000
)

type mftOutputStreamInfo struct {
	Flags, Size, Alignment uint32
}

type mftOutputDataBuffer struct {
	StreamID uint32
	Sample   *comObject
	Status   uint32
	Events   *comObject
}

var mfStart struct {
	once sync.Once
	err  error
}

// mfEncoder owns one MFT on its own COM thread.
type mfEncoder struct {
	width, height int
	kbps          int
	calls         chan func()
	done          chan struct{}
	closeOnce     sync.Once

	// Owned by the COM thread.
	mft       *comObject
	codec     *comObject
	outSize   uint32
	header    []byte // SPS and PPS, Annex B, when not in the stream
	started   time.Time
	pictures  int64
	nv12      []byte
	keyNeeded bool
}

func newMFEncoder(width, height, kbps int) (videoEncoder, error) {
	if width <= 0 || height <= 0 || width%2 != 0 || height%2 != 0 {
		return nil, fmt.Errorf("the encoder needs an even size, not %dx%d", width, height)
	}
	e := &mfEncoder{width: width, height: height, kbps: kbps, calls: make(chan func()), done: make(chan struct{})}
	ready := make(chan error, 1)
	go e.thread(ready)
	if err := <-ready; err != nil {
		return nil, err
	}
	return e, nil
}

// thread runs every Media Foundation call for this encoder.
func (e *mfEncoder) thread(ready chan<- error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := windows.CoInitializeEx(0, coinitMultithreaded); err != nil {
		ready <- fmt.Errorf("COM: %w", err)
		return
	}
	defer windows.CoUninitialize()
	if err := e.open(); err != nil {
		e.release()
		ready <- err
		return
	}
	ready <- nil
	for {
		select {
		case f := <-e.calls:
			f()
		case <-e.done:
			e.release()
			return
		}
	}
}

func (e *mfEncoder) do(f func()) error {
	finished := make(chan struct{})
	select {
	case e.calls <- func() { f(); close(finished) }:
	case <-e.done:
		return errors.New("the encoder is closed")
	}
	<-finished
	return nil
}

func (e *mfEncoder) open() error {
	mfStart.once.Do(func() {
		if r, _, _ := procMFStartup.Call(mfVersion, mfStartupLite); int32(r) < 0 {
			mfStart.err = fmt.Errorf("Media Foundation did not start (%v)", hresult(uint32(r)))
		}
	})
	if mfStart.err != nil {
		return mfStart.err
	}
	var mft *comObject
	if r, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidMSH264EncoderMFT)), 0, clsctxInprocServer, uintptr(unsafe.Pointer(&iidIMFTransform)), uintptr(unsafe.Pointer(&mft))); int32(r) < 0 || mft == nil {
		return fmt.Errorf("this Windows has no H.264 encoder (%v); an N edition needs the Media Feature Pack", hresult(uint32(r)))
	}
	e.mft = mft

	// Low latency: one picture in, that picture out.
	var attrs *comObject
	if err := e.mft.call(mftGetAttributes, uintptr(unsafe.Pointer(&attrs))); err == nil && attrs != nil {
		_ = attrs.call(attrSetUINT32, uintptr(unsafe.Pointer(&mfLowLatency)), 1)
		attrs.release()
	}
	var codec *comObject
	if err := e.mft.call(slotQueryInterface, uintptr(unsafe.Pointer(&iidICodecAPI)), uintptr(unsafe.Pointer(&codec))); err == nil && codec != nil {
		e.codec = codec
		e.setCodec(codecapiLowLatency, vtBool, 0xFFFF) // VARIANT_TRUE
		e.setCodec(codecapiRateControl, vtUI4, avEncRateControlCBR)
		e.setCodec(codecapiBPictureCount, vtUI4, 0)
		e.setCodec(codecapiGOPSize, vtUI4, mfKeyInterval)
		e.setCodec(codecapiMeanBitRate, vtUI4, uint64(e.kbps)*1000)
	}

	// The output type first, then the input: the encoder's order.
	var err error
	for _, profile := range []uint32{avEncH264ProfileCBase, avEncH264ProfileBase} {
		if err = e.setType(mftSetOutputType, mfVideoFormatH264, profile); err == nil {
			break
		}
	}
	if err != nil {
		return fmt.Errorf("the H.264 encoder took no baseline output (%w)", err)
	}
	if err := e.setType(mftSetInputType, mfVideoFormatNV12, 0); err != nil {
		return fmt.Errorf("the H.264 encoder took no NV12 input (%w)", err)
	}
	e.readHeader()

	var info mftOutputStreamInfo
	if err := e.mft.call(mftGetOutputStreamInfo, 0, uintptr(unsafe.Pointer(&info))); err != nil {
		return err
	}
	if info.Flags&mftOutputProvidesSamples != 0 {
		return errors.New("the H.264 encoder wants to provide its own samples")
	}
	e.outSize = max(info.Size, uint32(e.width*e.height*3/2))
	_ = e.mft.call(mftProcessMessage, mftMessageBeginStreaming, 0)
	_ = e.mft.call(mftProcessMessage, mftMessageStartOfStream, 0)
	e.nv12 = make([]byte, e.width*e.height*3/2)
	e.started = time.Now()
	e.keyNeeded = true
	return nil
}

func (e *mfEncoder) setCodec(key windows.GUID, vt uint16, value uint64) bool {
	if e.codec == nil {
		return false
	}
	v := variant{vt: vt, val: value}
	return e.codec.call(codecSetValue, uintptr(unsafe.Pointer(&key)), uintptr(unsafe.Pointer(&v))) == nil
}

func (e *mfEncoder) setType(slot int, subtype windows.GUID, profile uint32) error {
	var t *comObject
	if r, _, _ := procMFCreateMediaType.Call(uintptr(unsafe.Pointer(&t))); int32(r) < 0 || t == nil {
		return hresult(uint32(r))
	}
	defer t.release()
	size := uint64(e.width)<<32 | uint64(e.height)
	rate := uint64(videoFPS)<<32 | 1
	for _, err := range []error{
		t.call(attrSetGUID, uintptr(unsafe.Pointer(&mfMTMajorType)), uintptr(unsafe.Pointer(&mfMediaTypeVideo))),
		t.call(attrSetGUID, uintptr(unsafe.Pointer(&mfMTSubtype)), uintptr(unsafe.Pointer(&subtype))),
		t.call(attrSetUINT64, uintptr(unsafe.Pointer(&mfMTFrameSize)), uintptr(size)),
		t.call(attrSetUINT64, uintptr(unsafe.Pointer(&mfMTFrameRate)), uintptr(rate)),
		t.call(attrSetUINT64, uintptr(unsafe.Pointer(&mfMTPixelAspectRatio)), uintptr(uint64(1)<<32|1)),
		t.call(attrSetUINT32, uintptr(unsafe.Pointer(&mfMTInterlaceMode)), mfVideoInterlaceProgress),
	} {
		if err != nil {
			return err
		}
	}
	if profile != 0 {
		if err := t.call(attrSetUINT32, uintptr(unsafe.Pointer(&mfMTAvgBitrate)), uintptr(e.kbps*1000)); err != nil {
			return err
		}
		if err := t.call(attrSetUINT32, uintptr(unsafe.Pointer(&mfMTMPEG2Profile)), uintptr(profile)); err != nil {
			return err
		}
	}
	return e.mft.call(slot, 0, uintptr(unsafe.Pointer(t)), 0)
}

// readHeader keeps the SPS and PPS the output type carries, for key frames
// that come without them.
func (e *mfEncoder) readHeader() {
	var t *comObject
	if err := e.mft.call(mftGetOutputType, 0, uintptr(unsafe.Pointer(&t))); err != nil || t == nil {
		return
	}
	defer t.release()
	var size uint32
	if err := t.call(attrGetBlobSz, uintptr(unsafe.Pointer(&mfMTSequenceHeader)), uintptr(unsafe.Pointer(&size))); err != nil || size == 0 {
		return
	}
	blob := make([]byte, size)
	if err := t.call(attrGetBlob, uintptr(unsafe.Pointer(&mfMTSequenceHeader)), uintptr(unsafe.Pointer(&blob[0])), uintptr(size), uintptr(unsafe.Pointer(&size))); err != nil {
		return
	}
	e.header = blob[:size]
}

func (e *mfEncoder) Encode(img *image.RGBA, key bool) (data []byte, isKey bool, err error) {
	if img.Rect.Dx() != e.width || img.Rect.Dy() != e.height {
		return nil, false, fmt.Errorf("frame is %dx%d, the encoder %dx%d", img.Rect.Dx(), img.Rect.Dy(), e.width, e.height)
	}
	type result struct {
		data []byte
		key  bool
		err  error
	}
	got := make(chan result, 1)
	select {
	case e.calls <- func() { d, k, err := e.encode(img, key); got <- result{d, k, err} }:
	case <-e.done:
		return nil, false, errors.New("the encoder is closed")
	}
	select {
	case r := <-got:
		return r.data, r.key, r.err
	case <-time.After(mfEncodeTimeout):
		e.Close() // a hung encoder is not used again
		return nil, false, errors.New("the H.264 encoder did not answer")
	}
}

func (e *mfEncoder) encode(img *image.RGBA, key bool) ([]byte, bool, error) {
	rgbaToNV12(e.nv12, img)
	if key || e.keyNeeded {
		e.setCodec(codecapiForceKey, vtUI4, 1)
		e.keyNeeded = false
	}
	in, err := e.sample(e.nv12)
	if err != nil {
		return nil, false, err
	}
	at := time.Since(e.started).Milliseconds() * hundredNanosecondsPerMilli
	_ = in.call(sampleSetTime, uintptr(at))
	_ = in.call(sampleSetDuration, uintptr(1000/videoFPS*hundredNanosecondsPerMilli))
	err = e.mft.call(mftProcessInput, 0, uintptr(unsafe.Pointer(in)), 0)
	in.release()
	if err != nil {
		return nil, false, fmt.Errorf("the encoder took no picture (%w)", err)
	}
	e.pictures++

	var out []byte
	for {
		au, err := e.output()
		if errors.Is(err, hresult(mfENeedMoreInput)) {
			break
		}
		if errors.Is(err, hresult(mfEStreamChange)) {
			e.readHeader()
			continue
		}
		if err != nil {
			return nil, false, err
		}
		out = append(out, au...)
	}
	if len(out) == 0 {
		return nil, false, errVideoDropped
	}
	isKey := annexBHasNAL(out, 5)
	if isKey && !annexBHasNAL(out, 7) && len(e.header) > 0 {
		out = append(append([]byte(nil), e.header...), out...)
	}
	return out, isKey, nil
}

// output takes one picture out of the encoder.
func (e *mfEncoder) output() ([]byte, error) {
	sample, err := e.sample(nil)
	if err != nil {
		return nil, err
	}
	defer sample.release()
	buf := mftOutputDataBuffer{Sample: sample}
	var status uint32
	err = e.mft.call(mftProcessOutput, 0, 1, uintptr(unsafe.Pointer(&buf)), uintptr(unsafe.Pointer(&status)))
	if buf.Events != nil {
		buf.Events.release()
	}
	if err != nil {
		return nil, err
	}
	var mb *comObject
	if err := sample.call(sampleGetBuffer, 0, uintptr(unsafe.Pointer(&mb))); err != nil || mb == nil {
		return nil, fmt.Errorf("the encoder's picture has no buffer (%v)", err)
	}
	defer mb.release()
	var p *byte
	var maxLen, length uint32
	if err := mb.call(bufferLock, uintptr(unsafe.Pointer(&p)), uintptr(unsafe.Pointer(&maxLen)), uintptr(unsafe.Pointer(&length))); err != nil {
		return nil, err
	}
	au := append([]byte(nil), unsafe.Slice(p, int(length))...)
	_ = mb.call(bufferUnlock)
	return au, nil
}

// sample is a new sample with one memory buffer: holding data, or empty
// with room for an encoded picture.
func (e *mfEncoder) sample(data []byte) (*comObject, error) {
	size := e.outSize
	if data != nil {
		size = uint32(len(data))
	}
	var s, mb *comObject
	if r, _, _ := procMFCreateSample.Call(uintptr(unsafe.Pointer(&s))); int32(r) < 0 || s == nil {
		return nil, hresult(uint32(r))
	}
	if r, _, _ := procMFCreateMemBuffer.Call(uintptr(size), uintptr(unsafe.Pointer(&mb))); int32(r) < 0 || mb == nil {
		s.release()
		return nil, hresult(uint32(r))
	}
	defer mb.release()
	if data != nil {
		var p *byte
		var maxLen, length uint32
		if err := mb.call(bufferLock, uintptr(unsafe.Pointer(&p)), uintptr(unsafe.Pointer(&maxLen)), uintptr(unsafe.Pointer(&length))); err != nil {
			s.release()
			return nil, err
		}
		copy(unsafe.Slice(p, int(maxLen)), data)
		_ = mb.call(bufferUnlock)
		_ = mb.call(bufferSetLength, uintptr(len(data)))
	}
	if err := s.call(sampleAddBuffer, uintptr(unsafe.Pointer(mb))); err != nil {
		s.release()
		return nil, err
	}
	return s, nil
}

func (e *mfEncoder) SetBitrate(kbps int) {
	if kbps == e.kbps {
		return
	}
	_ = e.do(func() {
		if e.setCodec(codecapiMeanBitRate, vtUI4, uint64(kbps)*1000) {
			e.kbps = kbps
		}
	})
}

func (e *mfEncoder) Close() {
	e.closeOnce.Do(func() { close(e.done) })
}

// release lets go of the COM objects (on the COM thread).
func (e *mfEncoder) release() {
	if e.codec != nil {
		e.codec.release()
		e.codec = nil
	}
	if e.mft != nil {
		e.mft.release()
		e.mft = nil
	}
}

func annexBHasNAL(au []byte, kind byte) bool {
	for i := 0; i+4 < len(au); i++ {
		if au[i] == 0 && au[i+1] == 0 && (au[i+2] == 1 || (au[i+2] == 0 && au[i+3] == 1)) {
			at := i + 3
			if au[i+2] == 0 {
				at = i + 4
			}
			if at < len(au) && au[at]&0x1f == kind {
				return true
			}
			i = at - 1
		}
	}
	return false
}

// rgbaToNV12 converts img (even size) to NV12: a luma plane, then
// interleaved Cb Cr at half size each way. BT.601, limited range.
func rgbaToNV12(dst []byte, img *image.RGBA) {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	chroma := dst[w*h:]
	bands := min(runtime.GOMAXPROCS(0), 8)
	rowsPerBand := (h/2 + bands - 1) / bands * 2
	var wg sync.WaitGroup
	for top := 0; top < h; top += rowsPerBand {
		bottom := min(top+rowsPerBand, h)
		wg.Add(1)
		go func(top, bottom int) {
			defer wg.Done()
			for y := top; y < bottom; y += 2 {
				r0 := img.Pix[img.PixOffset(img.Rect.Min.X, img.Rect.Min.Y+y):]
				r1 := img.Pix[img.PixOffset(img.Rect.Min.X, img.Rect.Min.Y+y+1):]
				y0 := dst[y*w : y*w+w]
				y1 := dst[(y+1)*w : (y+1)*w+w]
				c := chroma[y/2*w : y/2*w+w]
				for x := 0; x < w; x += 2 {
					var rs, gs, bs int
					for i, row := range [2][]byte{r0, r1} {
						out := y0
						if i == 1 {
							out = y1
						}
						for dx := range 2 {
							p := row[(x+dx)*4:]
							r, g, b := int(p[0]), int(p[1]), int(p[2])
							out[x+dx] = byte((66*r+129*g+25*b+128)>>8 + 16)
							rs, gs, bs = rs+r, gs+g, bs+b
						}
					}
					rs, gs, bs = rs/4, gs/4, bs/4
					c[x] = byte((-38*rs-74*gs+112*bs+128)>>8 + 128)
					c[x+1] = byte((112*rs-94*gs-18*bs+128)>>8 + 128)
				}
			}
		}(top, bottom)
	}
	wg.Wait()
}
