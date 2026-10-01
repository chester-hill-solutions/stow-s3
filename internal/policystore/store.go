// Package policystore persists a policy revision. It is separate from
// internal/policy because that package is in the embedded runtime's dependency
// closure, which must not link the filesystem: a decision model that can read a
// file is a decision model whose answers depend on a filesystem, and a WASM or
// embedded host has none.
//
// docs/storage-admission-contract.md is the specification.
package policystore

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/atomicfile"
	"github.com/chester-hill-solutions/stow-s3/internal/authority"
	"github.com/chester-hill-solutions/stow-s3/internal/policy"
)

// Store is the durable home of the current policy revision.
//
// Exclusion is the reason this is a type rather than a bare file write. A revision is
// replaced by reading the current one, deciding, and writing the result, and two hosts
// doing that concurrently would both read the same sequence and both write — so a
// revocation and a re-grant could land in either order and the loser would not know it
// had lost. The lock makes read-decide-write a critical section, and the sequence check
// inside it turns "we raced" into a refusal rather than a silent overwrite.
type Store struct {
	path string
	// now is the clock revisions are stamped and read against, held so a test can
	// place a revision in the past without sleeping.
	now func() time.Time
}

// Open binds a store to a file path. The file need not exist: a first write
// creates it, and a read before that is policy.ErrNoRecord.
func Open(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("policystore: a store needs a file path")
	}
	return &Store{path: path, now: time.Now}, nil
}

// Path is the file this store reads and writes, so a host can reopen the same
// policy after a restart without threading the string through its own state.
func (s *Store) Path() string { return s.path }

// SetClock replaces the store's clock, so a test can place a read on the wrong
// side of a revision's deadline. That cannot be done by waiting.
func (s *Store) SetClock(now func() time.Time) { s.now = now }

// Current reads the persisted revision.
//
// A missing file is policy.ErrNoRecord and a damaged one is policy.ErrRecordDamaged,
// and the difference is why both are named: no record is a decision to run with
// the environment's own authority, while a damaged record is a policy whose
// answer is unknown — and answering that with the environment's authority hands
// back every permission the damaged policy was withholding.
func (s *Store) Current() (policy.Record, error) {
	lock, err := atomicfile.Acquire(s.lockPath())
	if err != nil {
		return policy.Record{}, err
	}
	defer lock.Release()
	return s.currentLocked()
}

// Policy is Current rebuilt as a decision model.
//
// Validation happens on the way *out* of the store as well as on the way in,
// because the environment a record is checked against is the one it will be read
// under, and a record written under a wider one must not become usable under
// this one.
func (s *Store) Policy(env authority.Authority) (policy.Set, error) {
	record, err := s.Current()
	if err != nil {
		return policy.Set{}, err
	}
	return record.Policy(s.now)
}

// Persist writes next as the successor of the stored record, and returns what it wrote.
// expected is the sequence the caller based its decision on, read from Current, and it
// is checked against the stored record while the lock is held, so a caller working from
// a revision that has since been replaced is refused instead of overwriting it:
// revisions are not commutative, and a revocation that loses a race to a re-grant is the
// failure this exists to prevent. The refusal is policy.ErrRecordSuperseded and the stored
// record is untouched.
func (s *Store) Persist(next policy.Record, expected uint64) (policy.Record, error) {
	lock, err := atomicfile.Acquire(s.lockPath())
	if err != nil {
		return policy.Record{}, err
	}
	defer lock.Release()
	stored, err := s.currentLocked()
	switch {
	case errors.Is(err, policy.ErrNoRecord):
		if expected != 0 {
			return policy.Record{}, fmt.Errorf("%w: expected sequence %d but no revision is stored",
				policy.ErrRecordSuperseded, expected)
		}
	case err != nil:
		return policy.Record{}, err
	case stored.Sequence != expected:
		return policy.Record{}, fmt.Errorf("%w: expected sequence %d, stored is %d",
			policy.ErrRecordSuperseded, expected, stored.Sequence)
	}
	// Numbered here rather than by the caller: the sequence's job is to order
	// revisions against each other, and a caller-chosen one can skip or repeat,
	// which is exactly what the check above cannot detect.
	written, err := s.write(next, stored.Sequence+1)
	if err != nil {
		return policy.Record{}, err
	}
	return written, nil
}

// write seals and stores a record at a given sequence, refusing a revision
// already past its own deadline. One stored that could only ever read as stale is
// a revocation nobody asked for: the answer would be "unknown" where the author
// meant "denied", and the file is the wrong place to discover that.
func (s *Store) write(record policy.Record, sequence uint64) (policy.Record, error) {
	issued := s.now().UTC()
	record.Sequence = sequence
	record.Issued = issued
	if !record.Expires.IsZero() && !record.Expires.After(issued) {
		return policy.Record{}, fmt.Errorf("%w: revision %q expires at %s, at or before it would be issued",
			policy.ErrDeadlineInThePast, record.Revision, record.Expires)
	}
	data, err := record.Encode()
	if err != nil {
		return policy.Record{}, err
	}
	if err := atomicfile.Write(s.path, data, 0o600); err != nil {
		return policy.Record{}, err
	}
	return policy.Decode(data)
}

// currentLocked is Current for a caller already holding the lock.
func (s *Store) currentLocked() (policy.Record, error) {
	data, err := readBounded(s.path)
	if err != nil {
		return policy.Record{}, err
	}
	return policy.Decode(data)
}

func (s *Store) lockPath() string { return s.path + ".lock" }

// readBounded reads at most one byte more than a record may occupy, so an
// oversized file is refused by size rather than after being held in memory.
func readBounded(path string) ([]byte, error) {
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, policy.ErrNoRecord
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, policy.MaxRecordBytes+1))
	if err != nil {
		return nil, err
	}
	return data, nil
}
