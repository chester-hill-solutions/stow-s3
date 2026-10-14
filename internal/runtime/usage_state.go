package runtime

import (
	"errors"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

type usageState struct {
	limits           Options
	usage            Usage
	usageRevision    uint64
	usageDirty       bool
	recoveryErr      error
	multipart        map[string]multipartUsage
	multipartTargets map[string]int
	reservedTargets  map[string]struct{}
	reservedObjects  int64
	reservedBytes    int64
	staged           streamReservation
}

func newUsageState(options Options) usageState {
	return usageState{
		limits:           options,
		multipart:        make(map[string]multipartUsage),
		multipartTargets: make(map[string]int),
		reservedTargets:  make(map[string]struct{}),
	}
}

func (s *usageState) recordMutation(err error) bool {
	s.usageRevision++
	if err != nil {
		s.usageDirty = true
	}
	return err == nil || errors.Is(err, storage.ErrMutationCommitted)
}

func (s *usageState) applyPut(target string, oldSize, newSize int64, exists bool) {
	if exists {
		s.usage.Bytes -= oldSize
	} else {
		s.usage.Objects++
	}
	s.usage.Bytes += newSize
	s.reconcileTargetReservation(target, true)
}

func (s *usageState) finishPut(target string, oldSize, newSize int64, exists bool, err error) bool {
	if !s.recordMutation(err) {
		return false
	}
	s.applyPut(target, oldSize, newSize, exists)
	return true
}

func (s *usageState) finishStoredPut(target string, oldSize int64, meta *storage.ObjectMeta, exists bool, err error) bool {
	if !s.recordMutation(err) {
		return false
	}
	s.applyPut(target, oldSize, meta.Size, exists)
	return true
}

func (s *usageState) applyDelete(target string, size int64, exists bool) {
	if exists {
		s.usage.Bytes -= size
		s.usage.Objects--
	}
	s.reconcileTargetReservation(target, false)
}

func (s *usageState) finishDelete(target string, size int64, exists bool, err error) {
	if s.recordMutation(err) {
		s.applyDelete(target, size, exists)
	}
}

func (s *usageState) finishDeletes(bucket string, deleted []string, sizes map[string]int64, err error) {
	s.recordMutation(err)
	for _, key := range deleted {
		if size, exists := sizes[key]; exists {
			delete(sizes, key)
			s.applyDelete(objectTarget(bucket, key), size, true)
		}
	}
}

func (s *usageState) reserveStaged(reservation streamReservation) {
	s.staged.bytes += reservation.bytes
	s.staged.objects += reservation.objects
	s.reservedBytes += reservation.bytes
	s.reservedObjects += reservation.objects
}

func (s *usageState) releaseStaged(reservation streamReservation) {
	s.staged.bytes -= reservation.bytes
	s.staged.objects -= reservation.objects
	s.reservedBytes -= reservation.bytes
	s.reservedObjects -= reservation.objects
}

func (s *usageState) adopt(snapshot usageState) {
	snapshot.limits = s.limits
	snapshot.staged = s.staged
	snapshot.reservedBytes += s.staged.bytes
	snapshot.reservedObjects += s.staged.objects
	*s = snapshot
}
