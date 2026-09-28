package conformance_test

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// judge checks one step's outcome against what the case file declared. Every
// judgement is a comparison the three drivers make identically, so a client that
// returns something different fails here rather than in a wrapper's own tests.
func (r *contractRun) judge(step contractStep, out contractOutcome) {
	r.t.Helper()

	if step.Fail != nil {
		r.judgeRefusal(step, out)
		return
	}
	if out.code != 0 {
		r.t.Fatalf("step %q: %s was supposed to succeed.\n%s", step.ID, step.Verb, describeOutcome(out))
	}
	if len(step.Expect) == 0 && step.Verb != "noop" {
		r.t.Fatalf("step %q declares no expectations, so it asserts nothing", step.ID)
	}
	if step.AlsoWritten != "" {
		r.judgeAlsoWritten(step, out)
	}
	for _, path := range sortedExpectations(step.Expect) {
		r.judgeField(step, out.stdout, path, r.substituteExpectation(step.Expect[path]))
	}
	for _, name := range sortedArgs(step.Capture) {
		r.capture(step, name, step.Capture[name], out.stdout)
	}
}

// judgeRefusal asserts that a step failed the way it said it would, and that it
// failed. A step that expects a refusal and gets a success is the dangerous
// direction: it means the guard the product relies on is not there.
func (r *contractRun) judgeRefusal(step contractStep, out contractOutcome) {
	r.t.Helper()
	if out.code == 0 {
		r.t.Fatalf("step %q: %s was supposed to be refused and it succeeded.\n%s",
			step.ID, step.Verb, describeOutcome(out))
	}
	if step.Fail.Contains != "" && !strings.Contains(out.stderr, step.Fail.Contains) {
		r.t.Fatalf("step %q: the refusal does not say %q.\nstderr: %s", step.ID, step.Fail.Contains, out.stderr)
	}
}

// judgeAlsoWritten asserts the file a step asked the verb to write, and asserts the
// right relationship to what the verb returned.
//
// For a verb that writes the document itself, the file and the returned value must
// be the same thing — that is the regression which motivated this file, stated once:
// both client wrappers used to decode stdout for `handoff --output`, so asking for
// a path produced a JSON parse error and the caller never got the document it had
// just asked for. For a verb that writes a document *and* prints a result, the two
// are different documents and comparing them would be wrong.
func (r *contractRun) judgeAlsoWritten(step contractStep, out contractOutcome) {
	r.t.Helper()
	written := r.readDocument(r.workPath(step.AlsoWritten), step.ID)

	if step.Returns != contractReturnsFile {
		if strings.TrimSpace(out.rawStdout) == "" {
			r.t.Errorf("step %q: the verb wrote %q and printed nothing, but the contract says the caller gets its result from stdout",
				step.ID, step.AlsoWritten)
		}
		return
	}
	if strings.TrimSpace(out.rawStdout) != "" {
		r.t.Errorf("step %q: the verb wrote %q and also printed something, but the contract says the document goes to the file and nowhere else.\nstdout: %s",
			step.ID, step.AlsoWritten, out.rawStdout)
	}
	if !sameDocument(written, out.stdout) {
		r.t.Fatalf("step %q: the document written to %q is not the one that was returned.\non disk: %s\nreturned: %s",
			step.ID, step.AlsoWritten, indent(written), indent(out.stdout))
	}
}

func (r *contractRun) capture(step contractStep, name, field string, from document) {
	r.t.Helper()
	raw, found := lookupField(from, field)
	if !found {
		r.t.Fatalf("step %q: nothing captured %q because the result has no %q.\n%s",
			step.ID, name, field, indent(from))
	}
	text, err := decodeString(raw)
	if err != nil {
		r.t.Fatalf("step %q: %q holds %s, and only a string can be substituted into a later argument",
			step.ID, field, string(raw))
	}
	r.captures[name] = text
}

// substituteExpectation resolves the {{...}} references inside an expected value,
// so a step can state "the root the manifest named" rather than repeating the path
// the manifest already declared. Without this the first step compared a real
// /tmp/... path against the literal text {{work}}/workspace and failed on the
// substitution rather than on the contract.
func (r *contractRun) substituteExpectation(want contractExpectation) contractExpectation {
	want.Equals = r.substituteValue(want.Equals)
	want.NotEquals = r.substituteValue(want.NotEquals)
	return want
}

func (r *contractRun) substituteValue(raw *json.RawMessage) *json.RawMessage {
	if raw == nil {
		return nil
	}
	text, err := decodeString(*raw)
	if err != nil {
		return raw
	}
	resolved, err := json.Marshal(r.substitute(text))
	if err != nil {
		return raw
	}
	encoded := json.RawMessage(resolved)
	return &encoded
}

