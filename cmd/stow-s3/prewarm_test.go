package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/runthrough"
)

// The message has to say why, or the refusal reads as pedantry the next person
// removes to make a glob work.
func TestPrewarmRefusesAPatternAndSaysWhy(t *testing.T) {
	for _, pattern := range []string{
		"models/*", "*.bin", "models/bert?.bin", "models/[ab].bin", "logs/*,models/a.bin",
	} {
		t.Run(pattern, func(t *testing.T) {
			_, err := readPrewarmKeys(pattern, "")
			if err == nil {
				t.Fatalf("a pattern was accepted, and this verb exists to refuse one: %q", pattern)
			}
			message := err.Error()
			if !strings.Contains(message, "pattern") {
				t.Errorf("the refusal %q does not say the key looked like a pattern", message)
			}
			if !strings.Contains(message, "exact keys") {
				t.Errorf("the refusal %q does not say that this verb takes exact keys", message)
			}
		})
	}
}

// A key that merely contains a glob metacharacter is a different question, and the
// answer is that the verb has no way to know. Refusing it would make the verb
// unusable against real buckets, and accepting it is the only honest option. What
// matters is that the decision is recorded rather than accidental: this test pins
// the behaviour so a future tightening is a deliberate change.
func TestPrewarmRefusesAPatternEvenWhenOnlyOneKeyIsNamed(t *testing.T) {
	// A single key and a pattern: there is no "the rest of the list" for the
	// expansion to be confused with, so the refusal has to come from the key itself
	// rather than from how many keys there are.
	if _, err := readPrewarmKeys("a*b", ""); err == nil {
		t.Error("a single key containing * was accepted")
	}
}

// The reader takes a list, and a list has to survive the shapes an operator actually
// produces: a comma on one line, a file of lines, comments, blank lines, and the
// same key twice. The shapes below are the ones a shell or an editor produces by
// accident, and a reader that mishandles one of them either warms the wrong key or
// refuses a list that was fine.
func TestPrewarmReadsTheListShapesAnOperatorProduces(t *testing.T) {
	work := t.TempDir()
	file := filepath.Join(work, "keys.txt")
	body := "# a comment, because the file is edited by a person\n" +
		"models/a.bin\n" +
		"\n" +
		"   models/b.bin   \n" +
		"models/a.bin\n" +
		"models/c.bin,models/d.bin\n"
	if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
		t.Fatalf("write the key file: %v", err)
	}

	fromFile, err := readPrewarmKeys("", file)
	if err != nil {
		t.Fatalf("read the key file: %v", err)
	}
	want := []string{"models/a.bin", "models/b.bin", "models/c.bin", "models/d.bin"}
	if strings.Join(fromFile, ",") != strings.Join(want, ",") {
		t.Errorf("a key file read as %v, want %v", fromFile, want)
	}

	fromFlag, err := readPrewarmKeys("a,b ,c", "")
	if err != nil {
		t.Fatalf("read the flag: %v", err)
	}
	if strings.Join(fromFlag, ",") != "a,b,c" {
		t.Errorf("a comma-separated flag read as %v, want [a b c] with the spaces trimmed", fromFlag)
	}
}

// A comment with a comma in it is the most ordinary comment anybody writes, and a
// comma-first parse cut it in half and warmed the remainder.
func TestPrewarmDoesNotWarmTheTextOfAComment(t *testing.T) {
	file := filepath.Join(t.TempDir(), "keys.txt")
	body := "# model weights, see the runbook for how these are produced\n" +
		"models/a.bin\n" +
		"# a second note, also with a comma\n" +
		"models/b.bin\n"
	if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
		t.Fatalf("write the key file: %v", err)
	}
	keys, err := readPrewarmKeys("", file)
	if err != nil {
		t.Fatalf("read the key file: %v", err)
	}
	if strings.Join(keys, ",") != "models/a.bin,models/b.bin" {
		t.Errorf("the key file read as %v, and a comment is not a key", keys)
	}
}

// A key containing # is a legal S3 key, so the marker is only a marker at the start
// of a line.
func TestPrewarmAcceptsAKeyThatContainsAHash(t *testing.T) {
	keys, err := readPrewarmKeys("builds/#1234/model.bin,release#2/a.bin", "")
	if err != nil {
		t.Fatalf("a legal key containing # was refused: %v", err)
	}
	if len(keys) != 2 {
		t.Errorf("the list is %v, want both keys", keys)
	}
}

