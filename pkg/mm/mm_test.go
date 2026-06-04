package mm

import (
	"context"
	"reflect"
	"sync"
	"testing"
)

func TestFromCtxAndLookup(t *testing.T) {
	ctx := WithValues(context.Background(), map[string]any{"low": 10})

	if v := FromCtx(ctx, "low"); v != 10 {
		t.Fatalf("expected low=10, got %#v", v)
	}
	if v := FromCtx(ctx, "missing"); v != nil {
		t.Fatalf("expected missing=nil, got %#v", v)
	}

	if v, ok := Lookup(ctx, "low"); !ok || v != 10 {
		t.Fatalf("expected lookup low=(10,true), got (%#v,%v)", v, ok)
	}
	if v, ok := Lookup(ctx, "missing"); ok || v != nil {
		t.Fatalf("expected lookup missing=(nil,false), got (%#v,%v)", v, ok)
	}
}

func TestDecodeObjectNormalizesIntegersToInt(t *testing.T) {
	obj, err := DecodeObject([]byte(`{"low":10,"arr":[1,2]}`))
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if _, ok := obj["low"].(int); !ok {
		t.Fatalf("expected low to be int, got %T", obj["low"])
	}
	arr, ok := obj["arr"].([]any)
	if !ok {
		t.Fatalf("expected arr slice, got %T", obj["arr"])
	}
	if _, ok := arr[0].(int); !ok {
		t.Fatalf("expected arr[0] to be int, got %T", arr[0])
	}
}

func TestDecodeObjectNormalizesDecimalsToFloat64(t *testing.T) {
	obj, err := DecodeObject([]byte(`{"ratio":10.5,"exp":1e2}`))
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if _, ok := obj["ratio"].(float64); !ok {
		t.Fatalf("expected ratio to be float64, got %T", obj["ratio"])
	}
	if _, ok := obj["exp"].(float64); !ok {
		t.Fatalf("expected exp to be float64, got %T", obj["exp"])
	}
}

func TestStoreCopySemantics(t *testing.T) {
	store := NewStore()
	orig := map[string]any{"nested": map[string]any{"k": "v"}}
	store.Replace("req", orig)

	origNested := orig["nested"].(map[string]any)
	origNested["k"] = "changed"

	got := store.Get("req")
	gotNested := got["nested"].(map[string]any)
	if gotNested["k"] != "v" {
		t.Fatalf("store leaked caller mutation, got nested=%v", gotNested)
	}

	gotNested["k"] = "mutated"
	again := store.Get("req")
	if again["nested"].(map[string]any)["k"] != "v" {
		t.Fatalf("store leaked read mutation, got %v", again)
	}
}

func TestStoreConcurrentAccess(t *testing.T) {
	store := NewStore()
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			store.Replace("req", map[string]any{"n": i})
		}(i)
	}
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = store.Get("req")
		}()
	}
	wg.Wait()
}

func TestWithValuesCopiesInput(t *testing.T) {
	in := map[string]any{"v": map[string]any{"a": 1}}
	ctx := WithValues(context.Background(), in)
	in["v"].(map[string]any)["a"] = 2
	got := FromCtx(ctx, "v")
	if !reflect.DeepEqual(got, map[string]any{"a": 1}) {
		t.Fatalf("expected copied context values, got %#v", got)
	}
}

func TestStoreCollectionOperations(t *testing.T) {
	store := NewStore()
	store.ReplaceAll(map[string]map[string]any{
		"a": {"x": 1},
		"b": {"y": 2},
	})

	store.MergeAll(map[string]map[string]any{
		"a": {"z": 3},
		"c": {"w": 4},
	})
	store.Merge("b", map[string]any{"y": 5, "k": 6})
	store.DeleteKeys("b", []string{"k"})
	store.Delete("c")

	got := store.GetAll()
	want := map[string]map[string]any{
		"a": {"x": 1, "z": 3},
		"b": {"y": 5},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected store state: got=%#v want=%#v", got, want)
	}
}

func TestDecodeStoreNormalizesNumbers(t *testing.T) {
	store, err := DecodeStore([]byte(`{"req":{"low":10,"ratio":0.5}}`))
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if _, ok := store["req"]["low"].(int); !ok {
		t.Fatalf("expected low to be int, got %T", store["req"]["low"])
	}
	if _, ok := store["req"]["ratio"].(float64); !ok {
		t.Fatalf("expected ratio to be float64, got %T", store["req"]["ratio"])
	}
}
