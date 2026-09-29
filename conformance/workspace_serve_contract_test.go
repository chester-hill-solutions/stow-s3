package conformance_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"time"
)

func (r *contractRun) performServe(step contractStep) contractOutcome {
	r.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, r.binary, r.argv(step)...)
	cmd.Env = r.childEnv()
	var stderr strings.Builder
	cmd.Stderr = &stderr
	output, err := cmd.StdoutPipe()
	if err != nil {
		r.t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		r.t.Fatal(err)
	}
	result := contractOutcome{stdout: document{}}
	decodeErr := json.NewDecoder(output).Decode(&result.stdout)
	_ = cmd.Process.Signal(os.Interrupt)
	waitErr := cmd.Wait()
	result.stderr = stderr.String()
	if decodeErr != nil || waitErr != nil {
		result.code = 1
	}
	return result
}
