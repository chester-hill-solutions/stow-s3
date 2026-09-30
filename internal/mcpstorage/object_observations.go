package mcpstorage

import (
	"context"
	"crypto/rand"
	"encoding/hex"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

func (host *ObjectServer) reapObservations() {
	now := host.now()
	for token, observation := range host.observations {
		if !now.Before(observation.expires) {
			delete(host.observations, token)
		}
	}
}

func (host *ObjectServer) retainObservation(key string, condition stow.SaveCondition) (string, error) {
	host.reapObservations()
	if len(host.observations) >= host.config.MaxObservations {
		return "", objectError{"observation_full"}
	}
	for range 4 {
		var nonce [16]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			return "", objectError{"unavailable"}
		}
		token := hex.EncodeToString(nonce[:])
		if _, exists := host.observations[token]; exists {
			continue
		}
		host.observations[token] = objectObservation{key: key, condition: condition, expires: host.now().Add(observationTTL)}
		return token, nil
	}
	return "", objectError{"unavailable"}
}

func (host *ObjectServer) observation(token, key string) (stow.SaveCondition, error) {
	observation, found := host.observations[token]
	expired := found && !host.now().Before(observation.expires)
	host.reapObservations()
	if expired {
		return stow.SaveCondition{}, objectError{"expired_observation"}
	}
	if !found || observation.key != key {
		return stow.SaveCondition{}, objectError{"invalid_observation"}
	}
	return observation.condition, nil
}

func (host *ObjectServer) beforeOperation(ctx context.Context) error {
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	if host.closed {
		return stow.ErrClosed
	}
	return nil
}
