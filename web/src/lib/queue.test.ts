import { describe, expect, it } from 'vitest'
import {
  advance,
  addToQueue,
  currentTrackId,
  cycleRepeat,
  emptyQueue,
  moveInQueue,
  queueFromIds,
  queueTrackIds,
  removeFromQueue,
  setQueue,
  toggleShuffle,
  type QueueState,
} from './queue'

/** Builds a queue in natural order from a list of track ids. */
function natural(ids: number[]): QueueState {
  return setQueue(emptyQueue, ids, null, false)
}

describe('setQueue', () => {
  it('starts at the requested track', () => {
    const q = setQueue(emptyQueue, [10, 20, 30], null, false)
    expect(currentTrackId(q)).toBe(10)
    expect(q.cursor).toBe(0)
  })

  it('keeps the current track playing when the queue is replaced', () => {
    const first = setQueue(emptyQueue, [1, 2, 3], null, false)
    const playingThree = { ...first, cursor: 2 }
    // A new queue arrives while track 3 is playing; the cursor should follow
    // the track, not stay on the same index.
    const next = setQueue(playingThree, [7, 8, 9, 3], 3, false)
    expect(currentTrackId(next)).toBe(3)
  })

  it('shuffles when asked, but still lands on the current track', () => {
    const natural_ = natural([1, 2, 3, 4, 5])
    const atEnd = { ...natural_, cursor: 4 }
    const shuffled = setQueue(atEnd, [1, 2, 3, 4, 5], 5, true)
    expect(currentTrackId(shuffled)).toBe(5)
    // A shuffle must actually reorder something, or "shuffle" is a no-op.
    expect(shuffled.sequence).not.toEqual([0, 1, 2, 3, 4])
  })
})

describe('addToQueue', () => {
  it('appends new tracks', () => {
    const q = addToQueue(natural([1, 2]), [3, 4])
    expect(q.order).toEqual([1, 2, 3, 4])
  })

  it('skips tracks already queued', () => {
    const q = addToQueue(natural([1, 2, 3]), [2, 3, 4])
    expect(q.order).toEqual([1, 2, 3, 4])
  })

  it('returns the same object when nothing is added', () => {
    const q = natural([1, 2])
    expect(addToQueue(q, [1, 2])).toBe(q)
  })
})

describe('removeFromQueue', () => {
  it('removes the item and renumbers the sequence', () => {
    const q = removeFromQueue(natural([1, 2, 3, 4]), 1)
    expect(q.order).toEqual([1, 3, 4])
    // sequence must still be a valid permutation of the new order's indices
    expect([...q.sequence].sort()).toEqual([0, 1, 2])
  })

  it('keeps playing the same track when a later item is removed', () => {
    let q = natural([1, 2, 3, 4])
    q = { ...q, cursor: 2 } // playing track 3
    q = removeFromQueue(q, 3) // remove track 4
    expect(currentTrackId(q)).toBe(3)
  })

  it('steps the cursor back when an earlier item is removed', () => {
    let q = natural([1, 2, 3, 4])
    q = { ...q, cursor: 3 } // playing track 4
    q = removeFromQueue(q, 0) // remove track 1
    expect(currentTrackId(q)).toBe(4)
  })

  it('ignores an out-of-range index', () => {
    const q = natural([1, 2])
    expect(removeFromQueue(q, 9)).toBe(q)
  })
})

describe('moveInQueue', () => {
  it('moves an item forward', () => {
    const q = moveInQueue(natural([1, 2, 3, 4]), 0, 2)
    expect(queueTrackIds(q)).toEqual([2, 3, 1, 4])
  })

  it('moves an item backward', () => {
    const q = moveInQueue(natural([1, 2, 3, 4]), 3, 1)
    expect(queueTrackIds(q)).toEqual([1, 4, 2, 3])
  })

  it('keeps the cursor on the same track after a move', () => {
    let q = natural([1, 2, 3, 4])
    q = { ...q, cursor: 0 } // playing track 1
    q = moveInQueue(q, 0, 3)
    expect(currentTrackId(q)).toBe(1)
  })

  it('clamps an out-of-range destination', () => {
    const q = moveInQueue(natural([1, 2, 3]), 0, 99)
    expect(queueTrackIds(q)).toEqual([2, 3, 1])
  })

  it('is a no-op when from equals to', () => {
    const q = natural([1, 2, 3])
    expect(moveInQueue(q, 1, 1)).toBe(q)
  })
})

