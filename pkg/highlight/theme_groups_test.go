package highlight

import (
	"fmt"
	"sync"
	"testing"
)

func TestCaptureGroupRegistry(t *testing.T) {
	// More than 256 groups must not wrap into regex IDs. Different fallback
	// choices can share a public theme name without overwriting each other.
	for i := 0; i < 300; i++ {
		name := fmt.Sprintf("test.capture%d", i)
		g := newCaptureGroup(name, 0)
		if g.String() != "treesitter."+name || g.Fallback() != 0 || g != newCaptureGroup(name, 0) {
			t.Fatal("unstable capture group")
		}
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				g := newCaptureGroup("concurrent", 0)
				if g.String() != "treesitter.concurrent" {
					t.Error("concurrent registration")
				}
			}
		}()
	}
	wg.Wait()
	a, b := newCaptureGroup("same", 0), newCaptureGroup("same", 1)
	if a == b || a.String() != b.String() || b.Fallback() != 1 {
		t.Fatal("fallbacks collided")
	}
}
