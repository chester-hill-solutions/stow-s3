package benchmark_test

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"testing"

	stow "github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

func BenchmarkManagedSave(b *testing.B) {
	b.Run("1048576B", func(b *testing.B) {
		if b.N > 256 {
			b.Fatal("use a fixed benchtime of at most 256x to bound retained save receipts")
		}
		ctx := context.Background()
		runtime, err := stow.OpenFilesystem(stow.FilesystemOptions{Dir: b.TempDir(), Options: stow.Options{MaxBytes: 256 << 20, MaxObjects: 4096}})
		if err != nil {
			b.Fatal(err)
		}
		defer runtime.Close()
		if err := runtime.CreateBucket(ctx, "benchmark"); err != nil {
			b.Fatal(err)
		}
		body := make([]byte, 1<<20)
		if _, err := rand.Read(body); err != nil {
			b.Fatal(err)
		}
		b.SetBytes(int64(len(body)))
		b.ReportAllocs()
		b.ResetTimer()
		for index := range b.N {
			binary.LittleEndian.PutUint64(body, uint64(index))
			result, err := runtime.SaveObject(ctx, "benchmark", "saved", body, stow.SaveOptions{Condition: stow.ReplacementCondition(), RequestKey: fmt.Sprintf("measured-%d", index)})
			if err != nil || result.Outcome != stow.SaveCommitted || result.Replayed {
				b.Fatalf("save was not newly committed: %+v, %v", result, err)
			}
		}
		b.StopTimer()
		object, err := runtime.GetObject(ctx, "benchmark", "saved")
		if err != nil || len(object.Data) != len(body) || sha256.Sum256(object.Data) != sha256.Sum256(body) {
			b.Fatalf("saved payload verification failed: %v", err)
		}
	})
}
