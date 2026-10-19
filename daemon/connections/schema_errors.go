package connections

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/google/jsonschema-go/jsonschema"
)

// Mismatches an explanation lists before it summarizes the rest, and how many
// the walk collects. A call that misses more than a few constraints usually
// means the caller guessed the whole shape.
const (
	schemaMismatchLimit  = 3
	schemaMismatchGather = schemaMismatchLimit + 1
	schemaMismatchDepth  = 32
)

// schemaMismatch is one argument that does not fit the tool's input schema.
type schemaMismatch struct {
	path    string
	problem string
}

// explainSchemaMismatch says why arguments missed a tool's input schema: which
// argument path, which constraint, and by how much. Validation itself stays
// with the schema library, so a rejected call is explained and nothing else
// changes.
func explainSchemaMismatch(inputSchema json.RawMessage, args map[string]any, cause error) error {
	e := &schemaExplainer{refs: map[string]*jsonschema.Schema{}, active: map[*jsonschema.Schema]bool{}}
	var root jsonschema.Schema
	if json.Unmarshal(inputSchema, &root) != nil {
		return schemaMismatchLine(nil, cause)
	}
	var document any
	if json.Unmarshal(inputSchema, &document) == nil {
		e.document = document
	}
	// Draft-07 ignores every keyword beside $ref; 2020-12 does not.
	e.draft7 = strings.Contains(root.Schema, "draft-07")
	return schemaMismatchLine(e.check(&root, args, "", 0), cause)
}

func schemaMismatchLine(mismatches []schemaMismatch, cause error) error {
	if len(mismatches) == 0 {
		return fmt.Errorf("arguments do not match the tool schema: %v; describe the tool first", cause)
	}
	parts := make([]string, 0, schemaMismatchLimit+1)
	for _, m := range mismatches {
		if len(parts) == schemaMismatchLimit {
			break
		}
		if m.path == "" {
			parts = append(parts, m.problem)
		} else {
			parts = append(parts, m.path+": "+m.problem)
		}
	}
	line := "arguments do not match the tool schema: " + strings.Join(parts, "; ")
	if len(mismatches) > schemaMismatchLimit {
		line += "; and more"
	}
	return errors.New(line + "; describe the tool first")
}

// schemaExplainer walks arguments beside a resolved schema. The schema library
// decides validity; this only names the paths and constraints it rejected.
type schemaExplainer struct {
	document any                           // schema JSON, for local $ref pointers
	refs     map[string]*jsonschema.Schema // resolved local $ref targets, nil when unreadable
	active   map[*jsonschema.Schema]bool   // $ref targets on the current path, for cycles
	draft7   bool
}

