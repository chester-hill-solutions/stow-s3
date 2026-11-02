package fs

import (
	"encoding/json"

	"github.com/chester-hill-solutions/stow-s3/internal/atomicfile"
)

// Atomic writes share one implementation and propagate file and directory sync errors.
func writeBytesAtomic(path string, data ...[]byte) error {
	return atomicfile.WriteParts(path, 0o644, data...)
}

func writeJSONAtomic(path string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return writeBytesAtomic(path, data)
}
