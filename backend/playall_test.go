package backend

import (
	"testing"
	"time"

	"github.com/supersonic-app/supersonic/backend/mediaprovider"
)

type fakeTrackIterator struct {
	tracks []*mediaprovider.Track
	idx    int
	delay  time.Duration // optional per-track delay to simulate network
}

func (f *fakeTrackIterator) Next() *mediaprovider.Track {
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	if f.idx >= len(f.tracks) {
		return nil
	}
	t := f.tracks[f.idx]
	f.idx++
	return t
}

func makeFakeTracks(n int) []*mediaprovider.Track {
	tracks := make([]*mediaprovider.Track, 0, n)
	for i := 0; i < n; i++ {
		tracks = append(tracks, &mediaprovider.Track{
			ID:     string(rune('a'+i%26)) + "-" + string(rune('0'+i/26%10)) + "-" + string(rune('0'+i/260)),
			Title:  "Track",
			Rating: i % 6, // some tracks rated 1
		})
	}
	return tracks
}

func drainAllBatches(t *testing.T, lp *libraryPlayback, batchSize int) []*mediaprovider.Track {
	t.Helper()
	var out []*mediaprovider.Track
	for i := 0; i < 10000; i++ { // guard against infinite loop
		batch := lp.NextBatch(batchSize)
		if len(batch) == 0 {
			return out
		}
		out = append(out, batch...)
	}
	t.Fatal("drainAllBatches did not terminate")
	return nil
}

func TestLibraryPlaybackInOrder(t *testing.T) {
	tracks := makeFakeTracks(25)
	lp := newLibraryPlayback(nil, &fakeTrackIterator{tracks: tracks}, false, nil)

	b1 := lp.NextBatch(10)
	b2 := lp.NextBatch(10)
	b3 := lp.NextBatch(10)
	if len(b1) != 10 || len(b2) != 10 || len(b3) != 5 {
		t.Fatalf("unexpected batch sizes: %d, %d, %d", len(b1), len(b2), len(b3))
	}
	got := append(append(b1, b2...), b3...)
	for i, tr := range got {
		if tr.ID != tracks[i].ID {
			t.Fatalf("track %d out of order: got %s want %s", i, tr.ID, tracks[i].ID)
		}
	}
	if !lp.Done() {
		t.Error("expected Done after iterator exhausted")
	}
	if b := lp.NextBatch(10); b != nil {
		t.Errorf("expected nil batch after done, got %d tracks", len(b))
	}
}

func TestLibraryPlaybackShuffleAllExactlyOnce(t *testing.T) {
	tracks := makeFakeTracks(250)
	lp := newLibraryPlayback(nil, &fakeTrackIterator{tracks: tracks}, true, nil)

	got := drainAllBatches(t, lp, 100)
	if len(got) != len(tracks) {
		t.Fatalf("expected %d tracks, got %d", len(tracks), len(got))
	}
	seen := make(map[*mediaprovider.Track]bool)
	for _, tr := range got {
		if seen[tr] {
			t.Fatalf("track %s enqueued more than once", tr.ID)
		}
		seen[tr] = true
	}
	// verify order actually shuffled (odds of identity permutation are negligible)
	identical := true
	for i, tr := range got {
		if tr != tracks[i] {
			identical = false
			break
		}
	}
	if identical {
		t.Error("shuffled output identical to input order")
	}
	if !lp.Done() {
		t.Error("expected Done after pool drained")
	}
}

func TestLibraryPlaybackShuffleFilter(t *testing.T) {
	tracks := makeFakeTracks(60)
	filter := func(tr *mediaprovider.Track) bool { return tr.Rating != 1 }
	lp := newLibraryPlayback(nil, &fakeTrackIterator{tracks: tracks}, true, filter)

	got := drainAllBatches(t, lp, 25)
	for _, tr := range got {
		if tr.Rating == 1 {
			t.Fatalf("filtered track %s (rating 1) was enqueued", tr.ID)
		}
	}
	want := 0
	for _, tr := range tracks {
		if tr.Rating != 1 {
			want++
		}
	}
	if len(got) != want {
		t.Fatalf("expected %d tracks after filter, got %d", want, len(got))
	}
}