func (e *schemaExplainer) check(s *jsonschema.Schema, value any, path string, depth int) []schemaMismatch {
	if s == nil || depth > schemaMismatchDepth {
		return nil
	}
	var out []schemaMismatch
	add := func(problem string) { out = append(out, schemaMismatch{path: path, problem: problem}) }
	full := func() bool { return len(out) >= schemaMismatchGather }
	child := func(subschema *jsonschema.Schema, value any, path string) {
		if full() {
			return
		}
		out = append(out, e.check(subschema, value, path, depth+1)...)
	}

	ref := s.Ref
	if ref == "" {
		ref = s.DynamicRef
	}
	if ref != "" {
		if target := e.deref(ref); target != nil && !e.active[target] {
			e.active[target] = true
			child(target, value, path)
			delete(e.active, target)
		}
		if e.draft7 {
			return out
		}
	}

	if got := jsonTypeName(value); got != "" {
		if allowed := schemaTypes(s); len(allowed) > 0 && !typeMatches(got, allowed) {
			add(fmt.Sprintf("has type %s, expected %s", got, strings.Join(allowed, " or ")))
			return out
		}
	}
	if s.Enum != nil {
		matched := false
		for _, candidate := range s.Enum {
			if equalJSON(candidate, value) {
				matched = true
				break
			}
		}
		if !matched {
			add(fmt.Sprintf("is %s, expected one of %s", shortJSON(value), shortJSON(s.Enum)))
		}
	}
	if s.Const != nil && !equalJSON(*s.Const, value) {
		add(fmt.Sprintf("is %s, expected %s", shortJSON(value), shortJSON(*s.Const)))
	}

	if number, ok := jsonNumber(value); ok {
		if s.MultipleOf != nil && *s.MultipleOf != 0 {
			if _, remainder := math.Modf(number / *s.MultipleOf); remainder != 0 {
				add(fmt.Sprintf("is %s, multipleOf is %s", formatNumber(number), formatNumber(*s.MultipleOf)))
			}
		}
		if s.Minimum != nil && number < *s.Minimum {
			add(fmt.Sprintf("is %s, minimum is %s", formatNumber(number), formatNumber(*s.Minimum)))
		}
		if s.Maximum != nil && number > *s.Maximum {
			add(fmt.Sprintf("is %s, maximum is %s", formatNumber(number), formatNumber(*s.Maximum)))
		}
		if s.ExclusiveMinimum != nil && number <= *s.ExclusiveMinimum {
			add(fmt.Sprintf("is %s, must be more than %s", formatNumber(number), formatNumber(*s.ExclusiveMinimum)))
		}
		if s.ExclusiveMaximum != nil && number >= *s.ExclusiveMaximum {
			add(fmt.Sprintf("is %s, must be less than %s", formatNumber(number), formatNumber(*s.ExclusiveMaximum)))
		}
	}
	if text, ok := value.(string); ok {
		length := utf8.RuneCountInString(text)
		if s.MinLength != nil && length < *s.MinLength {
			add(fmt.Sprintf("has %d characters, minLength is %d", length, *s.MinLength))
		}
		if s.MaxLength != nil && length > *s.MaxLength {
			add(fmt.Sprintf("has %d characters, maxLength is %d", length, *s.MaxLength))
		}
		if s.Pattern != "" {
			if pattern, err := regexp.Compile(s.Pattern); err == nil && !pattern.MatchString(text) {
				add(fmt.Sprintf("%s does not match pattern %q", shortJSON(text), s.Pattern))
			}
		}
	}

	for _, subschema := range s.AllOf {
		child(subschema, value, path)
	}
	if full() {
		return out
	}
	if len(s.AnyOf) > 0 {
		if mismatches, none := e.closest(s.AnyOf, value, path, depth); none {
			out = append(out, mismatches...)
		}
	}
	if full() {
		return out
	}
	if len(s.OneOf) > 0 {
		if mismatches, none := e.closest(s.OneOf, value, path, depth); none {
			out = append(out, mismatches...)
		}
	}
	if full() {
		return out
	}
	if s.Not != nil && len(e.check(s.Not, value, path, depth+1)) == 0 {
		add("matches a shape the schema forbids")
	}

	if object, ok := value.(map[string]any); ok {
		for _, name := range s.Required {
			if _, present := object[name]; !present {
				out = append(out, schemaMismatch{path: joinSchemaPath(path, name), problem: "is required"})
			}
		}
		for name, subschema := range s.Properties {
			value, present := object[name]
			if !present {
				continue
			}
			child(subschema, value, joinSchemaPath(path, name))
			if full() {
				return out
			}
		}
		for _, name := range sortedObjectKeys(object) {
			value := object[name]
			if _, known := s.Properties[name]; known {
				continue
			}
			matchedPattern := false
			for _, pattern := range sortedPatterns(s.PatternProperties) {
				if !pattern.re.MatchString(name) {
					continue
				}
				matchedPattern = true
				child(pattern.schema, value, joinSchemaPath(path, name))
			}
			if matchedPattern || full() {
				continue
			}
			if s.AdditionalProperties != nil {
				if isDeniedSchema(s.AdditionalProperties) {
					out = append(out, schemaMismatch{path: path, problem: fmt.Sprintf("unexpected property %q", name)})
				} else {
					child(s.AdditionalProperties, value, joinSchemaPath(path, name))
				}
			}
		}
		if full() {
			return out
		}
		if s.MinProperties != nil && len(object) < *s.MinProperties {
			add(fmt.Sprintf("has %d properties, minProperties is %d", len(object), *s.MinProperties))
		}
		if s.MaxProperties != nil && len(object) > *s.MaxProperties {
			add(fmt.Sprintf("has %d properties, maxProperties is %d", len(object), *s.MaxProperties))
		}
		if s.PropertyNames != nil {
			for _, name := range sortedObjectKeys(object) {
				child(s.PropertyNames, name, path)
				if full() {
					return out
				}
			}
		}
	}

	if array, ok := value.([]any); ok {
		if s.MinItems != nil && len(array) < *s.MinItems {
			add(fmt.Sprintf("has %d items, minItems is %d", len(array), *s.MinItems))
		}
		if s.MaxItems != nil && len(array) > *s.MaxItems {
			add(fmt.Sprintf("has %d items, maxItems is %d", len(array), *s.MaxItems))
		}
		if full() {
			return out
		}
		for i, subschema := range s.PrefixItems {
			if i >= len(array) {
				break
			}
			child(subschema, array[i], fmt.Sprintf("%s[%d]", path, i))
			if full() {
				return out
			}
		}
		start := min(len(s.PrefixItems), len(array))
		if len(s.ItemsArray) > 0 {
			for i, subschema := range s.ItemsArray[start:] {
				if start+i >= len(array) {
					break
				}
				child(subschema, array[start+i], fmt.Sprintf("%s[%d]", path, start+i))
				if full() {
					return out
				}
			}
			start = min(start+len(s.ItemsArray), len(array))
			for ; start < len(array) && s.AdditionalItems != nil; start++ {
				child(s.AdditionalItems, array[start], fmt.Sprintf("%s[%d]", path, start))
				if full() {
					return out
				}
			}
		} else if s.Items != nil {
			for i := start; i < len(array); i++ {
				child(s.Items, array[i], fmt.Sprintf("%s[%d]", path, i))
				if full() {
					return out
				}
			}
		}
	}
	return out
}

