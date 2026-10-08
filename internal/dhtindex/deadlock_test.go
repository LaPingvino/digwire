package dhtindex

import (
	"os"
	"sync"
	"testing"
	"time"
)

// The engine asks the index to record swarm activity while holding its own lock, and the index
// asks the engine whether a hash is a user torrent. If the index holds its lock while asking, the
// two orders deadlock and the whole app freezes -- which is what happened in practice. So the
// callback must never be called with the index locked.
func TestQueueCrawlAsksTheEngineWithoutHoldingTheIndexLock(t *testing.T) {
	idx, tmp := createTestIndexer(t)
	defer os.RemoveAll(tmp)
	defer idx.Close()

	// Stands in for the engine lock: the real checker takes it, and the engine holds it while
	// calling into the index.
	var engineLock sync.Mutex
	idx.SetUserTorrentChecker(func(string) bool {
		engineLock.Lock()
		defer engineLock.Unlock()
		return false
	})

	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { // The engine: holds its lock, then writes to the index.
		defer wg.Done()
		<-start
		for i := 0; i < 200; i++ {
			engineLock.Lock()
			idx.RecordSwarmActivity("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "seeding something", 3, 5)
			engineLock.Unlock()
		}
	}()
	go func() { // A search: queues every result for a metadata crawl.
		defer wg.Done()
		<-start
		for i := 0; i < 200; i++ {
			idx.QueueCrawl("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
		}
	}()

	close(start)
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("deadlock: the index and the engine took each other's locks in opposite orders")
	}
}
