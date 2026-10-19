package embedded

import (
	"context"
	"fmt"
	"io"
	"testing"
)

func TestEmbeddedStreamRejectsPrematureEOF(t *testing.T) {
	for _, together := range []bool{false, true} {
		t.Run(fmt.Sprintf("together=%t", together), func(t *testing.T) {
			store, outer := embeddedSaveBodyFixture(t)
			store.bodyErr, store.together = io.EOF, together
			reader, object, err := outer.OpenObject(context.Background(), "probe", "target")
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close()
			data, err := io.ReadAll(reader)
			if err != io.ErrUnexpectedEOF || string(data) != "o" || object.Size != 3 {
				t.Errorf("public stream=%q size=%d error=%v", data, object.Size, err)
			}
			if err := reader.Close(); err != nil {
				t.Fatal(err)
			}
			if store.bytes != 1 || store.closes != 1 {
				t.Fatalf("bytes=%d closes=%d", store.bytes, store.closes)
			}
			store.bodyErr = nil
			healthy, err := outer.GetObject(context.Background(), "probe", "target")
			if err != nil || string(healthy.Data) != "old" || healthy.ETag != object.ETag {
				t.Fatalf("healthy retry: object=%+v error=%v", healthy, err)
			}
		})
	}
}
