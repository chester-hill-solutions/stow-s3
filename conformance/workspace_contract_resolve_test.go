package conformance_test

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

// Two pieces of the driver are load-bearing and the contract passing does not prove
// either. A resolution pass that skipped a field would leave a literal {{work}} in a
// command line, a path, or an expectation, and the step would fail on the
// substitution rather than on the property it exists to assert. And the table of
// verbs that take a registry directory is a hand-maintained statement about the
// CLI's own flags, which is a claim until something watches it fail.

// newResolveRun is a contractRun with no binary behind it.
//
// The contract run builds stow-s3, which is right when steps are about the CLI and
// wrong for a test about reading the case file. Nothing here runs a verb.
func newResolveRun(t *testing.T) *contractRun {
	t.Helper()
	work := t.TempDir()
	return &contractRun{
		t:        t,
		work:     work,
		captures: map[string]string{"work": work, "root": "/tmp/workspace", "team": "platform"},
	}
}

// resolvableText is every field of a step that resolution is responsible for.
//
// It excludes the three kinds of field that legitimately keep their text, and the
// exclusions are the design rather than an omission. A write body and a seed body
// are content the case file states, not paths it names, so a body containing braces
// is a body. A rename's Path and To name paths inside the transported document
// rather than on this machine, so no capture produces either. A field left out of
// this list is a field nothing checks.
func resolvableText(step contractStep) map[string]string {
	texts := map[string]string{
		"root": step.Root, "team": step.Team,
		"alsoWritten": step.AlsoWritten, "tamper": step.Tamper,
	}
	for name, value := range step.Args {
		texts["arg "+name] = value
	}
	for i, write := range step.Write {
		texts["write path "+strconv.Itoa(i)] = write.Path
	}
	if step.Manifest != nil {
		texts["manifest root"] = step.Manifest.Root
		texts["manifest team"] = step.Manifest.Team
		for i, input := range step.Manifest.Inputs {
			texts["input destination "+strconv.Itoa(i)] = input.Destination
		}
	}
	if step.RenameInTransit != nil {
		texts["rename source"] = step.RenameInTransit.From
	}
	for path, want := range step.Expect {
		if want.Equals != nil {
			texts["expect "+path+" equals"] = string(*want.Equals)
		}
		if want.NotEquals != nil {
			texts["expect "+path+" notEquals"] = string(*want.NotEquals)
		}
	}
	return texts
}

// aStepThatNamesEveryField is a step with a reference in every place one can go.
// Every field is written out rather than generated so that adding a field to a step
// and forgetting to extend this is visible as an unused field, not as silence.
func aStepThatNamesEveryField(t *testing.T, work string) contractStep {
	t.Helper()
	return contractStep{
		ID: "everything", Verb: "apply", Registry: contractRegistryA,
		Team: "{{team}}", Root: "{{root}}",
		Args: map[string]string{
			"id":           "{{root}}",
			"delta":        "{{work}}/delta.json",
			"registry-dir": "{{work}}/registry-a",
		},
		AlsoWritten: "{{work}}/handoff.json",
		Tamper:      "{{work}}/delta.json",
		Write:       []contractWrite{{Path: "{{root}}/notes.md", Body: "a body naming {{root}} on purpose"}},
		Manifest: &contractManifestSpec{
			Root: "{{root}}", Team: "{{team}}",
			Inputs: []contractInput{{Body: "seed for {{root}}", Destination: "{{root}}/spec.md"}},
		},
		RenameInTransit: &contractRename{From: "{{work}}/a.json", Path: "old.md", To: "new.md"},
		Expect: map[string]contractExpectation{
			"results[0].path": {Equals: rawJSON(t, "{{root}}/notes.md")},
			"results[1].path": {NotEquals: rawJSON(t, "{{root}}/other.md")},
			"results[2].path": {Matches: "^/tmp/workspace"},
		},
	}
}

// Resolve has to reach every field. The previous arrangement resolved at each place a
// resolved value was wanted, and one of those places did not: the registry directory
// was read from the raw arguments, so a step was pointed at a directory literally
// named "{{registryA}}" and `collect` reported an empty registry rather than an
// error. Six call sites each remembering is six chances to forget, and this is the
// test that names the sixth.
func TestResolveLeavesNoReferenceInAnyFieldOfAStep(t *testing.T) {
	run := newResolveRun(t)
	step := aStepThatNamesEveryField(t, run.work)

	resolved := run.resolve(step)

	for field, text := range resolvableText(resolved) {
		if strings.Contains(text, "{{") {
			t.Errorf("%s still names a reference after resolution: %q. A step that arrives unresolved reaches the verb, or the comparison, as the literal text.",
				field, text)
		}
	}
}

// The excluded fields keep their text on purpose, so this is the other half of the
// list above. A seed body or a written file that happens to contain braces is a
// body, and resolving it would rewrite what the case file says the workspace
// contains; a rename's Path and To are paths inside the transported document, which
// no capture on this machine produces.
func TestResolveLeavesStatedBodiesAlone(t *testing.T) {
	run := newResolveRun(t)
	resolved := run.resolve(aStepThatNamesEveryField(t, run.work))

	if got := resolved.Write[0].Body; got != "a body naming {{root}} on purpose" {
		t.Errorf("a stated body is %q: a body is content, not a path, and resolving it would rewrite what the case file says", got)
	}
	if got := resolved.Manifest.Inputs[0].Body; got != "seed for {{root}}" {
		t.Errorf("a stated seed body is %q, and a body is content rather than a path", got)
	}
	if got := resolved.RenameInTransit.Path; got != "old.md" {
		t.Errorf("a rename's path is %q, and it names a path inside the transported document", got)
	}
	if got := resolved.RenameInTransit.To; got != "new.md" {
		t.Errorf("a rename's destination is %q, and it is a path the document will claim rather than one on this machine", got)
	}
}

