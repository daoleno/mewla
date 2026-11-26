package modelprofiles

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// What a Provider connection was asked, kept on the daemon for its page: each
// model request with its tokens and cost, the last failures, and a total per
// day. The requests are read from what the agent was answered, which is always
// in its own API (Anthropic Messages or OpenAI Responses), so translated,
// moved and passed-through requests are read the same way.

const (
	usageKeepRequests = 100
	usageKeepErrors   = 20
	usageKeepDays     = 30
	usageSaveDelay    = 2 * time.Second
	usageMessageMax   = 300
)

// UsageRecord is one model request a connection answered.
type UsageRecord struct {
	At    time.Time `json:"at"`
	Agent string    `json:"agent"`
	Model string    `json:"model,omitempty"`
	// Input is the input not read from the prompt cache; CacheRead and
	// CacheWrite are apart from it.
	Input      int64   `json:"input_tokens"`
	CacheRead  int64   `json:"cache_read_tokens,omitempty"`
	CacheWrite int64   `json:"cache_write_tokens,omitempty"`
	Output     int64   `json:"output_tokens"`
	CostUSD    float64 `json:"cost_usd,omitempty"`
	// Priced says the model's prices were known; a cost of 0 is then free.
	Priced bool  `json:"priced,omitempty"`
	Ms     int64 `json:"ms"`
}

// UsageError is one model request a connection failed.
type UsageError struct {
	At      time.Time `json:"at"`
	Agent   string    `json:"agent"`
	Model   string    `json:"model,omitempty"`
	Status  int       `json:"status,omitempty"`
	Message string    `json:"message"`
}

// UsageTotals adds up a connection's requests over one day.
type UsageTotals struct {
	Requests   int     `json:"requests"`
	Errors     int     `json:"errors,omitempty"`
	Input      int64   `json:"input_tokens"`
	CacheRead  int64   `json:"cache_read_tokens,omitempty"`
	CacheWrite int64   `json:"cache_write_tokens,omitempty"`
	Output     int64   `json:"output_tokens"`
	CostUSD    float64 `json:"cost_usd,omitempty"`
	// Unpriced counts requests whose model had no known price.
	Unpriced int `json:"unpriced,omitempty"`
}

// ConnectionUsage is what the Provider page shows: newest first, and the days
// by their local date (2026-11-27).
type ConnectionUsage struct {
	Requests []UsageRecord          `json:"requests"`
	Errors   []UsageError           `json:"errors"`
	Days     map[string]UsageTotals `json:"days"`
}

type usagePrices struct {
	input, output, cacheRead, cacheWrite *float64
}

type usageLog struct {
	mu     sync.Mutex
	byConn map[string]*ConnectionUsage
	path   string
	now    func() time.Time
	prices func(connectionID, model string) (usagePrices, bool)
	timer  *time.Timer
	dirty  bool
}

func newUsageLog(path string) *usageLog {
	l := &usageLog{byConn: map[string]*ConnectionUsage{}, path: path, now: time.Now}
	if path == "" {
		return l
	}
	if raw, err := os.ReadFile(path); err == nil {
		var saved map[string]*ConnectionUsage
		if json.Unmarshal(raw, &saved) == nil {
			for id, u := range saved {
				if u != nil {
					l.byConn[normalizeID(id)] = u
				}
			}
		}
	}
	return l
}

func (l *usageLog) entry(id string) *ConnectionUsage {
	id = normalizeID(id)
	u := l.byConn[id]
	if u == nil {
		u = &ConnectionUsage{Days: map[string]UsageTotals{}}
		l.byConn[id] = u
	}
	if u.Days == nil {
		u.Days = map[string]UsageTotals{}
	}
	return u
}

