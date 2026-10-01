// Package policy narrows environment authority for the resources an operation
// touches. It composes internal/authority rather than replacing it;
// docs/storage-admission-contract.md is the specification.
package policy

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/authority"
)

// Kind is the collection a resource belongs to. The two are never compared.
type Kind string

const (
	KindObject    Kind = "object"
	KindWorkspace Kind = "workspace"
)

// LocalNamespace is the namespace one stow process serves, and the one its policies
// are written against.
//
// It is a constant rather than a literal in each enforcement point because there are
// two — internal/runtime and internal/runthrough — gating the same objects. Two
// literals that have to agree fail silently in the worst direction: a policy naming
// one namespace matches nothing in the other, so the retry worker propagates the
// writes the policy was written to stop.
const LocalNamespace = "runtime"

// Object is the resource an object operation is decided on. The key is the locator
// verbatim, unnormalized: a selector that normalized would grant or withhold a
// permission on a key the author never wrote.
func Object(bucket, key string) Resource {
	return Resource{Namespace: LocalNamespace, Collection: bucket, Kind: KindObject, Locator: key}
}

// Resource is the contract's typed identity. It names what is checked: it grants
// nothing and asserts nothing about existence.
type Resource struct {
	Namespace  string
	Collection string
	Kind       Kind
	Locator    string
	Version    string
}

func (r Resource) String() string {
	name := string(r.Kind) + " " + r.Collection + "/" + r.Locator
	if r.Version != "" {
		name += "@" + r.Version
	}
	return r.Namespace + ":" + name
}

// Selector matches a set of resources of one kind.
type Selector struct {
	Kind Kind
	// Exact is a full-key equality test for an object and a full-path test for a
	// workspace. Prefix semantics depend on Kind and the two are never exchanged;
	// see "Implementing a scope" in docs/storage-admission-contract.md.
	Exact  string
	Prefix string
}

func (s Selector) matches(r Resource) bool {
	if s.Kind != r.Kind {
		return false
	}
	if s.Exact != "" {
		return s.Exact == r.Locator
	}
	if s.Prefix == "" {
		return false
	}
	if s.Kind == KindObject {
		return strings.HasPrefix(r.Locator, s.Prefix)
	}
	return r.Locator == s.Prefix || strings.HasPrefix(r.Locator, s.Prefix+"/")
}

func (s Selector) String() string {
	if s.Exact != "" {
		return string(s.Kind) + "=" + s.Exact
	}
	return string(s.Kind) + "~" + s.Prefix
}

// Effect is whether an entry grants or withholds.
type Effect string

const (
	Allow Effect = "allow"
	Deny  Effect = "deny"
)

type entry struct {
	namespace  string
	collection string
	selector   Selector
	effect     Effect
	permitted  authority.Authority
}

// ErrStalePolicy is deliberately not a refusal: a refusal is a decision and
// retrying it cannot help, while a stale policy means the answer is unknown.
var ErrStalePolicy = errors.New("policy revision is past its freshness deadline")

// ErrWidening reports a grant the environment never had. Authorize would clip it
// anyway; this names the selector that did it.
var ErrWidening = errors.New("policy grants an operation the environment authority does not")

// Set is one policy revision: selectors, their effects, and a deadline.
type Set struct {
	Revision string
	// Expires is when this revision stops being a basis for a decision.
	Expires time.Time
	Now     func() time.Time // the clock, replaceable in tests

	entries []entry
}

// Add records a selector's effect. The mask is built from None, not All: With only
// adds, so an All() mask would grant everything its selector covers.
func (s *Set) Add(namespace, collection string, selector Selector, effect Effect, permitted ...authority.Operation) {
	s.entries = append(s.entries, entry{
		namespace:  namespace,
		collection: collection,
		selector:   selector,
		effect:     effect,
		permitted:  authority.None().With(permitted...),
	})
}

// New validates a policy against the environment it will be applied to.
func New(s Set, env authority.Authority) (Set, error) {
	if s.Now == nil {
		s.Now = time.Now
	}
	for _, e := range s.entries {
		if env.IsSupersetOf(e.permitted) {
			continue
		}
		return Set{}, fmt.Errorf("%w: %s %s/%s allows %s", ErrWidening, e.effect, e.collection, e.selector, e.permitted)
	}
	return s, nil
}

// Authorize is the effective decision for one operation on one resource: the
// environment authority intersected with what the policy permits there. Explicit
// deny beats allow, an uncovered operation is denied, and the result is a subset
// of env by construction.
func (s Set) Authorize(env authority.Authority, r Resource, op authority.Operation) (authority.Authority, error) {
	if !env.Allows(op) {
		return env, &authority.ErrNotAuthorized{Operation: op, Authority: env}
	}
	if s.Now != nil && !s.Expires.IsZero() && s.Now().After(s.Expires) {
		return authority.None(), fmt.Errorf("%w: revision %q expired", ErrStalePolicy, s.Revision)
	}
	permitted := authority.None()
	for _, e := range s.entries {
		if e.namespace != r.Namespace || e.collection != r.Collection || !e.selector.matches(r) {
			continue
		}
		if e.effect == Deny {
			if e.permitted.Allows(op) {
				return authority.None(), &authority.ErrNotAuthorized{Operation: op, Authority: permitted}
			}
			continue
		}
		permitted = permitted.With(e.permitted.Operations()...)
	}
	if !permitted.Allows(op) {
		return authority.None(), &authority.ErrNotAuthorized{Operation: op, Authority: permitted}
	}
	// Intersect, not With: With would add the policy's operations to the
	// environment, the one direction a policy may never move authority in.
	return env.Intersect(permitted), nil
}

// Allows is Authorize reduced to what a check site needs.
func (s Set) Allows(env authority.Authority, r Resource, op authority.Operation) error {
	_, err := s.Authorize(env, r, op)
	return err
}

// Denies reports whether an entry withholds op, for an operation naming no resource
// locator. It is the only way a policy reaches such an operation, and the asymmetry
// with Authorize is the point: a deny is honoured wherever it can be attributed.
//
// A key prefix is not attributable — an entry over `public/` says nothing about
// whether a bucket may be created. A collection is matched when the operation names
// one and matches any when it does not, because Reset names no bucket and a deny
// scoped to one collection that could never apply is the same failure as one never
// written.
func (s Set) Denies(namespace, collection string, op authority.Operation) bool {
	for _, e := range s.entries {
		if e.namespace != namespace {
			continue
		}
		if collection != "" && e.collection != collection {
			continue
		}
		if e.effect == Deny && e.permitted.Allows(op) {
			return true
		}
	}
	return false
}

// Selectors lists what the policy covers, sorted, for a diagnostic.
func (s Set) Selectors() []string {
	out := make([]string, 0, len(s.entries))
	for _, e := range s.entries {
		out = append(out, fmt.Sprintf("%s %s %s %s", e.effect, e.namespace, e.collection, e.selector))
	}
	sort.Strings(out)
	return out
}
