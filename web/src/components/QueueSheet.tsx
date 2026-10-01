import { useEffect } from 'react'
import { coverUrl, type Track } from '../lib/api'
import { formatDuration } from '../lib/format'
import { Cover, IconButton } from './ui'
import { CloseIcon, TrashIcon } from './icons'
import type { PlayerSnapshot } from '../lib/player'
import type { QueueState } from '../lib/queue'

/** The "up next" queue, with drag-free reordering via the up/down affordance. */
export function QueueSheet({
  snapshot,
  onClose,
  onPlayAt,
  onRemoveAt,
  onMove,
  onClear,
  onSaveAsPlaylist,
}: {
  snapshot: PlayerSnapshot
  onClose: () => void
  onPlayAt: (index: number) => void
  onRemoveAt: (index: number) => void
  onMove: (from: number, to: number) => void
  onClear: () => void
  onSaveAsPlaylist: () => void
}) {
  const queue = snapshot.queue

  useEffect(() => {
    function onKey(event: KeyboardEvent) {
      if (event.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  // Map each queue position to its track id, keeping the natural order for
  // the "play next" list the listener actually sees.
  const entries: { trackId: number; position: number }[] = queue.sequence.map(
    (slot, position) => ({ trackId: queue.order[slot], position }),
  )
  const currentPosition = queue.cursor

  // Everything after the current position, which is what "remaining" means.
  const remainingMs = entries
    .slice(currentPosition + 1)
    .reduce((sum, entry) => sum + (trackCache.get(entry.trackId)?.durationMs ?? 0), 0)

  return (
    <div className="animate-fade fixed inset-0 z-50 flex justify-end">
      <button
        type="button"
        aria-label="Close queue"
        className="absolute inset-0 bg-black/50 backdrop-blur-sm"
        onClick={onClose}
      />
      <aside
        className="glass-solid animate-rise relative z-10 flex h-full w-full max-w-sm flex-col
          rounded-none border-y-0 border-r-0"
        role="dialog"
        aria-label="Play queue"
      >
        <header className="flex shrink-0 items-center justify-between px-5 pb-3 pt-5">
          <div>
            <h2 className="text-lg font-bold tracking-tight text-ink-100">Up next</h2>
            <p className="text-xs text-ink-500">
              {entries.length} track{entries.length === 1 ? '' : 's'}
              {remainingMs > 0 && ' — ' + formatDuration(remainingMs) + ' remaining'}
            </p>
          </div>
          <IconButton label="Close" onClick={onClose}>
            <CloseIcon />
          </IconButton>
        </header>

        <div className="flex shrink-0 gap-2 px-5 pb-3">
          {entries.length > 0 && (
            <button
              type="button"
              onClick={onSaveAsPlaylist}
              className="pressable flex-1 rounded-full bg-white/8 py-2 text-xs font-medium
                text-ink-200 transition hover:bg-white/14"
            >
              Save as playlist
            </button>
          )}
          {entries.length > 0 && (
            <button
              type="button"
              onClick={onClear}
              className="pressable rounded-full border border-white/10 px-4 py-2 text-xs
                text-ink-400 transition hover:bg-white/8"
            >
              Clear
            </button>
          )}
        </div>

        <ul className="scroll-pane min-h-0 flex-1 px-3 pb-6">
          {entries.length === 0 && (
            <p className="px-2 py-10 text-center text-sm text-ink-400">
              Nothing queued. Play an album to fill this up.
            </p>
          )}

          {entries.map((entry, index) => {
            const track = trackCache.get(entry.trackId)
            const isCurrent = index === currentPosition
            const isPast = index < currentPosition

            return (
              <li
                key={`${entry.trackId}-${index}`}
                className={`group flex items-center gap-3 rounded-2xl p-2 transition
                  ${isCurrent ? 'bg-white/10' : 'hover:bg-white/5'}
                  ${isPast ? 'opacity-45' : ''}`}
              >
                <button
                  type="button"
                  onClick={() => onPlayAt(index)}
                  className="flex min-w-0 flex-1 items-center gap-3 text-left"
                >
                  {track && (
                    <Cover
                      src={coverUrl(track.coverPath)}
                      alt=""
                      seed={track.albumId}
                      className="h-10 w-10 shrink-0"
                      rounded="rounded-lg"
                    />
                  )}
                  <div className="min-w-0 flex-1">
                    <p
                      className={`truncate text-sm font-medium ${
                        isCurrent ? 'text-[var(--accent-live)]' : 'text-ink-100'
                      }`}
                    >
                      {track?.title ?? `Track ${entry.trackId}`}
                    </p>
                    <p className="truncate text-xs text-ink-500">{track?.artist ?? ''}</p>
                  </div>
                  {track && (
                    <span className="shrink-0 text-xs tabular-nums text-ink-600">
                      {formatDuration(track.durationMs)}
                    </span>
                  )}
                </button>

                <div
                  className="flex shrink-0 items-center gap-0.5 opacity-0 transition group-hover:opacity-100"
                >
                  <button
                    type="button"
                    onClick={() => onMove(index, index - 1)}
                    disabled={index === 0}
                    aria-label="Move up"
                    className="pressable grid h-7 w-7 place-items-center rounded-full text-ink-500
                      transition hover:bg-white/10 hover:text-ink-200 disabled:opacity-30"
                  >
                    <svg viewBox="0 0 24 24" className="h-3.5 w-3.5" fill="none" stroke="currentColor" strokeWidth="2.2">
                      <path d="m6 15 6-6 6 6" strokeLinecap="round" strokeLinejoin="round" />
                    </svg>
                  </button>
                  <button
                    type="button"
                    onClick={() => onMove(index, index + 1)}
                    disabled={index === entries.length - 1}
                    aria-label="Move down"
                    className="pressable grid h-7 w-7 place-items-center rounded-full text-ink-500
                      transition hover:bg-white/10 hover:text-ink-200 disabled:opacity-30"
                  >
                    <svg viewBox="0 0 24 24" className="h-3.5 w-3.5" fill="none" stroke="currentColor" strokeWidth="2.2">
                      <path d="m6 9 6 6 6-6" strokeLinecap="round" strokeLinejoin="round" />
                    </svg>
                  </button>
                  <button
                    type="button"
                    onClick={() => onRemoveAt(index)}
                    aria-label="Remove from queue"
                    className="pressable grid h-7 w-7 place-items-center rounded-full text-ink-500
                      transition hover:bg-red-500/15 hover:text-red-300"
                  >
                    <TrashIcon className="h-3.5 w-3.5" />
                  </button>
                </div>
              </li>
            )
          })}
        </ul>
      </aside>
    </div>
  )
}

/**
 * A module-level track lookup.
 *
 * The queue holds ids, not track objects, so the sheet needs metadata for
 * rows it did not itself load. The player keeps every track it has seen, and
 * this mirrors that without threading another prop through the tree.
 */
const trackCache = new Map<number, Track>()

export function registerTracks(tracks: Track[]) {
  for (const t of tracks) trackCache.set(t.id, t)
}

export function trackFromCache(id: number): Track | undefined {
  return trackCache.get(id)
}

export type { QueueState, PlayerSnapshot }
