package policy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/authority"
)

// recordVersion is the on-disk shape this package writes and the newest it
// reads. A file declaring anything else is refused rather than interpreted with
// today's field meanings, which is the difference between failing loudly and
// silently dropping the entries it did not recognise.
const recordVersion = 1

// MaxRecordBytes bounds what is read from a policy file. A policy is a bounded
// set of selectors; a file far larger than any legitimate one is not a policy
// this reader should try to interpret. The reader of a stored record lives in
// another package, so the bound is exported for it to apply before parsing.
const MaxRecordBytes = 4 << 20

var (
	// ErrNoRecord reports that no policy has been written yet. Distinct from a
	// damaged one: "none" is a decision, "damaged" is an unknown.
	ErrNoRecord = errors.New("no policy revision has been written")

	// ErrRecordDamaged reports a file that is not a policy record this build can
	// stand behind: bad JSON, a wrong version, or a failing integrity seal.
	ErrRecordDamaged = errors.New("policy record is damaged")

	// ErrRecordSuperseded reports a write that lost a race, or tried to move the
	// record backwards. A policy is replaced, never rolled back: an older
	// revision written last would silently re-grant what a newer one revoked.
	ErrRecordSuperseded = errors.New("policy revision was superseded or moved backwards")

	// ErrDeadlineInThePast reports a revision whose freshness deadline is at or
	// before the moment it would be issued. It is refused rather than stored,
	// because a stored revision that can only read as stale is a revocation
	// nobody asked for: the answer would be "unknown" where the author meant
	// "denied", and those need different responses from a caller.
	ErrDeadlineInThePast = errors.New("policy revision expires at or before it is issued")
)

// Record is one persisted policy revision: the durable form of a Set, plus the
// revision identity, the sequence that orders it against other revisions, and
// the deadline that decides whether it may still be a basis for a decision.
type Record struct {
	Version  int           `json:"version"`
	Sequence uint64        `json:"sequence"`
	Revision string        `json:"revision"`
	Issued   time.Time     `json:"issued"`
	Expires  time.Time     `json:"expires"`
	Entries  []recordEntry `json:"entries"`
	// Integrity seals everything above it, and is verified on load. Without it a
	// truncated or edited file reads as *some* policy, and the direction that
	// looks safe is the wrong one: losing an entry narrows a grant, which reads
	// as a denial, while losing a deadline reads as a permission that never
	// expires.
	Integrity string `json:"integrity"`
}

// recordEntry is one persisted selector. It names operations rather than
// carrying an Authority mask, because a mask is a bit position: adding an
// Operation would renumber it, and a record written before that would decode to
// a different set of permissions afterwards.
type recordEntry struct {
	Namespace  string                `json:"namespace"`
	Collection string                `json:"collection"`
	Kind       Kind                  `json:"kind"`
	Exact      string                `json:"exact,omitempty"`
	Prefix     string                `json:"prefix,omitempty"`
	Effect     Effect                `json:"effect"`
	Operations []authority.Operation `json:"operations"`
}

// FromSet is the durable form of a policy. The environment is required, so a
// policy cannot be persisted wider than the environment it will be applied to:
// without that check a widening record sits on disk looking authoritative until
// each reader re-derives the mistake, rather than being refused once where it
// was written.
func FromSet(s Set, sequence uint64, issued time.Time, env authority.Authority) (Record, error) {
	validated, err := New(s, env)
	if err != nil {
		return Record{}, err
	}
	if validated.Revision == "" {
		return Record{}, errors.New("a persisted policy needs a host-issued revision identity")
	}
	record := Record{
		Version:  recordVersion,
		Sequence: sequence,
		Revision: validated.Revision,
		Issued:   issued.UTC(),
		Expires:  validated.Expires.UTC(),
		Entries:  make([]recordEntry, 0, len(validated.entries)),
	}
	for _, e := range validated.entries {
		record.Entries = append(record.Entries, recordEntry{
			Namespace:  e.namespace,
			Collection: e.collection,
			Kind:       e.selector.Kind,
			Exact:      e.selector.Exact,
			Prefix:     e.selector.Prefix,
			Effect:     e.effect,
			Operations: e.permitted.Operations(),
		})
	}
	return record.canonical().seal(), nil
}