describe('advance', () => {
  it('steps forward one track', () => {
    const q = natural([1, 2, 3])
    expect(advance(q, 1)).toEqual({ cursor: 1, stop: false, restart: false })
  })

  it('stops at the end of a non-repeating queue', () => {
    const q = { ...natural([1, 2]), cursor: 1 }
    expect(advance(q, 1).stop).toBe(true)
  })

  it('loops when repeat is all', () => {
    const q = { ...natural([1, 2]), cursor: 1, repeat: 'all' as const }
    expect(advance(q, 1)).toEqual({ cursor: 0, stop: false, restart: false })
  })

  it('restarts the track when repeat is one', () => {
    const q = { ...natural([1, 2, 3]), repeat: 'one' as const }
    expect(advance(q, 1)).toEqual({ cursor: 0, stop: false, restart: true })
  })

  it('still goes back on the previous track when repeat is one', () => {
    // Otherwise "previous" would be stuck looping the same track forever.
    const q = { ...natural([1, 2, 3]), cursor: 1, repeat: 'one' as const }
    expect(advance(q, -1).restart).toBe(false)
    expect(advance(q, -1).cursor).toBe(0)
  })

  it('restarts from the beginning when stepping back past the start', () => {
    const q = { ...natural([1, 2, 3]), cursor: 0 }
    expect(advance(q, -1)).toEqual({ cursor: 0, stop: false, restart: true })
  })

  it('wraps to the end when stepping back with repeat all', () => {
    const q = { ...natural([1, 2, 3]), cursor: 0, repeat: 'all' as const }
    expect(advance(q, -1).cursor).toBe(2)
  })

  it('treats an empty queue as stopped', () => {
    expect(advance(emptyQueue, 1).stop).toBe(true)
  })
})

describe('toggleShuffle', () => {
  it('puts the current track first when shuffling on', () => {
    const q = toggleShuffle(natural([1, 2, 3, 4, 5]), 3)
    expect(q.shuffle).toBe(true)
    expect(currentTrackId(q)).toBe(3)
    expect(queueTrackIds(q)).toHaveLength(5)
  })

  it('contains every track exactly once', () => {
    const q = toggleShuffle(natural([1, 2, 3, 4, 5]), 2)
    expect([...queueTrackIds(q)].sort()).toEqual([1, 2, 3, 4, 5])
  })

  it('restores natural order when shuffling off', () => {
    let q = toggleShuffle(natural([1, 2, 3, 4]), 3)
    q = toggleShuffle(q, 3)
    expect(q.shuffle).toBe(false)
    expect(queueTrackIds(q)).toEqual([1, 2, 3, 4])
    expect(currentTrackId(q)).toBe(3)
  })
})

describe('cycleRepeat', () => {
  it('cycles off, all, one, off', () => {
    let q = emptyQueue
    expect((q = cycleRepeat(q)).repeat).toBe('all')
    expect((q = cycleRepeat(q)).repeat).toBe('one')
    expect((q = cycleRepeat(q)).repeat).toBe('off')
  })
})

describe('queueFromIds', () => {
  it('rebuilds a queue from a synced snapshot', () => {
    const q = queueFromIds([5, 6, 7], 6, false)
    expect(queueTrackIds(q)).toEqual([5, 6, 7])
    expect(currentTrackId(q)).toBe(6)
  })

  it('respects the incoming shuffle flag', () => {
    const q = queueFromIds([5, 6, 7], 5, true)
    expect(q.shuffle).toBe(true)
    expect([...queueTrackIds(q)].sort()).toEqual([5, 6, 7])
  })
})

describe('queueTrackIds', () => {
  it('returns tracks in playback order, not storage order', () => {
    const q = moveInQueue(natural([1, 2, 3]), 2, 0)
    expect(q.order).toEqual([1, 2, 3])
    expect(queueTrackIds(q)).toEqual([3, 1, 2])
  })
})
