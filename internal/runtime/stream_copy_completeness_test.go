package runtime

import (
	"fmt"
	"io"
	"testing"
)

func TestStreamCopyEOFCompleteness(t *testing.T) {
	for _, adapter := range []bool{false, true} {
		for _, together := range []bool{false, true} {
			for _, after := range []int{0, 1, 3} {
				t.Run(fmt.Sprintf("adapter=%t/together=%t/after=%d", adapter, together, after), func(t *testing.T) {
					f, store := saveBodyFixture(t)
					store.fault = &saveBodyFault{after: after, together: together, err: io.EOF}
					reader, object, err := openCompletionStream(f.instance, adapter)
					if err != nil {
						t.Fatal(err)
					}
					defer reader.Close()
					if _, ok := reader.(io.WriterTo); !ok {
						t.Fatal("stream erased WriteTo")
					}
					n, err := io.Copy(io.Discard, reader)
					want := io.ErrUnexpectedEOF
					if after == 3 {
						want = nil
					}
					if n != int64(after) || err != want || object.Size != 3 {
						t.Errorf("copy bytes=%d size=%d error=%v want=%v", n, object.Size, err, want)
					}
					if err := reader.Close(); err != nil {
						t.Fatal(err)
					}
					assertSaveBodyCounts(t, store, after, 1)
				})
			}
		}
	}
}