// Policy rebuilds the decision model from a sealed record. The clock is the
// caller's rather than time.Now, so a stale revision is stale relative to a
// stated instant instead of to whenever the read happened.
func (r Record) Policy(now func() time.Time) (Set, error) {
	if r.Version != recordVersion {
		return Set{}, fmt.Errorf("%w: version %d, this build reads %d", ErrRecordDamaged, r.Version, recordVersion)
	}
	if r.Integrity == "" || r.Integrity != r.integrity() {
		return Set{}, fmt.Errorf("%w: integrity seal does not match the contents", ErrRecordDamaged)
	}
	if now == nil {
		return Set{}, errors.New("a record needs a clock to be read against")
	}
	set := Set{Revision: r.Revision, Expires: r.Expires, Now: now}
	for _, e := range r.Entries {
		if e.Effect != Allow && e.Effect != Deny {
			return Set{}, fmt.Errorf("%w: effect %q is neither allow nor deny", ErrRecordDamaged, e.Effect)
		}
		if e.Kind != KindObject && e.Kind != KindWorkspace {
			return Set{}, fmt.Errorf("%w: kind %q is not one this build knows", ErrRecordDamaged, e.Kind)
		}
		// Through Add rather than straight into an authority: an operation this
		// build does not define is dropped there, which is the safe direction,
		// and going through Add keeps one rule about that rather than two.
		set.Add(e.Namespace, e.Collection, Selector{Kind: e.Kind, Exact: e.Exact, Prefix: e.Prefix}, e.Effect, e.Operations...)
	}
	return set, nil
}

// Expired reports whether this revision has passed its freshness deadline. It is
// the same test Authorize makes, separated so a caller deciding whether to
// consult a revision at all need not invent a resource to ask about.
func (r Record) Expired(now time.Time) bool { return !r.Expires.IsZero() && now.After(r.Expires) }

// canonical is the record with its entries in one total order, so two records
// meaning the same thing seal identically. Without it, re-persisting one policy in
// a different Add order reads as a change.
func (r Record) canonical() Record {
	out := r
	out.Entries = append([]recordEntry(nil), r.Entries...)
	sort.SliceStable(out.Entries, func(i, j int) bool {
		return entryOrder(out.Entries[i]) < entryOrder(out.Entries[j])
	})
	return out
}

// entryOrder is a total order over entries. Operations are deliberately absent
// from it: Authority.Operations already returns them sorted, and two entries
// differing only in their operation list order equal here and stay stable.
func entryOrder(e recordEntry) string {
	return e.Namespace + "\x00" + e.Collection + "\x00" + string(e.Kind) + "\x00" + e.Exact + "\x00" + e.Prefix + "\x00" + string(e.Effect)
}

// integrity is the seal, over the record with its own field blanked so it can be
// reproduced by a reader that has no record of how it was written.
func (r Record) integrity() string {
	sealed := r
	sealed.Integrity = ""
	data, err := json.Marshal(sealed)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(append([]byte("stow-policy-v1\x00"), data...))
	return hex.EncodeToString(digest[:])
}

// seal is canonical with the integrity field filled in from the same bytes a
// reader will recompute it over.
func (r Record) seal() Record {
	canonical := r.canonical()
	canonical.Integrity = ""
	canonical.Integrity = canonical.integrity()
	return canonical
}

// Encode renders a record for storage.
func (r Record) Encode() ([]byte, error) {
	sealed := r.seal()
	return json.Marshal(sealed)
}

// Decode reads a stored record. A file longer than any policy could be is
// refused before it is parsed, and an empty one is ErrNoRecord rather than a
// decode failure, because the two mean opposite things to a caller.
func Decode(data []byte) (Record, error) {
	if len(data) == 0 {
		return Record{}, ErrNoRecord
	}
	if len(data) > MaxRecordBytes {
		return Record{}, fmt.Errorf("%w: %d bytes exceeds the %d a policy record may occupy",
			ErrRecordDamaged, len(data), MaxRecordBytes)
	}
	var record Record
	if err := json.Unmarshal(data, &record); err != nil {
		return Record{}, fmt.Errorf("%w: %v", ErrRecordDamaged, err)
	}
	return record, nil
}
