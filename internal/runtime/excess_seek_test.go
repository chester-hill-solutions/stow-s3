package runtime

import (
	"context"
	"fmt"
	"io"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func TestExcessReadAfterSeek(t *testing.T) {
	for _, offset := range []int64{0, 1, 3, 4} {
		t.Run(fmt.Sprintf("offset=%d", offset), func(t *testing.T) {
			f, store := saveBodyFixture(t)
			f.instance.store = &completionSeekStore{saveBodyStore: store, body: "old!"}
			reader, _, err := f.instance.OpenObject(context.Background(), "probe", "target")
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close()
			if _, err := reader.(io.Seeker).Seek(offset, io.SeekStart); err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(reader)
			want := "old"[min(offset, 3):]
			if string(data) != want || (offset < 4 && err == nil) || (offset == 4 && err != nil) {
				t.Errorf("seek exposed excess or changed EOF: offset=%d data=%q error=%v", offset, data, err)
			}
			if err := reader.Close(); err != nil {
				t.Fatal(err)
			}
			assertSaveBodyCounts(t, store, int(max(4-offset, 0)), 1)
		})
	}
}

type excessRangeStore struct{ *completionSeekStore }

func (s *excessRangeStore) OpenObjectRange(ctx context.Context, bucket, key string, _ storage.ObjectRange) (io.ReadCloser, *storage.ObjectMeta, error) {
	return s.GetObject(ctx, bucket, key)
}

func TestExcessRangeRead(t *testing.T) {
	for _, native := range []bool{false, true} {
		for _, length := range []int64{0, 2} {
			t.Run(fmt.Sprintf("native=%t/length=%d", native, length), func(t *testing.T) {
				f, store := saveBodyFixture(t)
				base := &completionSeekStore{saveBodyStore: store, body: "old!"}
				f.instance.store = base
				if native {
					f.instance.store = &excessRangeStore{base}
				}
				reader, object, err := f.instance.OpenObjectRange(context.Background(), "probe", "target", storage.ObjectRange{Length: length})
				if err != nil {
					t.Fatal(err)
				}
				defer reader.Close()
				data, err := io.ReadAll(reader)
				if string(data) != "old"[:length] || object.Size != 3 || (native && err == nil) || (!native && err != nil) {
					t.Errorf("range contract: data=%q size=%d error=%v native=%t", data, object.Size, err, native)
				}
				if err := reader.Close(); err != nil {
					t.Fatal(err)
				}
				consumed := int(length)
				if native {
					consumed++
				}
				assertSaveBodyCounts(t, store, consumed, 1)
			})
		}
	}
}
