package policy

import "time"

// Source is the policy in force, read when a decision is made rather than when the
// environment was opened, so a revocation is effective when it is issued.
//
// It is a function type rather than an interface because nil is the answer the
// caller already has for "no policy", and an interface would make a nil *Set mean
// something else.
//
// The error is for an answer that is unknown rather than absent, and is reported as
// unknown rather than resolved into a decision: "you may not" and "I do not know" need
// different responses from a caller.
//
// **A Source must be cheap and must not block** — it is consulted on every operation,
// and on the multipart paths while the instance mutex is held. A host whose policy
// lives on disk wraps it in something stating how stale the answer may be; that is the
// contract's "maximum stale-policy interval".
type Source func() (Set, error)

// Fixed is the Source for a policy that does not change while the Instance is open. It
// is named so a call site says which it means: a fixed policy and a live one answer the
// same question, and a reader of an Options literal has no other way to tell.
func Fixed(s *Set) Source {
	return func() (Set, error) {
		if s == nil {
			// A nil *Set reaching here is a caller's mistake, and the answer is the
			// empty set rather than no policy: an empty set denies everything, which
			// is the direction a mistake has to fail.
			return Set{}, nil
		}
		return *s, nil
	}
}

// Expiring is Fixed plus a deadline a host can shorten between calls. One moved
// forwards is ignored, because a policy that could extend its own lifetime by being
// asked again would never expire.
func Expiring(s *Set, now func() time.Time, expires func() time.Time) Source {
	return func() (Set, error) {
		out, err := Fixed(s)()
		if err != nil {
			return out, err
		}
		if out.Expires.IsZero() {
			return out, nil
		}
		if shortened := expires(); !shortened.IsZero() && shortened.Before(out.Expires) {
			out.Expires = shortened
		}
		if out.Now == nil {
			out.Now = now
		}
		return out, nil
	}
}
