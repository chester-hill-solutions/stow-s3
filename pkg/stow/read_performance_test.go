package stow_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

func TestGetObjectOwnsReturnedBytesAndMetadata(t *testing.T) {
	ctx, instance := newPublicRuntime(t)
	if err := instance.CreateBucket(ctx, "assets"); err != nil {
		t.Fatal(err)
	}
	data := bytes.Repeat([]byte("x"), 1<<20)
	if _, err := instance.PutObject(ctx, "assets", "key", data, stow.PutOptions{Metadata: map[string]string{"owner": "caller"}}); err != nil {
		t.Fatal(err)
	}
	first, err := instance.GetObject(ctx, "assets", "key")
	if err != nil {
		t.Fatal(err)
	}
	first.Data[0] = 'z'
	first.Metadata["owner"] = "changed"
	second, err := instance.GetObject(ctx, "assets", "key")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(second.Data, data) || second.Metadata["owner"] != "caller" {
		t.Fatal("returned object aliases stored state")
	}
	second.Data[1] = 'y'
	if first.Data[1] != 'x' {
		t.Fatal("independent results share bytes")
	}
}

func BenchmarkEmbeddedGet(b *testing.B) {
	for _, size := range []struct {
		name  string
		bytes int
	}{{"4KiB", 4 << 10}, {"1MiB", 1 << 20}, {"8MiB", 8 << 20}} {
		b.Run(size.name, func(b *testing.B) {
			instance, err := stow.Open(stow.Options{})
			if err != nil {
				b.Fatal(err)
			}
			defer instance.Close()
			ctx := context.Background()
			if err := instance.CreateBucket(ctx, "assets"); err != nil {
				b.Fatal(err)
			}
			data := bytes.Repeat([]byte("x"), size.bytes)
			if _, err := instance.PutObject(ctx, "assets", "key", data, stow.PutOptions{}); err != nil {
				b.Fatal(err)
			}
			b.SetBytes(int64(size.bytes))
			b.ReportAllocs()
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				got, err := instance.GetObject(ctx, "assets", "key")
				if err != nil || len(got.Data) != len(data) || got.Data[0] != 'x' || got.Data[len(data)-1] != 'x' {
					b.Fatalf("get size=%d err=%v", len(got.Data), err)
				}
			}
		})
	}
}

func TestEmbeddedGetAllocationBounded(t *testing.T) {
	result := testing.Benchmark(func(b *testing.B) {
		instance, err := stow.Open(stow.Options{})
		if err != nil {
			b.Fatal(err)
		}
		defer instance.Close()
		ctx := context.Background()
		if err := instance.CreateBucket(ctx, "assets"); err != nil {
			b.Fatal(err)
		}
		data := bytes.Repeat([]byte("x"), 1<<20)
		if _, err := instance.PutObject(ctx, "assets", "key", data, stow.PutOptions{}); err != nil {
			b.Fatal(err)
		}
		b.ResetTimer()
		for n := 0; n < b.N; n++ {
			if _, err := instance.GetObject(ctx, "assets", "key"); err != nil {
				b.Fatal(err)
			}
		}
	})
	if result.AllocedBytesPerOp() > (1<<20)+(256<<10) {
		t.Fatalf("GET allocated %d bytes/op for 1 MiB", result.AllocedBytesPerOp())
	}
}
