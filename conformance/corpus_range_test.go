package conformance_test

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// runCorpusRangeGet reads part of an object the setup seeded.
//
// The range is written into the corpus as the header a client would send, and the
// answer is checked on three surfaces: status, bytes, and the whole Content-Range.
// Status and body together are what a client reads, and they are not sufficient: a
// server can return the right bytes for a range it mis-parsed, or a correct
// Content-Range beside a body from the wrong offset. Asserting the full
// `bytes <first>-<last>/<size>` string also pins the total, the part a truncated read
// reports wrongly most often.
func runCorpusRangeGet(corpusContext *sharedCorpusContext) {
	corpusContext.t.Helper()
	testCase := corpusContext.testCase

	output, err := corpusContext.env.Client.GetObject(corpusContext.ctx, &s3.GetObjectInput{
		Bucket: aws.String(corpusContext.bucket(testCase.Bucket)),
		Key:    aws.String(testCase.Key),
		Range:  aws.String(testCase.Range),
	})

	if testCase.Expect.Status == http.StatusRequestedRangeNotSatisfiable {
		assertCorpusError(corpusContext.t, err, testCase.Expect)
		assertCorpusRangeRefusalTotal(corpusContext, testCase)
		return
	}
	if err != nil {
		corpusContext.t.Fatalf("GetObject with Range %q: %v", testCase.Range, err)
	}
	assertCorpusStatus(corpusContext.t, corpusContext.env, testCase.Expect.Status)
	assertCorpusGetOutput(corpusContext, output, testCase.Expect)
	if got := aws.ToString(output.ContentRange); got != testCase.Expect.ContentRange {
		corpusContext.t.Fatalf("Content-Range = %q, want %q", got, testCase.Expect.ContentRange)
	}
}

// assertCorpusRangeRefusalTotal re-reads the 416 through a HeadObject-sized
// probe to confirm the total the refusal reported is the real object size, and
// not a number the range parser produced on its way to refusing.
//
// A 416 carries `Content-Range: bytes * /<size>`, and that size is the only
// thing a client can use to work out what it should ask for instead. Reporting
// a wrong total there sends the caller into a loop of unsatisfiable requests,
// and nothing else in the corpus would notice.
func assertCorpusRangeRefusalTotal(corpusContext *sharedCorpusContext, testCase sharedCorpusCase) {
	corpusContext.t.Helper()
	head, err := corpusContext.env.Client.HeadObject(corpusContext.ctx, &s3.HeadObjectInput{
		Bucket: aws.String(corpusContext.bucket(testCase.Bucket)),
		Key:    aws.String(testCase.Key),
	})
	if err != nil {
		corpusContext.t.Fatalf("HeadObject after an unsatisfiable range: %v", err)
	}
	assertCorpusStatus(corpusContext.t, corpusContext.env, http.StatusOK)

	want, err := parseCorpusContentRangeTotal(testCase.Expect.ContentRange)
	if err != nil {
		corpusContext.t.Fatalf("range case %q states an unparseable Content-Range %q: %v",
			testCase.ID, testCase.Expect.ContentRange, err)
	}
	if head.ContentLength == nil {
		corpusContext.t.Fatalf("range case %q: HeadObject reported no ContentLength", testCase.ID)
	}
	if got := *head.ContentLength; got != want {
		corpusContext.t.Fatalf("range case %q refused with total %d, object is %d bytes",
			testCase.ID, want, got)
	}
}

// parseCorpusContentRangeTotal pulls the object size out of a Content-Range
// value, accepting both the `bytes <first>-<last>/<size>` and `bytes */<size>`
// forms. It is deliberately a parser rather than a string comparison: the case
// states one expected value and the check has to read the total out of it.
func parseCorpusContentRangeTotal(contentRange string) (int64, error) {
	const prefix = "bytes "
	if !strings.HasPrefix(contentRange, prefix) {
		return 0, fmt.Errorf("no %q prefix", prefix)
	}
	rest := strings.TrimPrefix(contentRange, prefix)
	slash := strings.LastIndex(rest, "/")
	if slash < 0 {
		return 0, fmt.Errorf("no %q separator", "/")
	}
	total, err := strconv.ParseInt(rest[slash+1:], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("total is not a number: %w", err)
	}
	return total, nil
}
