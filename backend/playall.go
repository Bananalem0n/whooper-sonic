package backend

import (
	"context"
	"errors"
	"math/rand"
	"sync"
	"time"

	"github.com/charlievieth/strcase"
	"github.com/supersonic-app/supersonic/backend/mediaprovider"
	"github.com/supersonic-app/supersonic/backend/player"
	"github.com/supersonic-app/supersonic/sharedutil"
)

const (
	// number of tracks to pull from the library iterator between
	// pauses when draining the library for Shuffle All
	libraryDrainChunkSize = 100
	// pause between chunk pulls to avoid hammering the server
	libraryDrainPause = 100 * time.Millisecond
	// How many tracks must remain ahead of the currently playing one
	// before the next library batch is fetched. This must be comfortably
	// more than one track: the playback engine hands the next track to the
	// player when 10 seconds remain (see playbackEngine.needToSetNextTrack),
	// and an in-order batch fetch can take much longer than that, since the
	// Subsonic all-tracks iterator issues one album request per album.
	// Refilling this far ahead keeps a next track always present, so gapless
	// playback survives every batch boundary.
	libraryLookahead = 20
)

// shouldRefillLibraryQueue reports whether the queue has been consumed to
// within libraryLookahead tracks of its end and so needs another batch.
func shouldRefillLibraryQueue(queueLen, nowPlayingIdx int) bool {
	if nowPlayingIdx < 0 {
		return false // nothing playing; nothing to refill for
	}
	return queueLen-1-nowPlayingIdx < libraryLookahead
}

// libraryPlayback progressively supplies the tracks of the entire library
// for the Play All and Shuffle All features.
//
// In in-order mode, tracks are pulled from the iterator on demand.
// In shuffle mode, a background goroutine drains the iterator into a pool
// from which NextBatch draws tracks at random (incremental Fisher-Yates),
// guaranteeing each track is enqueued exactly once.
type libraryPlayback struct {
	shuffle bool
	iter    mediaprovider.TrackIterator

	mutex       sync.Mutex
	cond        *sync.Cond
	pool        []*mediaprovider.Track // shuffle mode: tracks not yet enqueued
	doneLoading bool                   // shuffle mode: iterator fully drained
	iterDone    bool                   // in-order mode: iterator exhausted
	canceled    bool
}

// newLibraryPlayback creates a libraryPlayback reading from iter.
// filter, if non-nil, excludes tracks from shuffle mode (ignored in-order).
// If ctx is non-nil, the libraryPlayback is canceled when ctx is done.
func newLibraryPlayback(ctx context.Context, iter mediaprovider.TrackIterator, shuffle bool, filter func(*mediaprovider.Track) bool) *libraryPlayback {
	lp := &libraryPlayback{shuffle: shuffle, iter: iter}
	lp.cond = sync.NewCond(&lp.mutex)
	if shuffle {
		go lp.drainIterator(filter)
	}
	if ctx != nil {
		context.AfterFunc(ctx, lp.Cancel)
	}
	return lp
}

// NextBatch returns up to n more tracks to enqueue.
// In shuffle mode it blocks until at least one track is available,
// the library is exhausted, or the libraryPlayback is canceled.
// A nil/empty return means no more tracks will be supplied.
func (lp *libraryPlayback) NextBatch(n int) []*mediaprovider.Track {
	if lp.shuffle {
		return lp.nextShuffledBatch(n)
	}
	return lp.nextInOrderBatch(n)
}

// Done reports whether this libraryPlayback will supply no further tracks.
func (lp *libraryPlayback) Done() bool {
	lp.mutex.Lock()
	defer lp.mutex.Unlock()
	if lp.canceled {
		return true
	}
	if lp.shuffle {
		return lp.doneLoading && len(lp.pool) == 0
	}
	return lp.iterDone
}

// Cancel stops the libraryPlayback: the drain goroutine (if any) exits
// and all subsequent NextBatch calls return nil.
func (lp *libraryPlayback) Cancel() {
	lp.mutex.Lock()
	lp.canceled = true
	lp.pool = nil
	lp.cond.Broadcast()
	lp.mutex.Unlock()
}

func (lp *libraryPlayback) isCanceled() bool {
	lp.mutex.Lock()
	defer lp.mutex.Unlock()
	return lp.canceled
}

func (lp *libraryPlayback) nextInOrderBatch(n int) []*mediaprovider.Track {
	lp.mutex.Lock()
	if lp.canceled || lp.iterDone {
		lp.mutex.Unlock()
		return nil
	}
	lp.mutex.Unlock()

	var batch []*mediaprovider.Track
	for len(batch) < n {
		if lp.isCanceled() {
			return nil
		}
		tr := lp.iter.Next()
		if tr == nil {
			lp.mutex.Lock()
			lp.iterDone = true
			lp.mutex.Unlock()
			break
		}
		batch = append(batch, tr)
	}
	return batch
}