// record adds one answered request, pricing it from the model's catalog.
func (l *usageLog) record(connectionID string, r UsageRecord) {
	if l == nil || connectionID == "" {
		return
	}
	if l.prices != nil && r.Model != "" {
		if p, ok := l.prices(connectionID, r.Model); ok && p.input != nil && p.output != nil {
			cost := float64(r.Input)**p.input + float64(r.Output)**p.output
			cost += float64(r.CacheRead) * firstRate(p.cacheRead, p.input)
			cost += float64(r.CacheWrite) * firstRate(p.cacheWrite, p.input)
			r.CostUSD, r.Priced = cost/1e6, true
		}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	u := l.entry(connectionID)
	u.Requests = append([]UsageRecord{r}, u.Requests...)
	if len(u.Requests) > usageKeepRequests {
		u.Requests = u.Requests[:usageKeepRequests]
	}
	day := r.At.In(time.Local).Format("2006-01-02")
	t := u.Days[day]
	t.Requests++
	t.Input += r.Input
	t.CacheRead += r.CacheRead
	t.CacheWrite += r.CacheWrite
	t.Output += r.Output
	t.CostUSD += r.CostUSD
	if !r.Priced {
		t.Unpriced++
	}
	u.Days[day] = t
	l.trimDays(u)
	l.changed()
}

// fail adds one failed request.
func (l *usageLog) fail(connectionID string, e UsageError) {
	if l == nil || connectionID == "" {
		return
	}
	e.Message = cleanUsageMessage(e.Message)
	l.mu.Lock()
	defer l.mu.Unlock()
	u := l.entry(connectionID)
	u.Errors = append([]UsageError{e}, u.Errors...)
	if len(u.Errors) > usageKeepErrors {
		u.Errors = u.Errors[:usageKeepErrors]
	}
	day := e.At.In(time.Local).Format("2006-01-02")
	t := u.Days[day]
	t.Errors++
	u.Days[day] = t
	l.trimDays(u)
	l.changed()
}

func (l *usageLog) trimDays(u *ConnectionUsage) {
	if len(u.Days) <= usageKeepDays {
		return
	}
	days := make([]string, 0, len(u.Days))
	for d := range u.Days {
		days = append(days, d)
	}
	sort.Strings(days)
	for _, d := range days[:len(days)-usageKeepDays] {
		delete(u.Days, d)
	}
}

// forget drops a deleted connection's usage.
func (l *usageLog) forget(connectionID string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.byConn[normalizeID(connectionID)]; ok {
		delete(l.byConn, normalizeID(connectionID))
		l.changed()
	}
}

// of returns a copy of a connection's usage.
func (l *usageLog) of(connectionID string) ConnectionUsage {
	out := ConnectionUsage{Requests: []UsageRecord{}, Errors: []UsageError{}, Days: map[string]UsageTotals{}}
	if l == nil {
		return out
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	u := l.byConn[normalizeID(connectionID)]
	if u == nil {
		return out
	}
	out.Requests = append(out.Requests, u.Requests...)
	out.Errors = append(out.Errors, u.Errors...)
	for d, t := range u.Days {
		out.Days[d] = t
	}
	return out
}

// changed schedules a save; l.mu is held.
func (l *usageLog) changed() {
	if l.path == "" {
		return
	}
	l.dirty = true
	if l.timer == nil {
		l.timer = time.AfterFunc(usageSaveDelay, func() { _ = l.save() })
	}
}

// save writes the log when it changed since the last save.
func (l *usageLog) save() error {
	if l == nil || l.path == "" {
		return nil
	}
	l.mu.Lock()
	l.timer = nil
	if !l.dirty {
		l.mu.Unlock()
		return nil
	}
	raw, err := json.Marshal(l.byConn)
	l.dirty = false
	l.mu.Unlock()
	if err != nil {
		return err
	}
	return atomicWriteDiscoveryFile(l.path, raw)
}

func firstRate(rate, fallback *float64) float64 {
	if rate != nil {
		return *rate
	}
	return *fallback
}

var usageSecretRE = regexp.MustCompile(`\b(sk|pk|rk|key)-[A-Za-z0-9_\-]{8,}`)

// cleanUsageMessage keeps a failure's words short and without a key an
// upstream may have echoed back.
func cleanUsageMessage(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	s = usageSecretRE.ReplaceAllString(s, "$1-…")
	if len([]rune(s)) > usageMessageMax {
		s = string([]rune(s)[:usageMessageMax]) + "…"
	}
	return s
}

// usageRecorder watches one model request's answer to the agent.
type usageRecorder struct {
	http.ResponseWriter
	log        *usageLog
	agent      string
	connection string // the connection that answered
	asked      string // the model the agent asked for
	start      time.Time

	status int
	stream bool
	line   []byte // a stream line not yet ended
	body   []byte // the start of a plain (not streamed) answer
	usage  usageCounts
	model  string // the model the answer named
	errMsg string // a failure the answer told
	done   bool
}

type usageCounts struct {
	input, cacheRead, cacheWrite, output int64
	seen                                 bool
}

const usageLineMax = 1 << 20

func newUsageRecorder(w http.ResponseWriter, log *usageLog, agent, connection string) *usageRecorder {
	return &usageRecorder{ResponseWriter: w, log: log, agent: agent, connection: connection, start: time.Now()}
}

// newUsageTurn reads one Responses turn over a websocket, whose events
// come as frames rather than through a ResponseWriter.
func newUsageTurn(log *usageLog, agent, connection string) *usageRecorder {
	r := newUsageRecorder(nil, log, agent, connection)
	r.status, r.stream = http.StatusOK, true
	return r
}

func (r *usageRecorder) WriteHeader(code int) {
	if r.status == 0 {
		r.status = code
		r.stream = strings.HasPrefix(r.Header().Get("Content-Type"), "text/event-stream")
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *usageRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.WriteHeader(http.StatusOK)
	}
	r.observe(b)
	return r.ResponseWriter.Write(b)
}

func (r *usageRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (r *usageRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// servedBy says another connection (a fallback) answered the request.
func (r *usageRecorder) servedBy(connectionID string) {
	if connectionID != "" {
		r.connection = connectionID
	}
}

func (r *usageRecorder) observe(b []byte) {
	if !r.stream {
		if room := usageLineMax - len(r.body); room > 0 {
			r.body = append(r.body, b[:min(len(b), room)]...)
		}
		return
	}
	for len(b) > 0 {
		i := bytes.IndexByte(b, '\n')
		if i < 0 {
			if len(r.line)+len(b) <= usageLineMax {
				r.line = append(r.line, b...)
			} else {
				r.line = r.line[:0] // a line too long to be usage
			}
			return
		}
		if len(r.line)+i <= usageLineMax {
			r.line = append(r.line, b[:i]...)
			r.event(r.line)
		}
		r.line = r.line[:0]
		b = b[i+1:]
	}
}

// event reads one stream line.
func (r *usageRecorder) event(line []byte) {
	line = bytes.TrimSpace(line)
	data, ok := bytes.CutPrefix(line, []byte("data:"))
	if !ok {
		return
	}
	data = bytes.TrimSpace(data)
	if !bytes.Contains(data, []byte(`"usage"`)) && !bytes.Contains(data, []byte(`"error"`)) && !bytes.Contains(data, []byte(`"model"`)) {
		return // most lines are text deltas
	}
	r.read(data)
}

// read takes usage, the model and a failure from one JSON object of either
// agent's API.
func (r *usageRecorder) read(data []byte) {
	var obj struct {
		Type  string          `json:"type"`
		Model string          `json:"model"`
		Usage json.RawMessage `json:"usage"`
		// Anthropic's message_start carries the message; a Responses error
		// event's message is its words.
		Message  json.RawMessage `json:"message"`
		Response *struct {
			Model string          `json:"model"`
			Usage json.RawMessage `json:"usage"`
			Error json.RawMessage `json:"error"`
		} `json:"response"`
		Error json.RawMessage `json:"error"`
	}
	if json.Unmarshal(data, &obj) != nil {
		return
	}
	if obj.Model != "" {
		r.model = obj.Model
	}
	r.addUsage(obj.Usage)
	if len(obj.Message) > 0 && obj.Message[0] == '{' {
		var message struct {
			Model string          `json:"model"`
			Usage json.RawMessage `json:"usage"`
		}
		if json.Unmarshal(obj.Message, &message) == nil {
			if message.Model != "" {
				r.model = message.Model
			}
			r.addUsage(message.Usage)
		}
	} else if obj.Type == "error" {
		if words := errorWords(obj.Message); words != "" {
			r.errMsg = words
		}
	}
	if obj.Response != nil {
		if obj.Response.Model != "" {
			r.model = obj.Response.Model
		}
		r.addUsage(obj.Response.Usage)
		if msg := errorWords(obj.Response.Error); msg != "" {
			r.errMsg = msg
		}
	}
	if msg := errorWords(obj.Error); msg != "" {
		r.errMsg = msg
	}
}

// addUsage folds one usage block in. Anthropic's input_tokens leave the cache
// out and its counts grow through the stream (the last one stands); OpenAI
// Responses' input_tokens hold the cached ones too.
func (r *usageRecorder) addUsage(raw json.RawMessage) {
	if len(raw) == 0 || raw[0] != '{' {
		return
	}
	var u struct {
		Input        *int64 `json:"input_tokens"`
		Output       *int64 `json:"output_tokens"`
		CacheRead    *int64 `json:"cache_read_input_tokens"`
		CacheWrite   *int64 `json:"cache_creation_input_tokens"`
		InputDetails *struct {
			Cached int64 `json:"cached_tokens"`
		} `json:"input_tokens_details"`
	}
	if json.Unmarshal(raw, &u) != nil {
		return
	}
	r.usage.seen = true
	if u.InputDetails != nil && u.Input != nil { // Responses
		r.usage.input = max(*u.Input-u.InputDetails.Cached, 0)
		r.usage.cacheRead = u.InputDetails.Cached
		if u.Output != nil {
			r.usage.output = *u.Output
		}
		return
	}
	if u.Input != nil && *u.Input > 0 {
		r.usage.input = *u.Input
	}
	if u.CacheRead != nil && *u.CacheRead > 0 {
		r.usage.cacheRead = *u.CacheRead
	}
	if u.CacheWrite != nil && *u.CacheWrite > 0 {
		r.usage.cacheWrite = *u.CacheWrite
	}
	if u.Output != nil && *u.Output > 0 {
		r.usage.output = *u.Output
	}
}

// errorWords is a failure's message in either agent's error shape.
func errorWords(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var asText string
	if json.Unmarshal(raw, &asText) == nil {
		return asText
	}
	var e struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	}
	if json.Unmarshal(raw, &e) != nil {
		return ""
	}
	return firstNonEmpty(e.Message, e.Type)
}

// finish records the request once the handler is done with it.
func (r *usageRecorder) finish() {
	if r == nil || r.done || r.log == nil {
		return
	}
	r.done = true
	if !r.stream && len(r.body) > 0 {
		r.read(bytes.TrimSpace(r.body))
	}
	if r.stream && len(r.line) > 0 {
		r.event(r.line)
	}
	status := r.status
	if status == 0 {
		return // nothing was answered (the agent went away first)
	}
	at := time.Now()
	model := firstNonEmpty(r.model, r.asked)
	if status >= 400 || r.errMsg != "" {
		msg := r.errMsg
		if msg == "" {
			msg = http.StatusText(status)
		}
		r.log.fail(r.connection, UsageError{At: at, Agent: r.agent, Model: model, Status: status, Message: msg})
		return
	}
	r.log.record(r.connection, UsageRecord{
		At: at, Agent: r.agent, Model: model,
		Input: r.usage.input, CacheRead: r.usage.cacheRead, CacheWrite: r.usage.cacheWrite, Output: r.usage.output,
		Ms: at.Sub(r.start).Milliseconds(),
	})
}

// usageAgent names the agent by the API it asked in.
func usageAgent(anthropic bool) string {
	if anthropic {
		return ClientClaude
	}
	return ClientCodex
}
