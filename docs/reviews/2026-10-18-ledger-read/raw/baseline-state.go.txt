package capacity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf8"

	"github.com/chester-hill-solutions/stow-s3/internal/atomicfile"
	"github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"
)

func Open(options HostOptions) (*Host, error) {
	if options.Dir == "" || options.MaxBytes <= 0 || options.MaxNamespaces < 0 || options.MaxNamespaces > 256 {
		return nil, ErrInvalid
	}
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		return nil, ErrInvalid
	}
	if options.MaxNamespaces == 0 {
		options.MaxNamespaces = 16
	}
	dir, err := filepath.Abs(options.Dir)
	if err != nil {
		return nil, err
	}
	h := &Host{dir: dir, options: options}
	err = h.access(context.Background(), func(state *hostState) error { return h.write(state) })
	return h, err
}

func (h *Host) Bind(options NamespaceOptions) (*Namespace, error) {
	if !validID(options.ID) || options.MaxBytes <= 0 || options.RecoveryReserveBytes <= 0 || options.RecoveryReserveBytes >= options.MaxBytes {
		return nil, ErrInvalid
	}
	if options.MaxOperations == 0 {
		options.MaxOperations = 256
	}
	if options.MaxDependencies == 0 {
		options.MaxDependencies = 32
	}
	if options.MaxOperations < 1 || options.MaxOperations > 1024 || options.MaxDependencies < 1 || options.MaxDependencies > 256 {
		return nil, ErrInvalid
	}
	err := h.access(context.Background(), func(state *hostState) error {
		if current, ok := state.Namespaces[options.ID]; ok {
			if current.Options != options {
				return ErrConflict
			}
			return nil
		}
		if len(state.Namespaces) >= state.MaxNamespaces {
			return ErrFull
		}
		state.Namespaces[options.ID] = &namespaceState{Options: options, Stores: map[string]*storeState{}, Operations: map[string]operationState{}}
		return h.write(state)
	})
	return &Namespace{host: h, id: options.ID}, err
}

func (n *Namespace) BindStore(storeID, root string) error {
	if !validID(storeID) || root == "" {
		return ErrInvalid
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	return n.access(context.Background(), func(state *hostState, ns *namespaceState) error {
		for id, candidate := range state.Namespaces {
			for sid, binding := range candidate.Stores {
				if sid == storeID || binding.Root == abs {
					if id != n.id || sid != storeID || binding.Root != abs {
						return ErrConflict
					}
					return nil
				}
			}
		}
		if len(ns.Stores) >= 256 {
			return ErrFull
		}
		ns.Stores[storeID] = &storeState{Root: abs}
		return n.host.write(state)
	})
}

func (n *Namespace) Binding() (string, string) { return n.host.dir, n.id }

func validID(value string) bool {
	return len(value) > 0 && len(value) <= 256 && utf8.ValidString(value) && strings.TrimSpace(value) == value
}

func (n *Namespace) access(ctx context.Context, visit func(*hostState, *namespaceState) error) error {
	if n == nil || n.host == nil {
		return ErrInvalid
	}
	return n.host.access(ctx, func(state *hostState) error {
		ns, ok := state.Namespaces[n.id]
		if !ok {
			return ErrConflict
		}
		return visit(state, ns)
	})
}

func (h *Host) access(ctx context.Context, visit func(*hostState) error) error {
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	lock, err := workspace.AcquireCapture(h.dir, "capacity-host")
	if err != nil {
		return err
	}
	defer lock.Release()
	state, err := h.read()
	if err != nil {
		return err
	}
	return visit(state)
}

func (h *Host) read() (*hostState, error) {
	path := filepath.Join(h.dir, "capacity.json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &hostState{Version: 1, MaxBytes: h.options.MaxBytes, MaxNamespaces: h.options.MaxNamespaces, Namespaces: map[string]*namespaceState{}}, nil
	}
	if err != nil {
		return nil, err
	}
	if len(data) > maxStateBytes {
		return nil, ErrUnknown
	}
	var state hostState
	if json.Unmarshal(data, &state) != nil || state.Version != 1 || state.Namespaces == nil {
		return nil, ErrUnknown
	}
	integrity := state.Integrity
	encoded, err := encodeState(&state)
	if err != nil || integrity != state.Integrity || len(encoded) != len(data) {
		return nil, ErrUnknown
	}
	if state.MaxBytes != h.options.MaxBytes || state.MaxNamespaces != h.options.MaxNamespaces {
		return nil, ErrConflict
	}
	if err := validateState(&state); err != nil {
		return nil, err
	}
	return &state, nil
}

func encodeState(state *hostState) ([]byte, error) {
	state.Integrity = ""
	data, err := json.Marshal(state)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(append([]byte("stow-capacity-v1\x00"), data...))
	state.Integrity = hex.EncodeToString(digest[:])
	return json.Marshal(state)
}

func (h *Host) write(state *hostState) error {
	data, err := encodeState(state)
	if err != nil {
		return err
	}
	if len(data) > maxStateBytes {
		return ErrFull
	}
	if err := checkCapacity(state, int64(len(data))*2); err != nil {
		return err
	}
	if err := atomicfile.Write(filepath.Join(h.dir, "capacity.json"), data, 0o600); err != nil {
		return errors.Join(ErrUnknown, err)
	}
	return syncAncestors(h.dir)
}

func syncAncestors(dir string) error {
	for {
		file, err := os.Open(dir)
		if err != nil {
			return err
		}
		err = file.Sync()
		closeErr := file.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil
		}
		dir = parent
	}
}
