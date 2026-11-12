package server

import (
	"reflect"
	"strings"
	"testing"

	"github.com/daoleno/mewla/daemon/work"
)

// The fingerprint is what decides whether a client is told that an event
// changed, so a field the client receives and the fingerprint ignores is a
// change that never arrives: a review that settled, a card that was read, a
// Session that closed stayed as it was until the conversation changed some
// other way. Every field of the wire shape must move it.
func TestEventFingerprintCoversEveryFieldTheClientReceives(t *testing.T) {
	zero := codexConversationEventFingerprint(work.CodexConversationEvent{})
	eventType := reflect.TypeOf(work.CodexConversationEvent{})
	for index := range eventType.NumField() {
		field := eventType.Field(index)
		if tag := field.Tag.Get("json"); tag == "-" || !field.IsExported() {
			continue
		}
		var event work.CodexConversationEvent
		fillNonZero(reflect.ValueOf(&event).Elem().Field(index), 0)
		if codexConversationEventFingerprint(event) == zero {
			t.Errorf("a change to %s (json %q) does not change the event fingerprint", field.Name, strings.Split(field.Tag.Get("json"), ",")[0])
		}
	}
}

// fillNonZero sets value to something other than its zero value, going only
// as deep as the shapes need: a subagent holds events, which hold subagents.
func fillNonZero(value reflect.Value, depth int) {
	if depth > 3 {
		return
	}
	switch value.Kind() {
	case reflect.String:
		value.SetString("x")
	case reflect.Bool:
		value.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		value.SetInt(1)
	case reflect.Pointer:
		value.Set(reflect.New(value.Type().Elem()))
		fillNonZero(value.Elem(), depth+1)
	case reflect.Slice:
		value.Set(reflect.MakeSlice(value.Type(), 1, 1))
		fillNonZero(value.Index(0), depth+1)
	case reflect.Struct:
		for index := range value.NumField() {
			if value.Type().Field(index).IsExported() {
				fillNonZero(value.Field(index), depth+1)
			}
		}
	}
}
