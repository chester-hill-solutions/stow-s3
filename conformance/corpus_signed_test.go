package conformance_test

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func runCorpusSignedRequest(c *sharedCorpusContext) {
	endpoint, err := url.Parse(c.env.Endpoint)
	if err != nil {
		c.t.Fatal(err)
	}
	target := *endpoint
	target.Path = "/" + c.bucket(c.testCase.Bucket) + "/" + c.testCase.Key
	if c.testCase.VirtualHost {
		target.Host = c.bucket(c.testCase.Bucket) + "." + endpoint.Host
		target.Path = "/" + c.testCase.Key
	}
	query := target.Query()
	query.Set("X-Amz-Expires", "300")
	target.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(c.ctx, c.testCase.Method, target.String(), strings.NewReader(c.testCase.Body))
	if err != nil {
		c.t.Fatal(err)
	}
	signed, _, err := v4.NewSigner().PresignHTTP(c.ctx, aws.Credentials{AccessKeyID: c.env.Creds.AccessKeyID, SecretAccessKey: c.env.Creds.SecretAccessKey}, request, "UNSIGNED-PAYLOAD", "s3", testRegion, time.Now().Add(time.Duration(c.testCase.SigningOffsetSeconds)*time.Second))
	if err != nil {
		c.t.Fatal(err)
	}
	request, err = http.NewRequestWithContext(c.ctx, c.testCase.Method, signed, strings.NewReader(c.testCase.Body))
	if err != nil {
		c.t.Fatal(err)
	}
	request.Host = request.URL.Host
	request.URL.Host = endpoint.Host
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		c.t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		c.t.Fatal(err)
	}
	assertCorpusSignedResponse(c, response.StatusCode, string(body))
}

func assertCorpusSignedResponse(c *sharedCorpusContext, status int, body string) {
	if status != c.testCase.Expect.Status {
		c.t.Fatalf("status=%d body=%s", status, body)
	}
	if c.testCase.Expect.ErrorCode != "" && !strings.Contains(string(body), "<Code>"+c.testCase.Expect.ErrorCode+"</Code>") {
		c.t.Fatalf("error body=%s", body)
	}
	if c.testCase.Method == http.MethodGet && status == 200 && string(body) != c.testCase.Expect.Body {
		c.t.Fatalf("body=%q", body)
	}
	if c.testCase.Method == http.MethodPut && status == 200 {
		assertCorpusGet(c, c.testCase.Bucket, c.testCase.Key, c.testCase.Expect)
	}
}