func (lp *libraryPlayback) nextShuffledBatch(n int) []*mediaprovider.Track {
	lp.mutex.Lock()
	defer lp.mutex.Unlock()
	for len(lp.pool) == 0 && !lp.doneLoading && !lp.canceled {
		lp.cond.Wait()
	}
	if lp.canceled || len(lp.pool) == 0 {
		return nil
	}
	if n > len(lp.pool) {
		n = len(lp.pool)
	}
	batch := make([]*mediaprovider.Track, 0, n)
	for i := 0; i < n; i++ {
		j := rand.Intn(len(lp.pool))
		batch = append(batch, lp.pool[j])
		last := len(lp.pool) - 1
		lp.pool[j] = lp.pool[last]
		lp.pool[last] = nil
		lp.pool = lp.pool[:last]
	}
	return batch
}

// PlayAllTracks plays the entire library, either in order or shuffled.
// The first batch begins playback immediately; the remainder of the
// library is enqueued progressively as playback nears the end of the queue.
func (p *PlaybackManager) PlayAllTracks(shuffle bool) error {
	s := p.engine.sm.GetServer()
	if s == nil {
		return errors.New("logged out")
	}
	p.cancelLibraryPlayback()

	var filter func(*mediaprovider.Track) bool
	if shuffle {
		filter = func(t *mediaprovider.Track) bool {
			skipKwd := p.cfg.SkipKeywordWhenShuffling
			return (skipKwd == "" || !strcase.Contains(t.Title, skipKwd)) &&
				(!p.cfg.SkipOneStarWhenShuffling || t.Rating != 1)
		}
	}
	lp := newLibraryPlayback(p.bgCtx, s.IterateTracks(""), shuffle, filter)
	batch := lp.NextBatch(p.appCfg.EnqueueBatchSize)
	if len(batch) == 0 {
		lp.Cancel()
		return errors.New("no tracks found")
	}
	p.LoadTracks(batch, Replace, false)
	if p.engine.replayGainCfg.Mode == ReplayGainAuto {
		p.SetReplayGainMode(player.ReplayGainTrack)
	}
	p.PlayFromBeginning()
	if !lp.Done() {
		p.libPlaybackLock.Lock()
		p.libPlayback = lp
		p.libPlaybackLock.Unlock()
	}
	return nil
}

// cancelLibraryPlayback cancels any active Play All/Shuffle All continuation.
func (p *PlaybackManager) cancelLibraryPlayback() {
	p.libPlaybackLock.Lock()
	lp := p.libPlayback
	p.libPlayback = nil
	p.libPlaybackLock.Unlock()
	if lp != nil {
		lp.Cancel()
	}
}

// enqueueNextLibraryBatch appends the next batch of library tracks to the
// queue if a Play All/Shuffle All continuation is active. Returns true if
// a continuation is active (whether or not a fetch was started).
func (p *PlaybackManager) enqueueNextLibraryBatch() bool {
	p.libPlaybackLock.Lock()
	lp := p.libPlayback
	if lp == nil {
		p.libPlaybackLock.Unlock()
		return false
	}
	if p.pendingLibraryEnqueue {
		p.libPlaybackLock.Unlock()
		return true
	}
	p.pendingLibraryEnqueue = true
	p.libPlaybackLock.Unlock()

	// NextBatch may block on server fetches; run async since this is
	// invoked from a playback engine callback
	go func() {
		batch := lp.NextBatch(p.appCfg.EnqueueBatchSize)
		if len(batch) > 0 && !lp.isCanceled() {
			items := sharedutil.CopyTrackSliceToMediaItemSlice(batch)
			// append via the command queue directly, bypassing the
			// continuation-cancellation check in LoadItems
			p.cmdQueue.LoadItems(items, Append, false)
		}
		p.libPlaybackLock.Lock()
		if lp.Done() && p.libPlayback == lp {
			p.libPlayback = nil
		}
		p.pendingLibraryEnqueue = false
		p.libPlaybackLock.Unlock()
	}()
	return true
}

// drainIterator pulls the entire library into the pool, pacing itself
// with a short pause between chunks so as not to hammer the server.
func (lp *libraryPlayback) drainIterator(filter func(*mediaprovider.Track) bool) {
	pulled := 0
	for {
		if lp.isCanceled() {
			return
		}
		tr := lp.iter.Next()
		if tr == nil {
			lp.mutex.Lock()
			lp.doneLoading = true
			lp.cond.Broadcast()
			lp.mutex.Unlock()
			return
		}
		if filter == nil || filter(tr) {
			lp.mutex.Lock()
			lp.pool = append(lp.pool, tr)
			lp.cond.Broadcast()
			lp.mutex.Unlock()
		}
		pulled++
		if pulled%libraryDrainChunkSize == 0 {
			time.Sleep(libraryDrainPause)
		}
	}
}
