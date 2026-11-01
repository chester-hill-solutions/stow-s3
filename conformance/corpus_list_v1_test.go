package conformance_test

// The classic ListObjects (V1) corpus adapter.
//
// V1 is the listing a client sends when it never learned the list-type marker,
// and its cursor is a row of the listing rather than an opaque token: a marker
// can name a common prefix. Delimiter pages use NextMarker; flat pages use the last Key.
// Each page's cursor is asserted rather than merely followed, because a page
// that reports the wrong cursor sends a client back into a row it already has.

import (
	"net/http"
	"slices"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

func runCorpusListObjectsV1(corpusContext *sharedCorpusContext) {
	corpusContext.t.Helper()
	testCase := corpusContext.testCase
	bucket := corpusContext.bucket(testCase.Bucket)

	if testCase.Expect.Status >= http.StatusBadRequest {
		_, err := corpusContext.env.Client.ListObjects(corpusContext.ctx, &s3.ListObjectsInput{
			Bucket: aws.String(bucket),
			Marker: optionalString(testCase.Marker),
		})
		assertCorpusError(corpusContext.t, err, testCase.Expect)
		return
	}

	marker := testCase.Marker
	for index, expected := range testCase.Expect.Pages {
		output, err := corpusContext.env.Client.ListObjects(corpusContext.ctx, &s3.ListObjectsInput{
			Bucket:       aws.String(bucket),
			Prefix:       optionalString(testCase.Prefix),
			Delimiter:    optionalString(testCase.Delimiter),
			Marker:       optionalString(marker),
			MaxKeys:      corpusMaxKeys(testCase.MaxKeys),
			EncodingType: types.EncodingType(testCase.EncodingType),
		})
		if err != nil {
			corpusContext.t.Fatalf("ListObjects page %d: %v", index+1, err)
		}
		assertCorpusStatus(corpusContext.t, corpusContext.env, testCase.Expect.Status)

		contents := make([]string, 0, len(output.Contents))
		for _, object := range output.Contents {
			contents = append(contents, aws.ToString(object.Key))
		}
		prefixes := make([]string, 0, len(output.CommonPrefixes))
		for _, commonPrefix := range output.CommonPrefixes {
			prefixes = append(prefixes, aws.ToString(commonPrefix.Prefix))
		}
		if !slices.Equal(contents, expected.Contents) {
			corpusContext.t.Fatalf("page %d contents = %q, want %q", index+1, contents, expected.Contents)
		}
		if !slices.Equal(prefixes, expected.CommonPrefixes) {
			corpusContext.t.Fatalf("page %d common prefixes = %q, want %q", index+1, prefixes, expected.CommonPrefixes)
		}
		if got := aws.ToBool(output.IsTruncated); got != expected.IsTruncated {
			corpusContext.t.Fatalf("page %d IsTruncated = %t, want %t", index+1, got, expected.IsTruncated)
		}
		if got := aws.ToString(output.NextMarker); got != expected.NextMarker {
			corpusContext.t.Fatalf("page %d NextMarker = %q, want %q", index+1, got, expected.NextMarker)
		}
		if !aws.ToBool(output.IsTruncated) {
			break
		}
		marker = aws.ToString(output.NextMarker)
		if marker == "" && len(contents) > 0 {
			marker = contents[len(contents)-1]
		}
		if marker == "" {
			corpusContext.t.Fatal("truncated V1 page has no usable cursor")
		}
	}
}

// corpusMaxKeys omits the limit when a case does not state one, so the server's
// default page is exercised rather than a limit the case did not ask for.
func corpusMaxKeys(maxKeys int) *int32 {
	if maxKeys <= 0 {
		return nil
	}
	return aws.Int32(int32(maxKeys))
}

func listV1CursorProblem(delimiter string, page sharedCorpusPage) string {
	if !page.IsTruncated && page.NextMarker != "" {
		return "final page must not report NextMarker"
	}
	if delimiter == "" {
		if page.NextMarker != "" {
			return "NextMarker requires a delimiter"
		}
		if page.IsTruncated && len(page.Contents) == 0 {
			return "truncated flat page needs a last Key"
		}
	} else if page.IsTruncated && page.NextMarker == "" {
		return "truncated delimiter page needs NextMarker"
	}
	return ""
}

func TestListV1CorpusRequiresTheCorrectCursorSource(t *testing.T) {
	for _, testCase := range []struct {
		delimiter string
		page      sharedCorpusPage
		invalid   bool
	}{
		{"", sharedCorpusPage{IsTruncated: true, Contents: []string{"key"}}, false},
		{"/", sharedCorpusPage{IsTruncated: true, NextMarker: "group/"}, false},
		{"", sharedCorpusPage{IsTruncated: true}, true},
		{"", sharedCorpusPage{IsTruncated: true, Contents: []string{"key"}, NextMarker: "key"}, true},
		{"/", sharedCorpusPage{IsTruncated: true}, true},
		{"/", sharedCorpusPage{NextMarker: "group/"}, true},
	} {
		if problem := listV1CursorProblem(testCase.delimiter, testCase.page); (problem != "") != testCase.invalid {
			t.Errorf("delimiter=%q page=%+v: %q", testCase.delimiter, testCase.page, problem)
		}
	}
}
