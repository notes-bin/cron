package cron

import (
	"fmt"
	"slices"
	"testing"
	"time"
)

func BenchmarkEntryByNextSort(b *testing.B) {
	sizes := []int{10, 100, 1000}
	now := time.Now()

	for _, n := range sizes {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			base := make([]*Entry, n)
			for i := range n {
				base[i] = &Entry{ID: EntryID(i + 1), Next: now.Add(time.Duration(n-i) * time.Millisecond)}
			}
			for b.Loop() {
				entries := slices.Clone(base)
				slices.SortFunc(entries, entryByNext)
			}
		})
	}
}

func BenchmarkAddRemoveBeforeStart(b *testing.B) {
	for b.Loop() {
		c := New()
		id := c.AddFunc(Every(time.Hour), func() {})
		c.Remove(id)
	}
}
