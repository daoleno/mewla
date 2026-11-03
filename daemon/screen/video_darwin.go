//go:build darwin && !screenfake

package screen

import (
	"errors"
	"fmt"
	"image"
	"log"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// The Mac's H.264 encoder is VideoToolbox's hardware encoder, reached through
// purego like the rest of the macOS backend, so release builds stay
// cross-compiled without cgo. It runs real-time with no B-frames, so each
// frame comes out the moment it is encoded, and needs no permission.

func init() { defaultVideo = newVTEncoder }

const (
	vtCodecH264     = 0x61766331 // 'avc1'
	vtPixelBGRA     = 0x42475241 // 'BGRA'
	vtTimescale     = 1000
	vtResultTimeout = 3 * time.Second
	vtKeyInterval   = 900 // frames: a key frame at least every 30 s
)

var vt struct {
	once sync.Once
	err  error

	create        func(alloc uintptr, width, height int32, codec uint32, spec, srcAttrs, dataAlloc, callback, refcon uintptr, out *uintptr) int32
	setProperty   func(session, key, value uintptr) int32
	prepare       func(session uintptr) int32
	encodeFrame   func(session, image uintptr, pts, duration cmTime, props, refcon uintptr, flags *uint32) int32
	completeUntil func(session uintptr, until cmTime) int32
	invalidate    func(session uintptr)

	pixelBufferCreate func(alloc uintptr, width, height uint64, format uint32, attrs uintptr, out *uintptr) int32

	sampleDataBuffer  func(sample uintptr) uintptr
	sampleFormatDesc  func(sample uintptr) uintptr
	sampleAttachments func(sample uintptr, create bool) uintptr
	blockLength       func(block uintptr) uint64
	blockCopy         func(block uintptr, offset, length uint64, dest unsafe.Pointer) int32
	h264ParameterSet  func(desc uintptr, index uint64, data *unsafe.Pointer, size *uint64, count *uint64, nalLength *int32) int32

	arrayCount func(array uintptr) int64
	arrayAt    func(array uintptr, index int64) uintptr
	dictValue  func(dict, key uintptr) uintptr

	callback uintptr

	// Constants the framework exports; their values are the strings named,
	// used when a symbol is missing.
	keyRealTime, keyProfile, keyReorder, keyBitrate, keyKeyInterval, keyFrameRate, keyRateLimits uintptr
	keyRequireHW, keyForceKey, keyNotSync, keyIOSurface                                          uintptr
	profileHigh, boolTrue                                                                        uintptr
}

var vtEncoders sync.Map // refcon -> *vtEncoder
var vtNextID atomic.Uintptr

func loadVT() error {
	vt.once.Do(func() { vt.err = loadVTFunctions() })
	return vt.err
}

func loadVTFunctions() error {
	if err := loadMac(); err != nil {
		return err
	}
	const frameworks = "/System/Library/Frameworks/"
	libs := map[string]uintptr{}
	for _, name := range []string{"VideoToolbox", "CoreMedia", "CoreVideo", "CoreFoundation"} {
		lib, err := purego.Dlopen(frameworks+name+".framework/"+name, purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			return fmt.Errorf("load %s: %w", name, err)
		}
		libs[name] = lib
	}
	var missing []string
	bind := func(fn any, lib, name string) {
		if _, err := purego.Dlsym(libs[lib], name); err != nil {
			missing = append(missing, name)
			return
		}
		purego.RegisterLibFunc(fn, libs[lib], name)
	}
	bind(&vt.create, "VideoToolbox", "VTCompressionSessionCreate")
	bind(&vt.setProperty, "VideoToolbox", "VTSessionSetProperty")
	bind(&vt.prepare, "VideoToolbox", "VTCompressionSessionPrepareToEncodeFrames")
	bind(&vt.encodeFrame, "VideoToolbox", "VTCompressionSessionEncodeFrame")
	bind(&vt.completeUntil, "VideoToolbox", "VTCompressionSessionCompleteFrames")
	bind(&vt.invalidate, "VideoToolbox", "VTCompressionSessionInvalidate")
	bind(&vt.pixelBufferCreate, "CoreVideo", "CVPixelBufferCreate")
	bind(&vt.sampleDataBuffer, "CoreMedia", "CMSampleBufferGetDataBuffer")
	bind(&vt.sampleFormatDesc, "CoreMedia", "CMSampleBufferGetFormatDescription")
	bind(&vt.sampleAttachments, "CoreMedia", "CMSampleBufferGetSampleAttachmentsArray")
	bind(&vt.blockLength, "CoreMedia", "CMBlockBufferGetDataLength")
	bind(&vt.blockCopy, "CoreMedia", "CMBlockBufferCopyDataBytes")
	bind(&vt.h264ParameterSet, "CoreMedia", "CMVideoFormatDescriptionGetH264ParameterSetAtIndex")
	bind(&vt.arrayCount, "CoreFoundation", "CFArrayGetCount")
	bind(&vt.arrayAt, "CoreFoundation", "CFArrayGetValueAtIndex")
	bind(&vt.dictValue, "CoreFoundation", "CFDictionaryGetValue")
	if len(missing) > 0 {
		return fmt.Errorf("this version of macOS lacks %v", missing)
	}
	constant := func(lib, name, fallback string) uintptr {
		if sym, err := purego.Dlsym(libs[lib], name); err == nil {
			// The symbol is the address of a CFStringRef.
			return **(**uintptr)(unsafe.Pointer(&sym))
		}
		return uintptr(nsString(fallback))
	}
	vt.keyRealTime = constant("VideoToolbox", "kVTCompressionPropertyKey_RealTime", "RealTime")
	vt.keyProfile = constant("VideoToolbox", "kVTCompressionPropertyKey_ProfileLevel", "ProfileLevel")
	vt.keyReorder = constant("VideoToolbox", "kVTCompressionPropertyKey_AllowFrameReordering", "AllowFrameReordering")
	vt.keyBitrate = constant("VideoToolbox", "kVTCompressionPropertyKey_AverageBitRate", "AverageBitRate")
	vt.keyKeyInterval = constant("VideoToolbox", "kVTCompressionPropertyKey_MaxKeyFrameInterval", "MaxKeyFrameInterval")
	vt.keyFrameRate = constant("VideoToolbox", "kVTCompressionPropertyKey_ExpectedFrameRate", "ExpectedFrameRate")
	vt.keyRateLimits = constant("VideoToolbox", "kVTCompressionPropertyKey_DataRateLimits", "DataRateLimits")
	vt.keyRequireHW = constant("VideoToolbox", "kVTVideoEncoderSpecification_RequireHardwareAcceleratedVideoEncoder", "RequireHardwareAcceleratedVideoEncoder")
	vt.keyForceKey = constant("VideoToolbox", "kVTEncodeFrameOptionKey_ForceKeyFrame", "ForceKeyFrame")
	vt.keyNotSync = constant("CoreMedia", "kCMSampleAttachmentKey_NotSync", "NotSync")
	vt.keyIOSurface = constant("CoreVideo", "kCVPixelBufferIOSurfacePropertiesKey", "IOSurfaceProperties")
	vt.profileHigh = constant("VideoToolbox", "kVTProfileLevel_H264_High_AutoLevel", "H264_High_AutoLevel")
	if sym, err := purego.Dlsym(libs["CoreFoundation"], "kCFBooleanTrue"); err == nil {
		vt.boolTrue = **(**uintptr)(unsafe.Pointer(&sym))
	}
	vt.callback = purego.NewCallback(vtOutput)
	return nil
}

// vtResult is what the output callback hands the encoding goroutine.
type vtResult struct {
	data []byte
	key  bool
	err  error
}

type vtEncoder struct {
	id      uintptr
	session uintptr
	pixels  uintptr
	width   int
	height  int
	started time.Time
	results chan vtResult
	kbps    int
	closed  bool
}

func newVTEncoder(width, height, kbps int) (videoEncoder, error) {
	if err := loadVT(); err != nil {
		return nil, err
	}
	e := &vtEncoder{width: width, height: height, kbps: kbps, started: time.Now(), results: make(chan vtResult, 4)}
	e.id = vtNextID.Add(1)
	var err error
	withPool(func() { err = e.open() })
	if err != nil {
		e.release()
		return nil, err
	}
	vtEncoders.Store(e.id, e)
	return e, nil
}

func nsNumber(v int32) objc.ID {
	return objc.ID(objc.GetClass("NSNumber")).Send(objc.RegisterName("numberWithInt:"), v)
}

func (e *vtEncoder) open() error {
	spec := objc.ID(objc.GetClass("NSMutableDictionary")).Send(objc.RegisterName("dictionary"))
	setObject := objc.RegisterName("setObject:forKey:")
	spec.Send(setObject, objc.ID(vt.boolTrue), objc.ID(vt.keyRequireHW))
	var session uintptr
	// The callback's refcon is this encoder's id.
	if status := vt.create(0, int32(e.width), int32(e.height), vtCodecH264, uintptr(spec), 0, 0, vt.callback, e.id, &session); status != 0 {
		return fmt.Errorf("VideoToolbox has no hardware H.264 encoder for this screen (status %d)", status)
	}
	e.session = session
	no := objc.ID(objc.GetClass("NSNumber")).Send(objc.RegisterName("numberWithBool:"), false)
	for _, p := range []struct {
		key   uintptr
		value uintptr
		name  string
	}{
		{vt.keyRealTime, vt.boolTrue, "real time"},
		{vt.keyReorder, uintptr(no), "frame reordering"},
		{vt.keyProfile, vt.profileHigh, "profile"},
		{vt.keyKeyInterval, uintptr(nsNumber(vtKeyInterval)), "key frame interval"},
		{vt.keyFrameRate, uintptr(nsNumber(videoFPS)), "frame rate"},
	} {
		if p.key == 0 || p.value == 0 {
			continue
		}
		if status := vt.setProperty(e.session, p.key, p.value); status != 0 {
			log.Printf("screen: video: could not set %s (status %d)", p.name, status)
		}
	}
	e.applyBitrate(e.kbps)
	attrs := objc.ID(objc.GetClass("NSMutableDictionary")).Send(objc.RegisterName("dictionary"))
	attrs.Send(setObject, objc.ID(objc.GetClass("NSDictionary")).Send(objc.RegisterName("dictionary")), objc.ID(vt.keyIOSurface))
	var pixels uintptr
	if status := vt.pixelBufferCreate(0, uint64(e.width), uint64(e.height), vtPixelBGRA, uintptr(attrs), &pixels); status != 0 {
		return fmt.Errorf("could not allocate a pixel buffer (status %d)", status)
	}
	e.pixels = pixels
	if status := vt.prepare(e.session); status != 0 {
		return fmt.Errorf("VideoToolbox could not prepare to encode (status %d)", status)
	}
	return nil
}

// applyBitrate sets the average rate and caps a second's data at half again
// as much.
func (e *vtEncoder) applyBitrate(kbps int) {
	bits := int32(kbps * 1000)
	if status := vt.setProperty(e.session, vt.keyBitrate, uintptr(nsNumber(bits))); status != 0 {
		log.Printf("screen: video: could not set the bit rate (status %d)", status)
	}
	limits := objc.ID(objc.GetClass("NSMutableArray")).Send(objc.RegisterName("array"))
	add := objc.RegisterName("addObject:")
	limits.Send(add, nsNumber(bits/8*3/2))
	limits.Send(add, nsNumber(1))
	_ = vt.setProperty(e.session, vt.keyRateLimits, uintptr(limits))
	e.kbps = kbps
}

func (e *vtEncoder) SetBitrate(kbps int) {
	if e.closed || kbps == e.kbps {
		return
	}
	withPool(func() { e.applyBitrate(kbps) })
}

func (e *vtEncoder) Encode(img *image.RGBA, key bool) (data []byte, isKey bool, err error) {
	if e.closed {
		return nil, false, errors.New("the encoder is closed")
	}
	if img.Rect.Dx() != e.width || img.Rect.Dy() != e.height {
		return nil, false, fmt.Errorf("frame is %dx%d, the encoder %dx%d", img.Rect.Dx(), img.Rect.Dy(), e.width, e.height)
	}
	// Anything left from a frame that timed out is stale.
	for len(e.results) > 0 {
		<-e.results
	}
	withPool(func() { err = e.submit(img, key) })
	if err != nil {
		return nil, false, err
	}
	select {
	case res := <-e.results:
		return res.data, res.key, res.err
	case <-time.After(vtResultTimeout):
		return nil, false, errors.New("VideoToolbox did not answer")
	}
}

func (e *vtEncoder) submit(img *image.RGBA, key bool) error {
	if status := mac.CVPixelBufferLockBaseAddress(e.pixels, 0); status != 0 {
		return fmt.Errorf("could not lock the pixel buffer (status %d)", status)
	}
	base := mac.CVPixelBufferGetBaseAddress(e.pixels)
	stride := int(mac.CVPixelBufferGetBytesPerRow(e.pixels))
	dst := unsafe.Slice((*byte)(base), stride*e.height)
	row := e.width * 4
	for y := 0; y < e.height; y++ {
		in := img.Pix[y*img.Stride : y*img.Stride+row]
		out := dst[y*stride : y*stride+row]
		for i := 0; i < row; i += 4 {
			out[i], out[i+1], out[i+2], out[i+3] = in[i+2], in[i+1], in[i], 255
		}
	}
	mac.CVPixelBufferUnlockBase(e.pixels, 0)

	var props uintptr
	if key {
		dict := objc.ID(objc.GetClass("NSMutableDictionary")).Send(objc.RegisterName("dictionary"))
		dict.Send(objc.RegisterName("setObject:forKey:"), objc.ID(vt.boolTrue), objc.ID(vt.keyForceKey))
		props = uintptr(dict)
	}
	now := time.Since(e.started).Milliseconds()
	pts := cmTime{Value: now, Timescale: vtTimescale, Flags: 1}
	duration := cmTime{Value: 1000 / videoFPS, Timescale: vtTimescale, Flags: 1}
	var info uint32
	if status := vt.encodeFrame(e.session, e.pixels, pts, duration, props, 0, &info); status != 0 {
		return fmt.Errorf("VideoToolbox could not encode (status %d)", status)
	}
	// An invalid time completes every frame in flight: the callback has run
	// for this one by the time this returns.
	if status := vt.completeUntil(e.session, cmTime{}); status != 0 {
		return fmt.Errorf("VideoToolbox could not finish the frame (status %d)", status)
	}
	return nil
}

// vtOutput is VideoToolbox's output callback, on its own thread: it copies the
// frame out and passes it on.
func vtOutput(refcon, _, status, _, sample uintptr) {
	value, ok := vtEncoders.Load(refcon)
	if !ok {
		return
	}
	e := value.(*vtEncoder)
	var res vtResult
	switch {
	case int32(uint32(status)) != 0:
		res.err = fmt.Errorf("VideoToolbox failed on a frame (status %d)", int32(uint32(status)))
	case sample == 0:
		res.err = errVideoDropped
	default:
		res.data, res.key, res.err = e.extract(sample)
	}
	select {
	case e.results <- res:
	default:
	}
}

// extract copies a sample's NAL units out as an Annex B frame.
func (e *vtEncoder) extract(sample uintptr) ([]byte, bool, error) {
	block := vt.sampleDataBuffer(sample)
	if block == 0 {
		return nil, false, errVideoDropped
	}
	size := vt.blockLength(block)
	if size == 0 {
		return nil, false, errVideoDropped
	}
	frame := make([]byte, size)
	if status := vt.blockCopy(block, 0, size, unsafe.Pointer(&frame[0])); status != 0 {
		return nil, false, fmt.Errorf("could not read a frame (status %d)", status)
	}
	key := true
	if attachments := vt.sampleAttachments(sample, false); attachments != 0 && vt.arrayCount(attachments) > 0 {
		if dict := vt.arrayAt(attachments, 0); dict != 0 {
			// A frame that other frames depend on lacks NotSync, or has it false.
			if notSync := vt.dictValue(dict, vt.keyNotSync); notSync != 0 && notSync == vt.boolTrue {
				key = false
			}
		}
	}
	var sets [][]byte
	nalLength := int32(4)
	if key {
		desc := vt.sampleFormatDesc(sample)
		var count uint64
		for i := uint64(0); desc != 0 && (i == 0 || i < count); i++ {
			var data unsafe.Pointer
			var length uint64
			if status := vt.h264ParameterSet(desc, i, &data, &length, &count, &nalLength); status != 0 {
				return nil, false, fmt.Errorf("could not read the parameter sets (status %d)", status)
			}
			sets = append(sets, append([]byte(nil), unsafe.Slice((*byte)(data), int(length))...))
		}
	}
	out := avccToAnnexB(frame, int(nalLength), sets, key)
	if out == nil {
		return nil, false, errors.New("the encoder produced a frame this code cannot read")
	}
	return out, key, nil
}

func (e *vtEncoder) release() {
	if e.session != 0 {
		vt.invalidate(e.session)
		mac.CFRelease(e.session)
		e.session = 0
	}
	if e.pixels != 0 {
		mac.CVPixelBufferRelease(e.pixels)
		e.pixels = 0
	}
}

func (e *vtEncoder) Close() {
	if e.closed {
		return
	}
	e.closed = true
	vtEncoders.Delete(e.id)
	e.release()
}
