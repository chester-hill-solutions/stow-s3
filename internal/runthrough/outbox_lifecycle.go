package runthrough

import "time"

type outboxPublicationPhase uint8

const (
	outboxPublished outboxPublicationPhase = iota
	outboxPrepared
)

type outboxPropagationPhase uint8

const (
	outboxReady outboxPropagationPhase = iota
	outboxRetryScheduled
	outboxTerminal
)

func (e OutboxEntry) publicationPhase() outboxPublicationPhase {
	if e.Prepared {
		return outboxPrepared
	}
	return outboxPublished
}

func (e OutboxEntry) propagationPhase() outboxPropagationPhase {
	if e.Terminal {
		return outboxTerminal
	}
	if !e.NextAttempt.IsZero() {
		return outboxRetryScheduled
	}
	return outboxReady
}

func (e *OutboxEntry) stagePublication() { e.Prepared = true }

func (e *OutboxEntry) admitPublished() { e.Prepared = false }

func (e *OutboxEntry) publishVersion(version string) {
	e.admitPublished()
	e.setPreparationLease(outboxLease{})
	e.Version = version
}

func (e *OutboxEntry) recordPropagationFailure(cause error, retryAt time.Time) {
	e.Attempts++
	e.NextAttempt = retryAt
	if cause != nil {
		e.LastError = cause.Error()
	}
	if classifyRetry(cause) == RetryClassDeterministic {
		e.Terminal = true
		e.NextAttempt = time.Time{}
	}
}

type outboxLease struct {
	owner string
	until time.Time
	token uint64
}

func (lease outboxLease) present() bool { return lease.owner != "" }

func (lease outboxLease) matches(owner string, token uint64) bool {
	return owner != "" && token != 0 && lease.owner == owner && lease.token == token
}

func (lease outboxLease) heldAt(now time.Time) bool {
	return lease.present() && (lease.until.After(now) || outboxOwnerAlive(lease.owner))
}

func (e OutboxEntry) preparationLease() outboxLease {
	return outboxLease{owner: e.PreparedOwner, until: e.PreparedUntil, token: e.PreparedToken}
}

func (e OutboxEntry) propagationLease() outboxLease {
	return outboxLease{owner: e.ClaimOwner, until: e.ClaimUntil, token: e.ClaimToken}
}

func (e *OutboxEntry) setPreparationLease(lease outboxLease) {
	e.PreparedOwner, e.PreparedUntil, e.PreparedToken = lease.owner, lease.until, lease.token
}

func (e *OutboxEntry) setPropagationLease(lease outboxLease) {
	e.ClaimOwner, e.ClaimUntil, e.ClaimToken = lease.owner, lease.until, lease.token
}

func (e *OutboxEntry) beginPropagation(lease outboxLease) {
	e.setPropagationLease(lease)
	e.Attempted = true
}