// closest reports the mismatches of the branch that came nearest to fitting,
// when no shape in anyOf or oneOf fits at all.
func (e *schemaExplainer) closest(branches []*jsonschema.Schema, value any, path string, depth int) ([]schemaMismatch, bool) {
	var best []schemaMismatch
	for _, branch := range branches {
		mismatches := e.check(branch, value, path, depth+1)
		if len(mismatches) == 0 {
			return nil, false
		}
		if best == nil || len(mismatches) < len(best) {
			best = mismatches
		}
	}
	if len(best) == 0 {
		best = []schemaMismatch{{path: path, problem: "does not match any of the allowed shapes"}}
	} else if len(branches) > 1 {
		best[0].problem += fmt.Sprintf(" (none of %d allowed shapes matched)", len(branches))
	}
	return best, true
}

// deref follows a local $ref. The schema library resolves references, but it
// keeps them private; tool schemas hold their definitions in the same document,
// so a JSON pointer is enough. A remote or broken reference is left unexplained
// and the library's own message stands.
func (e *schemaExplainer) deref(ref string) *jsonschema.Schema {
	target, cached := e.refs[ref]
	if cached {
		return target
	}
	var resolved *jsonschema.Schema
	if e.document != nil && strings.HasPrefix(ref, "#") {
		if value, ok := jsonPointer(e.document, strings.TrimPrefix(ref, "#")); ok {
			raw, err := json.Marshal(value)
			var schema jsonschema.Schema
			if err == nil && json.Unmarshal(raw, &schema) == nil {
				resolved = &schema
			}
		}
	}
	e.refs[ref] = resolved
	return resolved
}

func jsonPointer(document any, pointer string) (any, bool) {
	if fragment, err := url.PathUnescape(pointer); err == nil {
		pointer = fragment
	}
	if pointer == "" {
		return document, true
	}
	if !strings.HasPrefix(pointer, "/") {
		return nil, false
	}
	node := document
	for _, segment := range strings.Split(pointer[1:], "/") {
		segment = strings.ReplaceAll(strings.ReplaceAll(segment, "~1", "/"), "~0", "~")
		switch container := node.(type) {
		case map[string]any:
			value, ok := container[segment]
			if !ok {
				return nil, false
			}
			node = value
		case []any:
			index, err := strconv.Atoi(segment)
			if err != nil || index < 0 || index >= len(container) {
				return nil, false
			}
			node = container[index]
		default:
			return nil, false
		}
	}
	return node, true
}