// judgeField checks one declared field against one expectation, and reports
// everything that is wrong with it rather than stopping at the first thing.
func (r *contractRun) judgeField(step contractStep, from document, path string, want contractExpectation) {
	r.t.Helper()
	raw, found := r.lookupDeclared(step, from, path)
	if want.Absent {
		if found {
			r.t.Errorf("step %q: %q should be absent, but it is %s", step.ID, path, raw)
		}
		return
	}
	if !found {
		r.t.Errorf("step %q: %q is missing from the result.\ngot: %s", step.ID, path, indent(from))
		return
	}
	for _, check := range []func(){
		func() { r.checkEquals(step, path, raw, want) },
		func() { r.checkNotEquals(step, path, raw, want) },
		func() { r.checkPattern(step, path, raw, want) },
		func() { r.checkLength(step, path, raw, want) },
		func() { r.checkMinLength(step, path, raw, want) },
		func() { r.checkMin(step, path, raw, want) },
	} {
		check()
	}
}

func (r *contractRun) checkEquals(step contractStep, path string, raw json.RawMessage, want contractExpectation) {
	if want.Equals == nil {
		return
	}
	if !sameJSON(*want.Equals, raw) {
		r.t.Errorf("step %q: %q is %s, and the contract says %s", step.ID, path, raw, *want.Equals)
	}
}

func (r *contractRun) checkNotEquals(step contractStep, path string, raw json.RawMessage, want contractExpectation) {
	if want.NotEquals == nil {
		return
	}
	if sameJSON(*want.NotEquals, raw) {
		r.t.Errorf("step %q: %q is %s, and the contract says it must differ from that", step.ID, path, raw)
	}
}

func (r *contractRun) checkPattern(step contractStep, path string, raw json.RawMessage, want contractExpectation) {
	if want.Matches == "" {
		return
	}
	text, err := decodeString(raw)
	if err != nil {
		r.t.Errorf("step %q: %q holds %s, and a pattern needs a string", step.ID, path, raw)
		return
	}
	matched, err := regexp.MatchString(want.Matches, text)
	if err != nil {
		r.t.Fatalf("step %q: %q is not a valid pattern: %v", step.ID, want.Matches, err)
	}
	if !matched {
		r.t.Errorf("step %q: %q is %q, which does not match %q", step.ID, path, text, want.Matches)
	}
}

func (r *contractRun) checkLength(step contractStep, path string, raw json.RawMessage, want contractExpectation) {
	if want.Length == nil {
		return
	}
	if got := lengthOf(raw); got != *want.Length {
		r.t.Errorf("step %q: %q holds %d entries, and the contract says %d", step.ID, path, got, *want.Length)
	}
}

func (r *contractRun) checkMinLength(step contractStep, path string, raw json.RawMessage, want contractExpectation) {
	if want.MinLength == nil {
		return
	}
	if got := lengthOf(raw); got < *want.MinLength {
		r.t.Errorf("step %q: %q holds %d entries, and the contract says at least %d", step.ID, path, got, *want.MinLength)
	}
}

func (r *contractRun) checkMin(step contractStep, path string, raw json.RawMessage, want contractExpectation) {
	if want.Min == nil {
		return
	}
	number, err := decodeNumber(raw)
	if err != nil {
		r.t.Errorf("step %q: %q holds %s, and a bound needs a number", step.ID, path, raw)
		return
	}
	if number < *want.Min {
		r.t.Errorf("step %q: %q is %v, and the contract says at least %v", step.ID, path, number, *want.Min)
	}
}

// lookupDeclared resolves one declared key. A "read" step's keys are file paths and
// are taken literally, because a workspace is full of names that contain dots —
// "seed.txt" is a file, not a field called seed inside a field called txt. Every
// other verb's keys are field paths into the returned document.
func (r *contractRun) lookupDeclared(step contractStep, from document, key string) (json.RawMessage, bool) {
	if step.Verb == "read" {
		raw, ok := from[key]
		return raw, ok
	}
	return lookupField(from, key)
}

// lookupField resolves a dotted path with optional indexes, so a case can say
// "changes[0].path" instead of every driver knowing the shape of a diff. Every
// segment but the last is a container to descend into; the last one is the value.
func lookupField(from document, path string) (json.RawMessage, bool) {
	segments := splitFieldPath(path)
	current := from
	for i, segment := range segments {
		raw, ok := current[segment.name]
		if !ok {
			return nil, false
		}
		for _, index := range segment.indexes {
			var list []json.RawMessage
			if err := json.Unmarshal(raw, &list); err != nil {
				return nil, false
			}
			if index < 0 || index >= len(list) {
				return nil, false
			}
			raw = list[index]
		}
		if i == len(segments)-1 {
			return raw, true
		}
		var next document
		if err := json.Unmarshal(raw, &next); err != nil {
			return nil, false
		}
		current = next
	}
	return nil, false
}

type fieldSegment struct {
	name    string
	indexes []int
}

func splitFieldPath(path string) []fieldSegment {
	var segments []fieldSegment
	for _, part := range strings.Split(path, ".") {
		segment := fieldSegment{name: part}
		if cut := strings.Index(part, "["); cut >= 0 {
			segment.name = part[:cut]
			segment.indexes = parseIndexes(part[cut:])
		}
		segments = append(segments, segment)
	}
	return segments
}

