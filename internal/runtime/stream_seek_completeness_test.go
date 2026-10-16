package runtime

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

type completionSeekStore struct {
	*saveBodyStore
	body string
}

func (s *completionSeekStore) GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, *storage.ObjectMeta, error) {
	s.gets++
	meta, err := s.MemoryStore.HeadObject(ctx, bucket, key)
	if err != nil {
		return nil, nil, err
	}
	return &completionSeekReader{Reader: bytes.NewReader([]byte(s.body)), store: s.saveBodyStore}, meta, nil
}

type completionSeekReader struct {
	*bytes.Reader
	store *saveBodyStore
}

func (r *completionSeekReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.store.bytes += n
	return n, err
}
func (r *completionSeekReader) Close() error { r.store.closes++; return nil }

type completionRangeStore struct{ *completionSeekStore }

func (s *completionRangeStore) OpenObjectRange(ctx context.Context, bucket, key string, span storage.ObjectRange) (io.ReadCloser, *storage.ObjectMeta, error) {
	reader, meta, err := s.GetObject(ctx, bucket, key)
	if err != nil {
		return nil, nil, err
	}
	seeker := reader.(*completionSeekReader)
	end := min(int64(len(s.body)), span.Offset+span.Length)
	start := min(span.Offset, end)
	seeker.Reader = bytes.NewReader([]byte(s.body)[start:end])
	return seeker, meta, nil
}

func TestStreamSeekCompleteness(t *testing.T) {
	for _, body := range []string{"old", "ol"} {
		for _, mode := range []string{"start", "current", "end", "rewind", "failed", "at-end", "beyond-end"} {
			t.Run(fmt.Sprintf("body=%s/%s", body, mode), func(t *testing.T) { checkStreamSeek(t, body, mode) })
		}
	}
}

func checkStreamSeek(t *testing.T, body, mode string) {
	t.Helper()
	f, store := saveBodyFixture(t)
	f.instance.store = &completionSeekStore{saveBodyStore: store, body: body}
	reader, _, err := f.instance.OpenObject(context.Background(), "probe", "target")
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	seeker, ok := reader.(io.ReadSeeker)
	if !ok {
		t.Fatal("stream erased seeking")
	}
	position := seekCompletionPosition(t, seeker, mode, len(body))
	data, err := io.ReadAll(reader)
	want := io.ErrUnexpectedEOF
	if body == "old" || position >= 3 {
		want = nil
	}
	if err != want || string(data) != body[min(position, len(body)):] {
		t.Errorf("seek stream=%q error=%v want=%v position=%d", data, err, want, position)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if store.closes != 1 {
		t.Fatalf("closes=%d", store.closes)
	}
}

func seekCompletionPosition(t *testing.T, seeker io.ReadSeeker, mode string, length int) int {
	t.Helper()
	offset, whence := int64(1), io.SeekStart
	switch mode {
	case "current", "rewind", "failed":
		if _, err := io.ReadFull(seeker, make([]byte, 1)); err != nil {
			t.Fatal(err)
		}
		offset, whence = 0, io.SeekCurrent
		if mode == "rewind" {
			offset, whence = 0, io.SeekStart
		}
		if mode == "failed" {
			if _, err := seeker.Seek(-1, io.SeekStart); err == nil {
				t.Fatal("negative seek succeeded")
			}
			return 1
		}
	case "end":
		offset, whence = -1, io.SeekEnd
	case "at-end":
		offset, whence = int64(length), io.SeekStart
	case "beyond-end":
		offset, whence = 4, io.SeekStart
	}
	position, err := seeker.Seek(offset, whence)
	if err != nil {
		t.Fatal(err)
	}
	return int(position)
}

func TestStreamRangeCompleteness(t *testing.T) {
	for _, native := range []bool{false, true} {
		for _, body := range []string{"old", "ol"} {
			for _, span := range []storage.ObjectRange{{Offset: 1, Length: 2}, {Offset: 1, Length: 1}, {Offset: 3, Length: 0}} {
				t.Run(fmt.Sprintf("native=%t/body=%s/span=%+v", native, body, span), func(t *testing.T) { checkStreamRange(t, native, body, span) })
			}
		}
	}
}

func checkStreamRange(t *testing.T, native bool, body string, span storage.ObjectRange) {
	t.Helper()
	f, store := saveBodyFixture(t)
	seekStore := &completionSeekStore{saveBodyStore: store, body: body}
	f.instance.store = seekStore
	if native {
		f.instance.store = &completionRangeStore{seekStore}
	}
	reader, object, err := f.instance.OpenObjectRange(context.Background(), "probe", "target", span)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	end := min(int64(len(body)), span.Offset+span.Length)
	start := min(span.Offset, end)
	want := io.ErrUnexpectedEOF
	if end-start == span.Length {
		want = nil
	}
	if err != want || string(data) != body[start:end] || object.Size != 3 || object.VersionID != f.before.VersionID {
		t.Errorf("range data=%q error=%v want=%v object=%+v", data, err, want, object)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if store.closes != 1 {
		t.Fatalf("range closes=%d", store.closes)
	}
}
