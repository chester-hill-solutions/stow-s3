package capacity

import (
	"context"
	"errors"
)

func (n *Namespace) Reconcile(ctx context.Context, usage Usage) error {
	if err := validateResources(usage.Resources); err != nil {
		return err
	}
	return n.access(ctx, func(state *hostState, ns *namespaceState) error {
		store, ok := ns.Stores[usage.StoreID]
		if !ok {
			return ErrConflict
		}
		if err := checkHeldProjection(ns, usage); err != nil {
			return err
		}
		store.Resources = usage.Resources
		store.Pending = nil
		return n.host.write(state)
	})
}

func (n *Namespace) WithAdmission(ctx context.Context, admission Admission, effect func() error) error {
	if admission.StagingBytes < 0 || admission.RecoveryBytes < 0 || effect == nil {
		return ErrInvalid
	}
	if err := validateResources(admission.Usage.Resources); err != nil {
		return err
	}
	return n.access(ctx, func(state *hostState, ns *namespaceState) error {
		store, ok := ns.Stores[admission.Usage.StoreID]
		if !ok {
			return ErrConflict
		}
		if store.Pending != nil {
			return ErrUnknown
		}
		if err := checkHeldProjection(ns, admission.Usage); err != nil {
			return err
		}
		settled := store.Resources
		store.Pending = &AdmissionState{Resources: admission.Usage.Resources, StagingBytes: admission.StagingBytes, RecoveryBytes: admission.RecoveryBytes}
		if err := n.host.write(state); err != nil {
			return err
		}
		if err := effect(); err != nil {
			// The reservation is durable before the effect runs, so a failed
			// effect withdraws it: left pending it would refuse every later
			// admission for this store, and nothing would clear it.
			store.Resources = settled
			store.Pending = nil
			return errors.Join(err, n.host.write(state))
		}
		store.Resources = admission.Usage.Resources
		store.Pending = nil
		return n.host.write(state)
	})
}

func checkHeldProjection(ns *namespaceState, usage Usage) error {
	index := map[string]Resource{}
	for _, resource := range usage.Resources {
		index[resource.ID] = resource
	}
	for _, operation := range ns.Operations {
		if operation.Released {
			continue
		}
		for _, dependency := range operation.Dependencies {
			if dependency.StoreID != usage.StoreID {
				continue
			}
			resource, ok := index[dependency.ID]
			if !ok || resource.Version != dependency.Version || resource.Bytes != dependency.Bytes {
				return ErrHeld
			}
		}
	}
	return nil
}
