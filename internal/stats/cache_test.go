package stats_test

import (
	"github.com/convin/webhook-ingest/internal/stats"
	"sync"
	"testing"
)

func TestCacheRecordAccumulates(t *testing.T) {
	c := stats.NewCache()

	c.Record("acc_1", 30)
	c.Record("acc_1", 12)
	c.Record("acc_2", 5)

	got := c.Get("acc_1")
	if got.CallCount != 2 || got.TotalDurationSec != 42 {
		t.Fatalf("acc_1: got %+v, want CallCount=2 TotalDurationSec=42", got)
	}

	other := c.Get("acc_2")
	if other.CallCount != 1 || other.TotalDurationSec != 5 {
		t.Fatalf("acc_2: got %+v, want CallCount=1 TotalDurationSec=5", other)
	}
}

func TestCacheGetUnknownAccountIsZero(t *testing.T) {
	c := stats.NewCache()
	if got := c.Get("nobody"); got.CallCount != 0 || got.TotalDurationSec != 0 {
		t.Fatalf("got %+v, want zero value", got)
	}
}

func TestConcurrentRecord(t *testing.T) {
	c := stats.NewCache()

	const records = 100

	var wg sync.WaitGroup
	wg.Add(records)

	for i := 0; i < records; i++ {
		go func() {
			defer wg.Done()
			c.Record("acc_123", 10)
		}()
	}

	wg.Wait()

	got := c.Get("acc_123")

	if got.CallCount != records {
		t.Fatalf("got CallCount=%d, want %d", got.CallCount, records)
	}

	if got.TotalDurationSec != records*10 {
		t.Fatalf("got TotalDurationSec=%d, want %d",
			got.TotalDurationSec, records*10)
	}
}
