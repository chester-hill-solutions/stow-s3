package runtime

import (
	"io"
	"sync"
)

var streamCopyBuffers sync.Pool

func (r *objectStream) WriteTo(writer io.Writer) (int64, error) {
	buffer, _ := streamCopyBuffers.Get().(*[]byte)
	if buffer == nil {
		data := make([]byte, 128<<10)
		buffer = &data
	}
	defer streamCopyBuffers.Put(buffer)
	return io.CopyBuffer(struct{ io.Writer }{writer}, struct{ io.Reader }{r}, *buffer)
}
