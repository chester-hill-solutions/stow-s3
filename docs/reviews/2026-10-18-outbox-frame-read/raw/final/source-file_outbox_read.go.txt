package runthrough

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
)

func readOutboxFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	var header [outboxProtoPrefixSize]byte
	n, err := file.ReadAt(header[:], 0)
	if err != nil && err != io.EOF {
		return nil, err
	}
	if n < len(header) {
		return append([]byte(nil), header[:n]...), nil
	}
	native := string(header[:len(outboxProtoMagic)]) == outboxProtoMagic
	if native && int64(binary.LittleEndian.Uint32(header[len(outboxProtoMagic):]))+int64(outboxProtoPrefixSize) != info.Size() {
		return nil, fmt.Errorf("invalid protobuf outbox length")
	}
	return readOutboxRemainder(file, info.Size(), native)
}

func readOutboxRemainder(file *os.File, size int64, native bool) ([]byte, error) {
	size = max(size, 0)
	if size >= math.MaxInt {
		return nil, fmt.Errorf("outbox exceeds platform size limit")
	}
	data := make([]byte, size+1)
	n, err := file.ReadAt(data, 0)
	if err == io.EOF {
		return data[:n], nil
	}
	if err != nil {
		return nil, err
	}
	if native {
		return nil, fmt.Errorf("invalid protobuf outbox length")
	}
	if _, err := file.Seek(int64(n), io.SeekStart); err != nil {
		return nil, err
	}
	tail, err := io.ReadAll(file)
	return append(data, tail...), err
}
