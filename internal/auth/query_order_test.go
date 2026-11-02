package auth

import "testing"

// The signing rules encode the query names and values before sorting them, so
// the order is the order of the encoded names. Sorting the decoded names puts
// a name ahead of another one that the encoded order reverses, and the
// signature then differs from the one a client computed.
func TestCanonicalQueryOrderFollowsTheEncodedNames(t *testing.T) {
	for _, test := range []struct{ name, raw, want string }{
		{"ascii", "z=last&a=first", "a=first&z=last"},
		{"encoded-key", "z=last&%C3%A9=first", "%C3%A9=first&z=last"},
		{"encoded-value", "k=%C3%A9", "k=%C3%A9"},
		{"repeated", "k=b&k=a", "k=a&k=b"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := canonicalQueryString(test.raw); got != test.want {
				t.Errorf("canonical query = %q, want %q", got, test.want)
			}
		})
	}
}
