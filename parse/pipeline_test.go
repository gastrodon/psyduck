package parse

import "testing"

func TestLiteralResourceFunc(t *testing.T) {
	rs := make([]Resource, 5)
	for i := range rs {
		rs[i] = Resource{Ref: string(rune('a' + i))}
	}

	f := LiteralResourceFunc(rs...)
	sizes := []int{}
	total := []Resource{}
	for {
		chunk, err := f(2)
		if err != nil {
			t.Fatal(err)
		}
		if len(chunk) == 0 {
			break
		}
		sizes = append(sizes, len(chunk))
		total = append(total, chunk...)
	}

	if len(sizes) != 3 || sizes[0] != 2 || sizes[1] != 2 || sizes[2] != 1 {
		t.Fatalf("bad chunk sizes: %v", sizes)
	}
	for i, r := range total {
		if r.Ref != rs[i].Ref {
			t.Fatalf("order broken at %d: %q", i, r.Ref)
		}
	}

	// exhausted stream stays exhausted
	if chunk, err := f(2); err != nil || chunk != nil {
		t.Fatalf("want exhausted, got %v %v", chunk, err)
	}

	// max < 1 yields nothing
	f = LiteralResourceFunc(rs...)
	if chunk, err := f(0); err != nil || chunk != nil {
		t.Fatalf("max=0: want nil, got %v %v", chunk, err)
	}

	// empty literal exhausts immediately
	if chunk, err := LiteralResourceFunc()(4); err != nil || chunk != nil {
		t.Fatalf("empty: want nil, got %v %v", chunk, err)
	}
}
