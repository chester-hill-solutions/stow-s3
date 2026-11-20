package corpus

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

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