// parseIndexes reads the "[0][1]" tail of a field path. A malformed tail yields no
// indexes rather than an error, so a typo in the contract is reported as a missing
// field — which names the contract — instead of a parse failure that names neither.
func parseIndexes(tail string) []int {
	var indexes []int
	for rest := tail; strings.HasPrefix(rest, "["); {
		closing := strings.Index(rest, "]")
		if closing < 0 {
			return nil
		}
		index, err := strconv.Atoi(rest[1:closing])
		if err != nil {
			return nil
		}
		indexes = append(indexes, index)
		rest = rest[closing+1:]
	}
	return indexes
}

// sameJSON compares two JSON values structurally rather than byte for byte, so two
// documents that differ only in key order or in whitespace are equal. A contract
// that compared bytes would fail on a reformat, which is a difference in encoding
// and not in meaning.
//
// The walk is written over raw JSON rather than over decoded interface values
// because a decoded walk needs a type switch on `any`, and this repository ratchets
// against `any` for good reason: it is where a lost type assertion becomes a silent
// wrong answer. Here every value is either an object, an array, a number, or a
// literal, and each is compared as itself.
func sameJSON(want, got json.RawMessage) bool {
	wantText, gotText := strings.TrimSpace(string(want)), strings.TrimSpace(string(got))
	if wantText == gotText {
		return true
	}
	if isJSONObject(wantText) && isJSONObject(gotText) {
		return sameJSONObject(wantText, gotText)
	}
	if isJSONArray(wantText) && isJSONArray(gotText) {
		return sameJSONArray(wantText, gotText)
	}
	if wantNumber, ok := jsonNumber(wantText); ok {
		gotNumber, gotIsNumber := jsonNumber(gotText)
		return gotIsNumber && wantNumber == gotNumber
	}
	return false
}

func sameDocument(want, got document) bool {
	return sameJSON(mustEncode(want), mustEncode(got))
}

func sameJSONObject(want, got string) bool {
	var wantObject, gotObject document
	if err := json.Unmarshal([]byte(want), &wantObject); err != nil {
		return false
	}
	if err := json.Unmarshal([]byte(got), &gotObject); err != nil {
		return false
	}
	if len(wantObject) != len(gotObject) {
		return false
	}
	for key, wantChild := range wantObject {
		gotChild, ok := gotObject[key]
		if !ok || !sameJSON(wantChild, gotChild) {
			return false
		}
	}
	return true
}

func sameJSONArray(want, got string) bool {
	var wantList, gotList []json.RawMessage
	if err := json.Unmarshal([]byte(want), &wantList); err != nil {
		return false
	}
	if err := json.Unmarshal([]byte(got), &gotList); err != nil {
		return false
	}
	if len(wantList) != len(gotList) {
		return false
	}
	for i := range wantList {
		if !sameJSON(wantList[i], gotList[i]) {
			return false
		}
	}
	return true
}

func isJSONObject(text string) bool { return strings.HasPrefix(text, "{") }
func isJSONArray(text string) bool  { return strings.HasPrefix(text, "[") }

// jsonNumber parses a JSON number, so a contract that says 2 and an engine that
// says 2.0 are equal. Comparing the text instead would make a reformat a
// disagreement about meaning.
func jsonNumber(text string) (float64, bool) {
	number, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return 0, false
	}
	return number, true
}

func lengthOf(raw json.RawMessage) int {
	var list []json.RawMessage
	if err := json.Unmarshal(raw, &list); err == nil {
		return len(list)
	}
	var object document
	if err := json.Unmarshal(raw, &object); err == nil {
		return len(object)
	}
	return -1
}

func decodeString(raw json.RawMessage) (string, error) {
	var text string
	err := json.Unmarshal(raw, &text)
	return text, err
}

func decodeNumber(raw json.RawMessage) (float64, error) {
	var number float64
	err := json.Unmarshal(raw, &number)
	return number, err
}

// The encoders below are separate rather than one generic. A generic needs `any`
// in its constraint, and this repository ratchets against `any` because it is where
// a lost type assertion becomes a silent wrong answer. They are called from a
// helper that has already checked each shape, so an encoding failure is not a case
// worth distinguishing from any other.
func encodeJSONString(value string) json.RawMessage {
	encoded, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage("null")
	}
	return encoded
}

func encodeDocument(value document) json.RawMessage {
	encoded, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage("null")
	}
	return encoded
}

func encodeChangeList(value []json.RawMessage) json.RawMessage {
	encoded, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage("null")
	}
	return encoded
}

func encodeContentMap(value map[string]json.RawMessage) json.RawMessage {
	encoded, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage("null")
	}
	return encoded
}

func mustEncode(value document) json.RawMessage {
	encoded, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage("null")
	}
	return encoded
}

func indent(value document) string {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return string(encoded)
}