func TestPrewarmDropsARepeatedKeyRatherThanRefusingIt(t *testing.T) {
	keys, err := readPrewarmKeys("a,b,a,b,a", "")
	if err != nil {
		t.Fatalf("a repeated key was refused: %v", err)
	}
	if len(keys) != 2 {
		t.Errorf("the list is %v, want the two distinct keys", keys)
	}
}

func TestPrewarmRefusesTwoSourcesForTheSameList(t *testing.T) {
	file := filepath.Join(t.TempDir(), "keys.txt")
	if err := os.WriteFile(file, []byte("a\n"), 0o600); err != nil {
		t.Fatalf("write the key file: %v", err)
	}
	_, err := readPrewarmKeys("a,b", file)
	if err == nil {
		t.Fatal("two sources for the same list were accepted, and one of them will be silently ignored")
	}
	if !strings.Contains(err.Error(), "--keys-file") {
		t.Errorf("the refusal %q does not name the two flags involved", err)
	}
}

func TestPrewarmRefusesAKeyFileItCannotRead(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.txt")
	_, err := readPrewarmKeys("", missing)
	if err == nil {
		t.Fatal("a key file that does not exist was accepted")
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("the refusal %q does not name the path, so the operator cannot tell a typo from a permission problem", err)
	}
}

// Three counts, because a key the bucket does not have is a failure and a key the
// adapter refused to ask for is a configured choice, and lumping them together costs
// the operator a nonzero exit for doing what they asked.
func TestPrewarmCountsWarmedFailedAndSkippedApart(t *testing.T) {
	entries := []runthrough.PrewarmResult{
		{Key: "warm", Cached: true, Bytes: 10},
		{Key: "gone", Reason: "not-found"},
		{Key: "evicted", Reason: "not in the cache: the limits did not leave room for it"},
		{Key: "unreached", Reason: "offline"},
		{Key: "also-warm", Cached: true, Bytes: 20},
	}
	out := summarizePrewarm("datasets", entries)

	if out.Warmed != 2 {
		t.Errorf("warmed is %d, want 2: %+v", out.Warmed, out.Results)
	}
	if out.Failed != 2 {
		t.Errorf("failed is %d, want 2: a missing key and an evicted key are both failures and an offline one is not: %+v", out.Failed, out.Results)
	}
	if out.Skipped != 1 {
		t.Errorf("skipped is %d, want 1: a key the adapter refused to ask for is a configured choice, not a failure: %+v", out.Skipped, out.Results)
	}
	if out.Bucket != "datasets" || out.Version != 1 {
		t.Errorf("the report does not say which bucket and which version it describes: %+v", out)
	}
	if len(out.Results) != len(entries) {
		t.Fatalf("the report has %d entries for %d keys: a key with no entry cannot be counted, checked, or read", len(out.Results), len(entries))
	}
	for i, entry := range out.Results {
		if entry.Reason != entries[i].Reason {
			t.Errorf("entry %d has reason %q, want %q: the report drops the only thing that says why a key is not warm", i, entry.Reason, entries[i].Reason)
		}
	}
	if out.Results[0].Bytes != 10 {
		t.Errorf("a warm key reports %d bytes, want 10: the size is what an operator sizes the cache from", out.Results[0].Bytes)
	}
}

func TestPrewarmRefusesIncompleteArgumentsBeforeTouchingAStore(t *testing.T) {
	for name, testCase := range map[string]struct {
		args []string
		want string
	}{
		"no bucket":       {args: []string{"--keys", "a"}, want: "--bucket"},
		"no keys":         {args: []string{"--bucket", "datasets"}, want: "--keys"},
		"no keys at all":  {args: []string{"--bucket", "datasets"}, want: "--keys"},
		"an empty keyset": {args: []string{"--bucket", "datasets", "--keys", " , "}, want: "--keys"},
	} {
		t.Run(name, func(t *testing.T) {
			err := prewarm(testCase.args)
			if err == nil {
				t.Fatalf("prewarm %v succeeded", testCase.args)
			}
			if !strings.Contains(err.Error(), testCase.want) {
				t.Errorf("the refusal %q does not name %q, so the operator is left guessing which flag they forgot", err, testCase.want)
			}
		})
	}
}
