//go:build darwin && !screenfake

package screen

import (
	"errors"
	"log"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/ebitengine/purego"
)

// An AXObserver delivers an app's accessibility notifications on a run loop,
// so one thread of this process runs a run loop for them. It is started
// the first time an agent asks to watch an app and only when
// MEWLA_SCREEN_AX_OBSERVER is set (see axwatch.go).

var axObs struct {
	once sync.Once
	err  error

	create        func(pid int32, callback uintptr, out *uintptr) int32
	add           func(observer, element, notification, refcon uintptr) int32
	sourceOf      func(observer uintptr) uintptr
	loopCurrent   func() uintptr
	loopAddSource func(loop, source, mode uintptr)
	loopRemove    func(loop, source, mode uintptr)
	loopRun       func(mode uintptr, seconds float64, returnAfterSource bool) int32
	mode          uintptr

	requests chan func()
	next     atomic.Uintptr
	mu       sync.Mutex
	watches  map[uintptr]*axWatch
	callback uintptr
}

// The notifications that say an app changed, asked of the app itself: many
// reach it from the elements inside.
var axNotifications = []string{
	"AXValueChanged", "AXFocusedUIElementChanged", "AXFocusedWindowChanged", "AXWindowCreated",
	"AXTitleChanged", "AXUIElementDestroyed", "AXSelectedTextChanged", "AXSelectedChildrenChanged",
	"AXLayoutChanged", "AXMenuOpened", "AXMenuClosed",
}

func loadAXObserver() error {
	axObs.once.Do(func() {
		if !loadApps() {
			axObs.err = ErrNoApps
			return
		}
		as, err := purego.Dlopen("/System/Library/Frameworks/ApplicationServices.framework/ApplicationServices", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			axObs.err = err
			return
		}
		cf, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			axObs.err = err
			return
		}
		for _, fn := range []struct {
			ptr  any
			lib  uintptr
			name string
		}{
			{&axObs.create, as, "AXObserverCreate"},
			{&axObs.add, as, "AXObserverAddNotification"},
			{&axObs.sourceOf, as, "AXObserverGetRunLoopSource"},
			{&axObs.loopCurrent, cf, "CFRunLoopGetCurrent"},
			{&axObs.loopAddSource, cf, "CFRunLoopAddSource"},
			{&axObs.loopRemove, cf, "CFRunLoopRemoveSource"},
			{&axObs.loopRun, cf, "CFRunLoopRunInMode"},
		} {
			if _, err := purego.Dlsym(fn.lib, fn.name); err != nil {
				axObs.err = errors.New("no " + fn.name)
				return
			}
			purego.RegisterLibFunc(fn.ptr, fn.lib, fn.name)
		}
		// kCFRunLoopDefaultMode is a CFString whose text is its own name; modes
		// are matched by string equality.
		if axObs.mode = axLib.createString(0, "kCFRunLoopDefaultMode", cfStringEncodingUTF8); axObs.mode == 0 {
			axObs.err = errors.New("no run loop mode")
			return
		}
		axObs.watches = map[uintptr]*axWatch{}
		axObs.callback = purego.NewCallback(func(observer, element, notification, refcon uintptr) uintptr {
			axObs.mu.Lock()
			w := axObs.watches[refcon]
			axObs.mu.Unlock()
			if w != nil {
				select {
				case w.events <- struct{}{}:
				default: // a slow reader loses events, never blocks the app
				}
				if settleTrace {
					log.Printf("screen: ax notification %s", cfStringOf(notification))
				}
			}
			return 0
		})
		axObs.requests = make(chan func())
		go func() {
			runtime.LockOSThread()
			for {
				select {
				case f := <-axObs.requests:
					f()
				default:
					withPool(func() { axObs.loopRun(axObs.mode, 0.02, false) })
				}
			}
		}()
	})
	return axObs.err
}

// onLoop runs f on the run loop's thread and waits for it.
func onLoop(f func()) {
	done := make(chan struct{})
	axObs.requests <- func() { f(); close(done) }
	<-done
}

type axWatch struct {
	events   chan struct{}
	observer uintptr
	source   uintptr
	loop     uintptr
	id       uintptr
}

func (w *axWatch) Events() <-chan struct{} { return w.events }

func (w *axWatch) Close() {
	axObs.mu.Lock()
	delete(axObs.watches, w.id)
	axObs.mu.Unlock()
	onLoop(func() {
		if w.source != 0 {
			axObs.loopRemove(w.loop, w.source, axObs.mode)
		}
		if w.observer != 0 {
			axLib.release(w.observer)
		}
	})
}

// WatchApp listens to pid's notifications until Close.
func (b *macBackend) WatchApp(pid int) (AppWatch, error) {
	if err := loadAXObserver(); err != nil {
		return nil, err
	}
	w := &axWatch{events: make(chan struct{}, 64), id: axObs.next.Add(1)}
	var err error
	onLoop(func() {
		var observer uintptr
		if code := axObs.create(int32(pid), axObs.callback, &observer); code != 0 || observer == 0 {
			err = errors.New("couldn't observe the app")
			return
		}
		app := appsLib.createApplication(int32(pid))
		if app == 0 {
			axLib.release(observer)
			err = errors.New("couldn't reach the app")
			return
		}
		defer axLib.release(app)
		added := 0
		for _, name := range axNotifications {
			note := axLib.createString(0, name, cfStringEncodingUTF8)
			if note == 0 {
				continue
			}
			if axObs.add(observer, app, note, w.id) == 0 {
				added++
			}
			axLib.release(note)
		}
		if added == 0 {
			axLib.release(observer)
			err = errors.New("the app takes no notifications")
			return
		}
		w.observer, w.loop = observer, axObs.loopCurrent()
		w.source = axObs.sourceOf(observer)
		axObs.loopAddSource(w.loop, w.source, axObs.mode)
	})
	if err != nil {
		return nil, err
	}
	axObs.mu.Lock()
	axObs.watches[w.id] = w
	axObs.mu.Unlock()
	return w, nil
}

var _ appWatcher = (*macBackend)(nil)
