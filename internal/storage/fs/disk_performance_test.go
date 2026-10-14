package fs

import (
	"context"
	"io"
	"runtime"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

type repeatingDiskReader struct{ remaining int64 }

func (r *repeatingDiskReader) Read(data []byte) (int, error) {
	if r.remaining == 0 {
		return 0, io.EOF
	}
	n := len(data)
	if int64(n) > r.remaining {
		n = int(r.remaining)
	}
	for i := range data[:n] {
		data[i] = 37
	}
	r.remaining -= int64(n)
	return n, nil
}
func BenchmarkDiskObject(b *testing.B) {
	for _, size := range []struct {
		name  string
		bytes int64
	}{{"1MiB", 1 << 20}, {"32MiB", 32 << 20}} {
		b.Run(size.name, func(b *testing.B) {
			s, err := NewFilesystemStore(b.TempDir())
			if err != nil {
				b.Fatal(err)
			}
			defer s.Close()
			ctx := context.Background()
			if err := s.CreateBucket(ctx, "assets"); err != nil {
				b.Fatal(err)
			}
			if _, err := s.PutObject(ctx, "assets", "source", &repeatingDiskReader{remaining: size.bytes}, storage.PutOptions{}); err != nil {
				b.Fatal(err)
			}
			for _, operation := range []string{"put", "copy"} {
				b.Run(operation, func(b *testing.B) {
					b.ReportAllocs()
					b.SetBytes(size.bytes)
					for n := 0; n < b.N; n++ {
						var meta *storage.ObjectMeta
						var err error
						if operation == "put" {
							meta, err = s.PutObject(ctx, "assets", "dest", &repeatingDiskReader{remaining: size.bytes}, storage.PutOptions{})
						} else {
							meta, err = s.CopyObject(ctx, "assets", "source", "assets", "dest")
						}
						if err != nil || meta.Size != size.bytes {
							b.Fatalf("meta=%+v err=%v", meta, err)
						}
					}
				})
			}
		})
	}
}
func TestDiskPutAndCopyAllocationBounded(t *testing.T) {
	s, err := NewFilesystemStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.CreateBucket(context.Background(), "assets"); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	const size = 32 << 20
	if _, err := s.PutObject(ctx, "assets", "source", &repeatingDiskReader{remaining: size}, storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	for _, operation := range []string{"put", "copy"} {
		t.Run(operation, func(t *testing.T) {
			if operation == "copy" && runtime.GOOS == "windows" {
				t.Skip("Windows materializes captured payloads before replacement")
			}
			result := testing.Benchmark(func(b *testing.B) {
				for n := 0; n < b.N; n++ {
					var err error
					if operation == "put" {
						_, err = s.PutObject(ctx, "assets", "dest", &repeatingDiskReader{remaining: size}, storage.PutOptions{})
					} else {
						_, err = s.CopyObject(ctx, "assets", "source", "assets", "dest")
					}
					if err != nil {
						b.Fatal(err)
					}
				}
			})
			if result.AllocedBytesPerOp() > 1<<20 {
				t.Fatalf("%s allocated %d bytes/op for 32 MiB", operation, result.AllocedBytesPerOp())
			}
		})
	}
}
