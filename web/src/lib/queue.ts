/**
 * Playback queue logic, kept as pure functions so it can be tested without a
 * DOM or an audio element.
 *
 * The queue is a list of track ids plus a cursor. Shuffle is implemented as
 * a separate shuffled order rather than by mutating the visible queue, so
 * turning shuffle off restores the order the listener actually chose.
 */

export type RepeatMode = 'off' | 'all' | 'one'

export interface QueueState {
  /** The order the listener chose, always preserved. */
  order: number[]
  /** Indices into `order`, in playback sequence. Equals order when not shuffled. */
  sequence: number[]
  /** Position within `sequence`. */
  cursor: number
  shuffle: boolean
  repeat: RepeatMode
}

export const emptyQueue: QueueState = {
  order: [],
  sequence: [],
  cursor: 0,
  shuffle: false,
  repeat: 'off',
}

/** Fisher-Yates over indices, not ids, so duplicates in `order` are fine. */
export function shuffledIndices(length: number, random: () => number = Math.random): number[] {
  const indices = Array.from({ length }, (_, i) => i)
  for (let i = indices.length - 1; i > 0; i--) {
    const j = Math.floor(random() * (i + 1))
    ;[indices[i], indices[j]] = [indices[j], indices[i]]
  }
  return indices
}

/**
 * setQueue installs a new playback list. Starting on the track that was
 * already playing avoids an audible gap when a queue is replaced while audio
 * is running.
 */
export function setQueue(
  state: QueueState,
  order: number[],
  currentTrackId: number | null,
  shuffle = state.shuffle,
): QueueState {
  const keepIndex = currentTrackId === null ? -1 : order.indexOf(currentTrackId)

  const sequence = shuffle
    ? shuffledIndices(order.length)
    : order.map((_, i) => i)

  // With shuffle on, the current track's slot in the shuffled order is where
  // the cursor belongs, so playback does not jump.
  const cursor = shuffle
    ? Math.max(0, sequence.indexOf(keepIndex))
    : Math.max(0, keepIndex)

  // shuffle must be written back, not just used to build the sequence: a
  // caller rebuilding a queue from a synced snapshot passes the flag in and
  // expects the resulting state to report it.
  return { ...state, order, sequence, cursor, shuffle }
}

/** Appends tracks, skipping any already queued. */
export function addToQueue(state: QueueState, trackIds: number[]): QueueState {
  const existing = new Set(state.order)
  const fresh = trackIds.filter((id) => !existing.has(id))
  if (fresh.length === 0) return state

  const order = [...state.order, ...fresh]
  const addedFrom = state.order.length
  const newIndices = Array.from({ length: fresh.length }, (_, i) => addedFrom + i)
  const sequence = [...state.sequence, ...newIndices]

  return { ...state, order, sequence }
}

export function removeFromQueue(state: QueueState, index: number): QueueState {
  if (index < 0 || index >= state.order.length) return state

  const order = state.order.filter((_, i) => i !== index)
  // Rebuild the sequence against the new order, dropping the removed slot.
  const sequence = state.sequence.filter((i) => i !== index).map((i) => (i > index ? i - 1 : i))

  // If the removed item was at or before the cursor, step back to keep the
  // same track playing.
  const removedBeforeCursor = index < state.sequence[state.cursor]
  const cursor = Math.max(
    0,
    Math.min(sequence.length - 1, state.cursor - (removedBeforeCursor ? 1 : 0)),
  )

  return { ...state, order, sequence, cursor }
}

export function moveInQueue(state: QueueState, from: number, to: number): QueueState {
  if (from === to) return state
  if (from < 0 || from >= state.sequence.length) return state
  to = Math.max(0, Math.min(state.sequence.length - 1, to))

  // Reordering the sequence is enough: `order` is indexed by slot, and the
  // sequence is a permutation of every slot, so the array it points into
  // does not need rebuilding.
  const currentSlot = state.sequence[state.cursor]
  const sequence = [...state.sequence]

  const [moved] = sequence.splice(from, 1)
  sequence.splice(to, 0, moved)

  return { ...state, sequence, cursor: sequence.indexOf(currentSlot) }
}

export function currentTrackId(state: QueueState): number | null {
  const slot = state.sequence[state.cursor]
  if (slot === undefined) return null
  return state.order[slot] ?? null
}

/**
 * advance computes the next position. Returns null when playback should stop,
 * which happens at the end of a non-repeating, non-shuffled queue.
 */
export function advance(
  state: QueueState,
  direction: 1 | -1,
): { cursor: number; stop: boolean; restart: boolean } {
  if (state.sequence.length === 0) return { cursor: 0, stop: true, restart: false }

  // Repeat-one restarts the current track, except when the listener pressed
  // "previous", where restarting would be wrong.
  if (state.repeat === 'one' && direction === 1) {
    return { cursor: state.cursor, stop: false, restart: true }
  }

  const next = state.cursor + direction

  if (next < 0) {
    // Stepping back past the start: either loop round, or restart the queue.
    if (state.repeat === 'all' || state.shuffle) {
      return { cursor: state.sequence.length - 1, stop: false, restart: false }
    }
    return { cursor: 0, stop: false, restart: true }
  }

  if (next >= state.sequence.length) {
    if (state.repeat === 'all' || state.shuffle) {
      return { cursor: 0, stop: false, restart: false }
    }
    return { cursor: state.sequence.length - 1, stop: true, restart: false }
  }

  return { cursor: next, stop: false, restart: false }
}

export function toggleShuffle(state: QueueState, currentTrackId_: number | null): QueueState {
  const shuffle = !state.shuffle
  const order = state.order

  if (!shuffle) {
    // Leaving shuffle: put the cursor on the same track in natural order.
    const naturalIndex = currentTrackId_ === null ? -1 : order.indexOf(currentTrackId_)
    return {
      ...state,
      shuffle: false,
      sequence: order.map((_, i) => i),
      cursor: Math.max(0, naturalIndex),
    }
  }

  // Entering shuffle: reshuffle, but keep the current track first so playback
  // does not jump to something else the instant the toggle is flipped.
  const sequence = shuffledIndices(order.length)
  if (currentTrackId_ !== null) {
    const currentSlot = order.indexOf(currentTrackId_)
    if (currentSlot >= 0) {
      sequence.splice(sequence.indexOf(currentSlot), 1)
      sequence.unshift(currentSlot)
    }
  }
  return { ...state, shuffle: true, sequence, cursor: 0 }
}

export function cycleRepeat(state: QueueState): QueueState {
  const order: RepeatMode[] = ['off', 'all', 'one']
  const next = order[(order.indexOf(state.repeat) + 1) % order.length]
  return { ...state, repeat: next }
}

/** The queue as track ids in playback order, which is what syncs across devices. */
export function queueTrackIds(state: QueueState): number[] {
  return state.sequence.map((slot) => state.order[slot]).filter((id): id is number => id != null)
}

/** Rebuilds queue state from a synced snapshot received from another device. */
export function queueFromIds(ids: number[], currentTrackId_: number | null, shuffle: boolean): QueueState {
  return setQueue({ ...emptyQueue }, ids, currentTrackId_, shuffle)
}
