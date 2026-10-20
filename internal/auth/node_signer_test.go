package auth_test

import (
	"encoding/json"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/auth"
)

// A request the pinned Node signer produced must authenticate, including one
// whose query carries a name that needs encoding. The canonical query is
// ordered by the encoded names, so a name that needs encoding can come first.
//
// The fixture is the recorded output of @smithy/signature-v4 5.7.4 with a
// fixed signing date and fixed credentials, so it is the same on every run.
func TestPinnedNodeSignerRequestsAuthenticate(t *testing.T) {
	data, err := os.ReadFile("testdata/node-sdk-signatures.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name    string            `json:"name"`
		URL     string            `json:"url"`
		Headers map[string]string `json:"headers"`
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	if len(fixtures) == 0 {
		t.Fatal("the fixture is empty, so nothing is being checked")
	}
	reference := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, fixture.URL, nil)
			if err != nil {
				t.Fatal(err)
			}
			for name, value := range fixture.Headers {
				req.Header.Set(name, value)
			}
			if err := auth.NewVerifier("us-east-1").AuthenticateAt(req, strictCreds, reference); err != nil {
				t.Errorf("a valid pinned Node signer request was refused: %v", err)
			}
		})
	}
}
