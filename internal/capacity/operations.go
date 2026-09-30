package capacity

import (
	"context"
	"encoding/json"
	"reflect"
)

func (n *Namespace) Operation(ctx context.Context, id string) (Operation, bool, error) {
	var out Operation
	var found bool
	err := n.access(ctx, func(_ *hostState, ns *namespaceState) error {
		op, ok := ns.Operations[id]
		out, found = op.Operation, ok
		return nil
	})
	return out, found, err
}

func (n *Namespace) BeginOperation(ctx context.Context, operation Operation) error {
	if !validID(operation.ID) || !validID(operation.Owner) || !validID(operation.Meaning) || operation.ReserveBytes < 0 {
		return ErrInvalid
	}
	return n.access(ctx, func(state *hostState, ns *namespaceState) error {
		if existing, ok := ns.Operations[operation.ID]; ok {
			if !sameOperation(existing.Operation, operation) {
				return ErrConflict
			}
			return nil
		}
		if len(ns.Operations) >= ns.Options.MaxOperations || len(operation.Dependencies) > ns.Options.MaxDependencies {
			return ErrFull
		}
		if err := verifyDependencies(ns, operation.Dependencies); err != nil {
			return err
		}
		encoded, err := json.Marshal(operationState{Operation: operation})
		if err != nil {
			return err
		}
		if len(encoded) > 64<<10 {
			return ErrFull
		}
		minimum := int64(len(encoded)+64) * 2
		if operation.ReserveBytes == 0 {
			operation.ReserveBytes = minimum
		}
		if operation.ReserveBytes < minimum {
			return ErrInvalid
		}
		ns.Operations[operation.ID] = operationState{Operation: operation}
		return n.host.write(state)
	})
}

func sameOperation(a, b Operation) bool {
	return a.ID == b.ID && a.Owner == b.Owner && a.Meaning == b.Meaning && reflect.DeepEqual(a.Dependencies, b.Dependencies) && (b.ReserveBytes == 0 || a.ReserveBytes == b.ReserveBytes)
}

func verifyDependencies(ns *namespaceState, dependencies []Resource) error {
	seen := map[string]bool{}
	for _, dependency := range dependencies {
		key := dependency.StoreID + "\x00" + dependency.ID
		if !validResource(dependency) || seen[key] {
			return ErrInvalid
		}
		seen[key] = true
		store, ok := ns.Stores[dependency.StoreID]
		if !ok {
			return ErrConflict
		}
		if store.Pending != nil {
			return ErrUnknown
		}
		found := false
		for _, resource := range store.Resources {
			if resource.ID == dependency.ID && resource.Version == dependency.Version && resource.Bytes == dependency.Bytes && resource.Class == dependency.Class {
				found = true
				break
			}
		}
		if !found {
			return ErrConflict
		}
	}
	return nil
}

func (n *Namespace) ReleaseOperation(ctx context.Context, id, owner string) error {
	if !validID(id) || !validID(owner) {
		return ErrInvalid
	}
	return n.access(ctx, func(state *hostState, ns *namespaceState) error {
		operation, ok := ns.Operations[id]
		if !ok {
			return ErrConflict
		}
		if operation.Owner != owner {
			return ErrConflict
		}
		if operation.Released {
			return nil
		}
		operation.Released = true
		ns.Operations[id] = operation
		return n.host.write(state)
	})
}

func (n *Namespace) CheckDependencies(ctx context.Context, resources []Resource) error {
	return n.access(ctx, func(_ *hostState, ns *namespaceState) error {
		for _, operation := range ns.Operations {
			if operation.Released {
				continue
			}
			for _, dependency := range operation.Dependencies {
				for _, resource := range resources {
					if dependency.StoreID == resource.StoreID && dependency.ID == resource.ID {
						return ErrHeld
					}
				}
			}
		}
		return nil
	})
}
