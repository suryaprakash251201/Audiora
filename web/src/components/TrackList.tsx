import { useState } from 'react'
import type { Track } from '../lib/api'
import { coverUrl } from '../lib/api'
import { formatDuration } from '../lib/format'
import { Cover, PlayingIndicator } from './ui'
import { HeartIcon, MoreIcon, MusicNoteIcon, ShuffleIcon, PlayIcon } from './icons'

/**
 * A track table.
 *
 * On a phone the table becomes a stacked list: a five-column table at 375px
 * is unreadable, so the metadata collapses under the title and the duration
 * moves to the right.
 */
export function TrackList({
  tracks,
  onPlay,
  onPlayAll,
  currentTrackId,
  isPlaying,
  favoriteIds,
  onToggleFavorite,
  onAddToPlaylist,
  onRemove,
  showAlbum = true,
  showCover = true,
  emptyMessage,
}: {
  tracks: Track[]
  onPlay: (index: number) => void
  onPlayAll?: () => void
  currentTrackId?: number | null
  isPlaying?: boolean
  favoriteIds?: Set<number>
  onToggleFavorite?: (track: Track) => void
  onAddToPlaylist?: (track: Track) => void
  onRemove?: (track: Track, index: number) => void
  showAlbum?: boolean
  showCover?: boolean
  emptyMessage?: string
}) {
  const [menuFor, setMenuFor] = useState<number | null>(null)

  if (tracks.length === 0) {
    return <p className="px-4 py-10 text-center text-sm text-ink-400">{emptyMessage ?? 'Nothing here yet.'}</p>
  }

  return (
    <div className="relative">
      {/* Header, desktop only. */}
      <div
        className="hidden grid-cols-[2.5rem_1fr_1fr_5rem_2.5rem] items-center gap-3
          border-b border-white/8 px-3 pb-2 text-[11px] font-semibold uppercase
          tracking-wider text-ink-600 md:grid"
      >
        <span className="text-right">#</span>
        <span>Title</span>
        {showAlbum && <span>Album</span>}
        <span className="text-right">
          <span className="md:hidden">Time</span>
        </span>
        <span />
      </div>

      <ul className="mt-1">
        {tracks.map((track, index) => {
          const isCurrent = currentTrackId === track.id
          const isFav = favoriteIds?.has(track.id) ?? false

          return (
            <li
              key={`${track.id}-${index}`}
              className="group relative grid grid-cols-[2.5rem_1fr_auto] items-center gap-3
                rounded-xl px-3 py-2 transition-colors md:grid-cols-[2.5rem_1fr_1fr_5rem_2.5rem]
                hover:bg-white/6"
            >
              {/* Index / play button / equaliser */}
              <div className="flex justify-end">
                <button
                  type="button"
                  onClick={() => onPlay(index)}
                  aria-label={`Play ${track.title}`}
                  className="pressable grid h-8 w-8 place-items-center"
                >
                  {isCurrent && isPlaying ? (
                    <PlayingIndicator />
                  ) : (
                    <>
                      <span className="text-xs tabular-nums text-ink-600 group-hover:hidden">
                        {track.trackNo ?? index + 1}
                      </span>
                      <PlayIcon className="hidden h-4 w-4 text-ink-100 group-hover:block" />
                    </>
                  )}
                </button>
              </div>

              {/* Title and artist */}
              <div className="flex min-w-0 items-center gap-3">
                {showCover && (
                  <Cover
                    src={coverUrl(track.coverPath)}
                    alt=""
                    seed={track.albumId}
                    className="h-10 w-10 shrink-0"
                    rounded="rounded-lg"
                  />
                )}
                <div className="min-w-0">
                  <p
                    className={`truncate text-sm font-medium ${
                      isCurrent ? 'text-[var(--accent-live)]' : 'text-ink-100'
                    }`}
                  >
                    {track.title}
                    {track.hasLyrics && (
                      <span className="ml-1.5 text-[10px] text-ink-600" title="Has lyrics">
                        ♪
                      </span>
                    )}
                  </p>
                  <p className="truncate text-xs text-ink-400">
                    {track.artist}
                    {showAlbum && <span className="hidden md:contents"> · {track.album}</span>}
                  </p>
                </div>
              </div>

              {/* Album, desktop only. */}
              {showAlbum && (
                <p className="hidden truncate text-xs text-ink-400 md:block">{track.album}</p>
              )}

              <span className="text-right text-xs tabular-nums text-ink-600">
                {formatDuration(track.durationMs)}
              </span>

              {/* Row actions */}
              <div className="flex items-center justify-end gap-0.5">
                {onToggleFavorite && (
                  <button
                    type="button"
                    onClick={() => onToggleFavorite(track)}
                    aria-label={isFav ? 'Remove from favourites' : 'Add to favourites'}
                    className={`pressable grid h-8 w-8 place-items-center rounded-full transition
                      ${isFav ? 'text-[var(--accent-live)]' : 'text-ink-600 opacity-0 group-hover:opacity-100 focus:opacity-100'}`}
                  >
                    <HeartIcon className="h-4 w-4" filled={isFav} />
                  </button>
                )}
                <button
                  type="button"
                  onClick={() => setMenuFor(menuFor === index ? null : index)}
                  aria-label="More actions"
                  aria-expanded={menuFor === index}
                  className="pressable grid h-8 w-8 place-items-center rounded-full
                    text-ink-600 opacity-0 transition hover:text-ink-100
                    group-hover:opacity-100 focus:opacity-100"
                >
                  <MoreIcon className="h-4 w-4" />
                </button>
              </div>

              {menuFor === index && (
                <>
                  {/* Click-catcher so a click elsewhere closes the menu. */}
                  <button
                    type="button"
                    aria-label="Close menu"
                    className="fixed inset-0 z-10 cursor-default"
                    onClick={() => setMenuFor(null)}
                  />
                  <div
                    className="glass-bright absolute right-3 top-full z-20 mt-1 w-52
                      overflow-hidden rounded-2xl p-1.5 shadow-2xl"
                  >
                    <button
                      type="button"
                      onClick={() => {
                        onPlay(index)
                        setMenuFor(null)
                      }}
                      className="flex w-full items-center gap-2.5 rounded-xl px-3 py-2 text-sm
                        text-ink-200 transition hover:bg-white/10"
                    >
                      <PlayIcon className="h-4 w-4" />
                      Play now
                    </button>
                    {onPlayAll && (
                      <button
                        type="button"
                        onClick={() => {
                          onPlayAll()
                          setMenuFor(null)
                        }}
                        className="flex w-full items-center gap-2.5 rounded-xl px-3 py-2 text-sm
                          text-ink-200 transition hover:bg-white/10"
                      >
                        <ShuffleIcon className="h-4 w-4" />
                        Play next
                      </button>
                    )}
                    {onAddToPlaylist && (
                      <button
                        type="button"
                        onClick={() => {
                          onAddToPlaylist(track)
                          setMenuFor(null)
                        }}
                        className="flex w-full items-center gap-2.5 rounded-xl px-3 py-2 text-sm
                          text-ink-200 transition hover:bg-white/10"
                      >
                        <MusicNoteIcon className="h-4 w-4" />
                        Add to playlist
                      </button>
                    )}
                    {onToggleFavorite && (
                      <button
                        type="button"
                        onClick={() => {
                          onToggleFavorite(track)
                          setMenuFor(null)
                        }}
                        className="flex w-full items-center gap-2.5 rounded-xl px-3 py-2 text-sm
                          text-ink-200 transition hover:bg-white/10"
                      >
                        <HeartIcon className="h-4 w-4" filled={isFav} />
                        {isFav ? 'Remove from favourites' : 'Add to favourites'}
                      </button>
                    )}
                    {onRemove && (
                      <button
                        type="button"
                        onClick={() => {
                          onRemove(track, index)
                          setMenuFor(null)
                        }}
                        className="flex w-full items-center gap-2.5 rounded-xl px-3 py-2 text-sm
                          text-red-300 transition hover:bg-red-500/15"
                      >
                        Remove
                      </button>
                    )}
                  </div>
                </>
              )}
            </li>
          )
        })}
      </ul>
    </div>
  )
}