func TestLibraryPlaybackCancel(t *testing.T) {
	tracks := makeFakeTracks(50)
	lp := newLibraryPlayback(nil, &fakeTrackIterator{tracks: tracks}, true, nil)

	if b := lp.NextBatch(10); len(b) != 10 {
		t.Fatalf("expected 10 tracks before cancel, got %d", len(b))
	}
	lp.Cancel()
	if b := lp.NextBatch(10); b != nil {
		t.Errorf("expected nil batch after cancel, got %d tracks", len(b))
	}
	if !lp.Done() {
		t.Error("expected Done after cancel")
	}
}

func TestLibraryPlaybackInOrderCancel(t *testing.T) {
	tracks := makeFakeTracks(50)
	lp := newLibraryPlayback(nil, &fakeTrackIterator{tracks: tracks}, false, nil)

	if b := lp.NextBatch(10); len(b) != 10 {
		t.Fatalf("expected 10 tracks before cancel, got %d", len(b))
	}
	lp.Cancel()
	if b := lp.NextBatch(10); b != nil {
		t.Errorf("expected nil batch after cancel, got %d tracks", len(b))
	}
	if !lp.Done() {
		t.Error("expected Done after cancel")
	}
}

func TestShouldRefillLibraryQueue(t *testing.T) {
	cases := []struct {
		name         string
		queueLen     int
		nowPlayingId int
		want         bool
	}{
		{"start of a fresh 100-track batch", 100, 0, false},
		{"midway through the queue", 100, 50, false},
		{"exactly at the lookahead boundary", 100, 100 - 1 - libraryLookahead, false},
		{"one track past the boundary", 100, 100 - libraryLookahead, true},
		{"on the final track", 100, 99, true},
		{"queue grown to 200, plenty ahead", 200, 100, false},
		{"nothing playing yet", 100, -1, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := shouldRefillLibraryQueue(c.queueLen, c.nowPlayingId)
			if got != c.want {
				remaining := c.queueLen - 1 - c.nowPlayingId
				t.Errorf("shouldRefillLibraryQueue(len=%d, idx=%d) = %v, want %v (remaining=%d, lookahead=%d)",
					c.queueLen, c.nowPlayingId, got, c.want, remaining, libraryLookahead)
			}
		})
	}
}

// The lookahead exists because in-order mode pulls from the iterator
// synchronously, and the Subsonic all-tracks iterator issues one album
// fetch per album. A batch can therefore take far longer than the 10
// seconds the engine's gapless prefetch allows.
func TestLibraryPlaybackInOrderSlowIteratorStillFillsBatch(t *testing.T) {
	tracks := makeFakeTracks(30)
	lp := newLibraryPlayback(nil, &fakeTrackIterator{tracks: tracks, delay: 3 * time.Millisecond}, false, nil)

	start := time.Now()
	batch := lp.NextBatch(20)
	elapsed := time.Since(start)

	if len(batch) != 20 {
		t.Fatalf("expected full batch of 20, got %d", len(batch))
	}
	// proves the fetch is slow enough to matter: 20 tracks * 3ms of latency
	if elapsed < 20*3*time.Millisecond {
		t.Errorf("expected fetch to take at least 60ms, took %v", elapsed)
	}
	for i, tr := range batch {
		if tr.ID != tracks[i].ID {
			t.Fatalf("track %d out of order: got %s want %s", i, tr.ID, tracks[i].ID)
		}
	}
}

func TestLibraryPlaybackShuffleReturnsPartialWhileLoading(t *testing.T) {
	// slow iterator: pool fills gradually; NextBatch should return
	// as soon as at least one track is available rather than waiting
	// for the full batch size
	tracks := makeFakeTracks(30)
	lp := newLibraryPlayback(nil, &fakeTrackIterator{tracks: tracks, delay: 2 * time.Millisecond}, true, nil)

	start := time.Now()
	b := lp.NextBatch(1000)
	if len(b) == 0 {
		t.Fatal("expected at least one track")
	}
	// should not have waited for the full drain (30 * 2ms plus pauses)
	if time.Since(start) > 2*time.Second {
		t.Error("NextBatch blocked far too long")
	}
	rest := drainAllBatches(t, lp, 1000)
	if len(b)+len(rest) != len(tracks) {
		t.Fatalf("expected %d total tracks, got %d", len(tracks), len(b)+len(rest))
	}
}
