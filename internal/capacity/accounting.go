package capacity

import (
	"context"
	"encoding/json"
	"math"
)

func add(total *int64, value int64) error {
	if value < 0 || *total > math.MaxInt64-value {
		return ErrInvalid
	}
	*total += value
	return nil
}

func resourcesCharge(resources []Resource) (int64, int64, error) {
	var payload, recovery int64
	for _, resource := range resources {
		target := &payload
		if resource.Class == Recovery {
			target = &recovery
		}
		if err := add(target, resource.Bytes); err != nil {
			return 0, 0, err
		}
	}
	return payload, recovery, nil
}

func namespaceCharge(ns *namespaceState) (Snapshot, error) {
	var out Snapshot
	var count int
	for _, store := range ns.Stores {
		contributed, err := storeCharge(store, &out)
		if err != nil {
			return out, err
		}
		count += contributed
	}
	if count > maxResources {
		return out, ErrFull
	}
	if err := operationCharge(ns, &out); err != nil {
		return out, err
	}
	encoded, err := json.Marshal(ns)
	if err != nil {
		return out, err
	}
	if err := add(&out.RecoveryBytes, int64(len(encoded))*2); err != nil {
		return out, err
	}
	out.ReservedRecoveryBytes = ns.Options.RecoveryReserveBytes
	return out, nil
}

// storeCharge folds one store into the namespace totals. A pending admission is
// a maximum, not an addition: the store already accounts for those bytes.
func storeCharge(store *storeState, out *Snapshot) (int, error) {
	payload, recovery, err := resourcesCharge(store.Resources)
	if err != nil {
		return 0, err
	}
	if store.Pending != nil {
		stagedPayload, stagedRecovery, err := resourcesCharge(store.Pending.Resources)
		if err != nil {
			return 0, err
		}
		payload, recovery = max(payload, stagedPayload), max(recovery, stagedRecovery)
		if err := add(&out.PendingBytes, store.Pending.StagingBytes); err != nil {
			return 0, err
		}
		if err := add(&recovery, store.Pending.RecoveryBytes); err != nil {
			return 0, err
		}
	}
	if err := add(&out.PayloadBytes, payload); err != nil {
		return 0, err
	}
	return len(store.Resources), add(&out.RecoveryBytes, recovery)
}

// operationCharge charges each operation's reserve as recovery, so filling
// the payload budget cannot leave no room to record its own release.
func operationCharge(ns *namespaceState, out *Snapshot) error {
	for _, operation := range ns.Operations {
		if err := add(&out.RecoveryBytes, operation.ReserveBytes); err != nil {
			return err
		}
		if !operation.Released {
			out.Operations++
		}
	}
	return nil
}

func checkCapacity(state *hostState, staging int64) error {
	total := staging
	for _, ns := range state.Namespaces {
		usage, err := namespaceCharge(ns)
		if err != nil {
			return err
		}
		if usage.RecoveryBytes > ns.Options.RecoveryReserveBytes {
			return ErrFull
		}
		payload := usage.PayloadBytes
		if err := add(&payload, usage.PendingBytes); err != nil {
			return err
		}
		if payload > ns.Options.MaxBytes-ns.Options.RecoveryReserveBytes {
			return ErrFull
		}
		if err := add(&total, payload); err != nil {
			return err
		}
		if err := add(&total, ns.Options.RecoveryReserveBytes); err != nil {
			return err
		}
	}
	if total > state.MaxBytes {
		return ErrFull
	}
	return nil
}

func (n *Namespace) Snapshot(ctx context.Context) (Snapshot, error) {
	var result Snapshot
	err := n.access(ctx, func(_ *hostState, ns *namespaceState) error {
		var err error
		result, err = namespaceCharge(ns)
		return err
	})
	return result, err
}

func validateResources(resources []Resource) error {
	if len(resources) > maxResources {
		return ErrFull
	}
	seen := map[string]bool{}
	for _, resource := range resources {
		if !validResource(resource) || seen[resource.ID] {
			return ErrInvalid
		}
		seen[resource.ID] = true
	}
	return nil
}

func validResource(resource Resource) bool {
	return validID(resource.ID) && validID(resource.Version) && resource.Bytes >= 0 && (resource.Class == Payload || resource.Class == Recovery)
}

func validateState(state *hostState) error {
	if len(state.Namespaces) > state.MaxNamespaces {
		return ErrUnknown
	}
	for id, ns := range state.Namespaces {
		if err := validateNamespace(id, ns); err != nil {
			return err
		}
	}
	return nil
}

func validateNamespace(id string, ns *namespaceState) error {
	if ns == nil || id != ns.Options.ID || ns.Stores == nil || ns.Operations == nil {
		return ErrUnknown
	}
	if len(ns.Operations) > ns.Options.MaxOperations {
		return ErrUnknown
	}
	for storeID, store := range ns.Stores {
		if err := validateStore(storeID, store); err != nil {
			return err
		}
	}
	for operationID, operation := range ns.Operations {
		if err := validateOperation(operationID, operation, ns.Options.MaxDependencies); err != nil {
			return err
		}
	}
	return nil
}

func validateStore(id string, store *storeState) error {
	if !validID(id) || store == nil || store.Root == "" || validateResources(store.Resources) != nil {
		return ErrUnknown
	}
	if store.Pending != nil && validateResources(store.Pending.Resources) != nil {
		return ErrUnknown
	}
	return nil
}

func validateOperation(id string, operation operationState, maxDependencies int) error {
	if id != operation.ID || !validID(operation.ID) || !validID(operation.Meaning) || operation.ReserveBytes < 0 {
		return ErrUnknown
	}
	if len(operation.Dependencies) > maxDependencies {
		return ErrUnknown
	}
	return nil
}
