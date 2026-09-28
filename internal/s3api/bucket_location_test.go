package s3api_test

import (
	"encoding/xml"
	"io"
	"net/http"
	"strings"
	"testing"
)

// GetBucketLocation answered 400 Invalid request for every request.
//
// That is the one status an SDK cannot recover from. The call is part of a bucket's
// own surface, so a client doing a bucket capability probe, or configuring itself
// from a bucket it was handed, hits it before anything else works — and the message
// named neither the operation nor a fix.
//
// A real provider is the only honest test of the response *shape*, because the shape
// is the part SDKs disagree about. AWS returns the element present and empty for
// us-east-1, and a client decoding into a string cannot tell an absent element from
// a refusal, so the element has to be there and empty rather than omitted.

func TestGetBucketLocationAnswersWithAnEmptyConstraint(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/locationtest", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	resp.Body.Close()

	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/locationtest?location", nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get location: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "xml") {
		t.Errorf("Content-Type = %q, and a client parsing this as XML needs to be told", ct)
	}

	var parsed struct {
		XMLName  xml.Name `xml:"LocationConstraint"`
		Location string   `xml:",chardata"`
	}
	if err := xml.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("the body is not a LocationConstraint document: %v\n%s", err, body)
	}
	if parsed.XMLName.Local != "LocationConstraint" {
		t.Errorf("root element is %q, want LocationConstraint: %s", parsed.XMLName.Local, body)
	}
	// The element is present and empty. Present because a client that checks for it
	// gets the answer S3 gives; empty because that is what us-east-1 looks like, and
	// a fabricated region name would be worse than the truth.
	if !strings.Contains(string(body), "LocationConstraint") {
		t.Errorf("the body does not contain the element at all, so a client cannot tell a region from a refusal: %s", body)
	}
	if parsed.Location != "" {
		t.Errorf("LocationConstraint = %q, want empty", parsed.Location)
	}
}

// A bucket that is not there is NoSuchBucket, not an empty region. Reporting an
// empty constraint for a bucket that does not exist would let a client configure
// itself from a bucket name it got wrong.
func TestGetBucketLocationRefusesABucketThatIsNotThere(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/no-such-bucket-here?location", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get location: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: an empty region for a bucket that does not exist would let a client configure itself from a name it got wrong. %s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "NoSuchBucket") {
		t.Errorf("the body does not name NoSuchBucket: %s", body)
	}
}
