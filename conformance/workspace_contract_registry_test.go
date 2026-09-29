package conformance_test

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

// contractRegistryDirFlags names the verbs the driver points at its own registry
// directory, because the contract runs every step against one registry and a
// registry is state. prepare is absent on purpose: its registry comes from the
// manifest the driver writes.
//
// It drifted once: adopt and prune both took --registry-dir, neither was listed, and
// checkContractRegistryFlags did not fire for either — the prune step names its own
// registry directory and the adopt step runs in the default registry, so neither was
// the uncovered shape that guard looks for. A guard keyed on the case file cannot
// see a verb it happens not to exercise, hence the binary check below.
var contractRegistryDirFlags = map[string]bool{
	"adopt": true, "apply": true, "checkpoint": true, "collect": true,
	"delta": true, "destroy": true, "diff": true, "export": true,
	"handoff": true, "import": true, "list": true, "prune": true,
	"restore": true, "resume": true, "serve": true,
}

// workspaceVerbPattern reads the verb list out of the binary's own usage line, so
// the check below is about what the binary can do rather than a second list of verbs
// maintained here.
var workspaceVerbPattern = regexp.MustCompile(`workspace <([a-z|]+)>`)

// registryFlagsFromBinary asks the binary which verbs take a registry directory.
// A per-verb `-h` exits 1, because the flag package treats -h as ErrHelp; the flags
// on stderr are the answer, so the exit status is not checked.
func registryFlagsFromBinary(t *testing.T, binary string) (map[string]bool, []string) {
	t.Helper()
	usage := exec.Command(binary, "workspace", "--help")
	stdout, err := usage.Output()
	if err != nil {
		t.Fatalf("ask the binary for its workspace verbs: %v", err)
	}
	match := workspaceVerbPattern.FindSubmatch(stdout)
	if match == nil {
		t.Fatalf("the binary printed no workspace verb list, so nothing can be checked against it:\n%s", stdout)
	}
	verbs := strings.Split(string(match[1]), "|")
	takes := map[string]bool{}
	for _, verb := range verbs {
		help := exec.Command(binary, "workspace", verb, "-h")
		stderr := &strings.Builder{}
		help.Stderr = stderr
		_ = help.Run()
		if strings.Contains(stderr.String(), "-registry-dir") {
			takes[verb] = true
		}
	}
	return takes, verbs
}

// checkRegistryFlagsAgainstBinary reports every verb whose registry-directory
// handling disagrees with contractRegistryDirFlags, in both directions. The omitted
// direction is the quiet one — nothing fails until a step is added — and the other
// fails immediately, which is why it was never the problem worth catching.
func checkRegistryFlagsAgainstBinary(takes map[string]bool, verbs []string) []string {
	var problems []string
	for _, verb := range verbs {
		want := contractRegistryDirFlags[verb]
		got := takes[verb]
		if want == got {
			continue
		}
		if got {
			problems = append(problems, fmt.Sprintf(
				"workspace %s takes --registry-dir but is not in contractRegistryDirFlags, so a step running it in registry %q is pointed at the default registry instead",
				verb, contractRegistryA))
			continue
		}
		problems = append(problems, fmt.Sprintf(
			"workspace %s is in contractRegistryDirFlags but does not take --registry-dir, so the driver hands it a flag it will refuse",
			verb))
	}
	return problems
}