func schemaTypes(s *jsonschema.Schema) []string {
	if s.Type != "" {
		return []string{s.Type}
	}
	return s.Types
}

func jsonTypeName(value any) string {
	if value == nil {
		return "null"
	}
	if number, ok := value.(json.Number); ok {
		if _, err := number.Int64(); err == nil {
			return "integer"
		}
		return "number"
	}
	switch reflect.ValueOf(value).Kind() {
	case reflect.Bool:
		return "boolean"
	case reflect.String:
		return "string"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "integer"
	case reflect.Float32, reflect.Float64:
		number := reflect.ValueOf(value).Float()
		if number == math.Trunc(number) && !math.IsInf(number, 0) {
			return "integer"
		}
		return "number"
	case reflect.Slice, reflect.Array:
		return "array"
	case reflect.Map:
		return "object"
	}
	return ""
}

func typeMatches(got string, allowed []string) bool {
	for _, want := range allowed {
		if got == want || (got == "integer" && want == "number") {
			return true
		}
	}
	return false
}

func jsonNumber(value any) (float64, bool) {
	switch number := value.(type) {
	case float64:
		return number, true
	case float32:
		return float64(number), true
	case json.Number:
		parsed, err := number.Float64()
		return parsed, err == nil
	case int:
		return float64(number), true
	case int64:
		return float64(number), true
	case int32:
		return float64(number), true
	}
	return 0, false
}

func formatNumber(number float64) string {
	return strconv.FormatFloat(number, 'f', -1, 64)
}

// shortJSON renders a value for a message, short enough to read in a log line.
func shortJSON(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	const limit = 60
	if utf8.RuneCount(raw) <= limit {
		return string(raw)
	}
	runes := []rune(string(raw))
	return string(runes[:limit]) + "…"
}

// equalJSON compares two decoded JSON values, treating every number alike.
func equalJSON(a, b any) bool {
	if x, ok := jsonNumber(a); ok {
		y, alsoNumber := jsonNumber(b)
		return alsoNumber && x == y
	}
	switch left := a.(type) {
	case nil:
		return b == nil
	case bool:
		right, ok := b.(bool)
		return ok && left == right
	case string:
		right, ok := b.(string)
		return ok && left == right
	case []any:
		right, ok := b.([]any)
		if !ok || len(left) != len(right) {
			return false
		}
		for i := range left {
			if !equalJSON(left[i], right[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		right, ok := b.(map[string]any)
		if !ok || len(left) != len(right) {
			return false
		}
		for key, value := range left {
			other, present := right[key]
			if !present || !equalJSON(value, other) {
				return false
			}
		}
		return true
	}
	return reflect.DeepEqual(a, b)
}

// isDeniedSchema reports JSON Schema's "false" schema, which denies every value.
func isDeniedSchema(s *jsonschema.Schema) bool {
	return s != nil && s.Not != nil && reflect.ValueOf(*s.Not).IsZero() &&
		len(s.Properties) == 0 && len(s.Required) == 0 && s.Type == "" && len(s.Types) == 0
}

type schemaPattern struct {
	expression string
	re         *regexp.Regexp
	schema     *jsonschema.Schema
}

func sortedPatterns(patterns map[string]*jsonschema.Schema) []schemaPattern {
	out := make([]schemaPattern, 0, len(patterns))
	for expression, schema := range patterns {
		re, err := regexp.Compile(expression)
		if err != nil {
			continue
		}
		out = append(out, schemaPattern{expression: expression, re: re, schema: schema})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].expression < out[j].expression })
	return out
}

func sortedObjectKeys(object map[string]any) []string {
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func joinSchemaPath(base, name string) string {
	if base == "" {
		return name
	}
	return base + "." + name
}
