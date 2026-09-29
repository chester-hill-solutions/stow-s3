package conformance_test

import (
	"strings"
	"testing"
)

// TestWorkspaceContractHoldsInTheCLI runs the workspace contract in
// conformance/workspace/cases.json against the stow-s3 binary.
//
// The other two drivers for this contract are the TypeScript and Python wrappers.
// They are not redundant with this one: this asserts the engine is self-consistent
// and the case file true, and theirs assert that a wrapper passes the same document
// through unchanged. Both wrappers were wrong in the same way, so the engine being
// correct is not evidence about them.
//
// Each step is a subtest so a failure names the property that broke, and the pass is
// one linear run: the steps share a registry, and a registry is state.
func TestWorkspaceContractHoldsInTheCLI(t *testing.T) {
	contract := loadContract(t)
	run := newContractRun(t)

	for _, step := range contract.Steps {
		t.Run(stepID(step), func(t *testing.T) {
			if run.broken != "" {
				t.Skipf("an earlier step already failed (%s), so the rest of the scenario would only report its consequences", run.broken)
			}
			// The run is shared and the captures belong to the scenario rather than
			// to a subtest, which is what lets a later step use an identifier an
			// earlier one produced. The scenario is therefore one linear pass and
			// cannot be run step by step.
			run.t = t
			// Cleanup rather than a statement after the judgement, because a
			// Fatalf inside judge ends the subtest there and would leave the flag
			// unset — which is exactly when the cascade guard is needed most.
			t.Cleanup(func() {
				if t.Failed() {
					run.broken = step.ID
				}
			})
			// Resolved once, here, and handed to both halves. A step's text is
			// resolved at the only point where every capture it might name already
			// exists, and perform and judge then read the same resolved step rather
			// than each resolving it themselves.
			step := run.resolve(step)
			outcome := run.perform(step)
			run.judge(step, outcome)
			run.checkDocumentFields(step)
		})
	}
}

// stepID turns a step's declared prose into a subtest name. The contract is
// written as sentences because a step that reads as a claim is one a reviewer can
// check against the code, and Go wants a name without the punctuation.
func stepID(step contractStep) string {
	var b strings.Builder
	for _, r := range step.ID {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}