// The fields are named one by one as well, so a failure says which field and not
// only that something is left. The property test above says a reference survived;
// this one says which.
func TestResolveReachesEachDeclaredField(t *testing.T) {
	run := newResolveRun(t)
	resolved := run.resolve(aStepThatNamesEveryField(t, run.work))

	for _, check := range []struct{ what, got, want string }{
		{"team", resolved.Team, "platform"},
		{"root", resolved.Root, "/tmp/workspace"},
		{"arg id", resolved.Args["id"], "/tmp/workspace"},
		{"arg delta", resolved.Args["delta"], run.work + "/delta.json"},
		{"arg registry-dir", resolved.Args["registry-dir"], run.work + "/registry-a"},
		{"alsoWritten", resolved.AlsoWritten, run.work + "/handoff.json"},
		{"tamper", resolved.Tamper, run.work + "/delta.json"},
		{"write path", resolved.Write[0].Path, "/tmp/workspace/notes.md"},
		{"manifest root", resolved.Manifest.Root, "/tmp/workspace"},
		{"manifest team", resolved.Manifest.Team, "platform"},
		{"input destination", resolved.Manifest.Inputs[0].Destination, "/tmp/workspace/spec.md"},
		{"rename source", resolved.RenameInTransit.From, run.work + "/a.json"},
		// Only the source names a path on this machine. Path names a path inside the
		// transported document and To is the path that document will claim instead.
		{"rename path", resolved.RenameInTransit.Path, "old.md"},
		{"rename to", resolved.RenameInTransit.To, "new.md"},
	} {
		if check.got != check.want {
			t.Errorf("%s is %q, want %q", check.what, check.got, check.want)
		}
	}
	// A reference inside an expected value is resolved inside the JSON string, and
	// the result is still the same JSON. A step that expects "the root the manifest
	// named" is stating a fact about the contract, and comparing it against a
	// literal {{work}} would fail on the substitution rather than on the property.
	if got := string(*resolved.Expect["results[0].path"].Equals); got != `"/tmp/workspace/notes.md"` {
		t.Errorf("the expected value is %s, want the reference resolved inside the JSON string", got)
	}
	if got := string(*resolved.Expect["results[1].path"].NotEquals); got != `"/tmp/workspace/other.md"` {
		t.Errorf("the notEquals value is %s, want the reference resolved inside the JSON string", got)
	}
	if resolved.Expect["results[2].path"].Matches != "^/tmp/workspace" {
		t.Errorf("the pattern is %q, and a pattern is not a path and was never substituted", resolved.Expect["results[2].path"].Matches)
	}
}

// resolve must not edit the case file the run loaded. A second step sharing a map
// would otherwise see text that had already been resolved once, and re-resolving an
// already-resolved value is how a driver ends up reporting a substitution that did
// not happen.
func TestResolveDoesNotEditTheStepItWasGiven(t *testing.T) {
	run := newResolveRun(t)
	step := aStepThatNamesEveryField(t, run.work)

	run.resolve(step)

	for field, text := range resolvableText(step) {
		if !strings.Contains(text, "{{") {
			t.Errorf("%s is %q in the caller's step after resolution: resolve edited the case file rather than returning a resolved copy", field, text)
		}
	}
}

// checkContractRegistryFlags exists because contractRegistryDirFlags is a
// hand-maintained statement about the CLI's own flags. This is the part that says
// the statement is still true, and it has to be able to fail: a verb added to the
// case file without a table entry runs against the default registry and reports an
// empty one, which reads as a contract failure rather than as a missing entry.
func TestCheckContractRegistryFlagsNamesAVerbTheTableDoesNotCover(t *testing.T) {
	steps := []contractStep{{
		ID: "a step in registry a", Verb: "newverb", Registry: contractRegistryA,
	}}
	problems := checkContractRegistryFlags(steps)
	if len(problems) != 1 {
		t.Fatalf("a verb the table does not cover produced %d problems, want 1: %v", len(problems), problems)
	}
	// The message has to name the table, because that is the thing to fix, and it has
	// to name the verb, because that is the thing that changed.
	for _, want := range []string{"newverb", "contractRegistryDirFlags"} {
		if !strings.Contains(problems[0], want) {
			t.Errorf("the problem %q does not mention %q, so it does not say what to fix", problems[0], want)
		}
	}
}

// The three ways a step reaches a registry all have to be silent, because all three
// are how the real case file works. A guard that fired on the case file would be a
// guard nobody keeps.
func TestCheckContractRegistryFlagsAcceptsTheThreeWaysAStepReachesARegistry(t *testing.T) {
	steps := []contractStep{
		{ID: "covered by the table", Verb: "apply", Registry: contractRegistryA},
		{ID: "names its own", Verb: "collect", Args: map[string]string{"registry-dir": "{{registryA}}"}},
		{ID: "reaches it through a manifest", Verb: "prepare", Registry: contractRegistryA,
			Manifest: &contractManifestSpec{Root: "{{work}}/workspace"}},
		{ID: "is not in registry a at all", Verb: "adopt", Registry: "default"},
	}
	if problems := checkContractRegistryFlags(steps); len(problems) > 0 {
		t.Errorf("the driver raised %d problems about steps that each reach a registry: %v", len(problems), problems)
	}
}

func rawJSON(t *testing.T, text string) *json.RawMessage {
	t.Helper()
	encoded, err := json.Marshal(text)
	if err != nil {
		t.Fatalf("encode %q: %v", text, err)
	}
	raw := json.RawMessage(encoded)
	return &raw
}
