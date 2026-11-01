package runthrough

import (
	"fmt"
	"reflect"
	"runtime"
	"testing"
)

func TestOutboxStateCloneDoesNotDuplicateDeletedCapacity(t *testing.T) {
	state := cloneBenchFixture(128, true)
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	clone := state.clone()
	runtime.GC()
	runtime.ReadMemStats(&after)
	fmt.Printf("STOWCLONECAPACITY: {\"heapBefore\":%d,\"heapAfter\":%d,\"growth\":%d}\n", before.HeapAlloc, after.HeapAlloc, int64(after.HeapAlloc)-int64(before.HeapAlloc))
	if !reflect.DeepEqual(state, clone) {
		t.Fatal("clone changed retained state")
	}
	if after.HeapAlloc > before.HeapAlloc && after.HeapAlloc-before.HeapAlloc > 2<<20 {
		t.Fatal("small snapshot duplicates deleted map capacity")
	}
	runtime.KeepAlive(state)
	runtime.KeepAlive(clone)
}
