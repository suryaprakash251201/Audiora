import { useEffect, useRef, useState } from 'react'
import { api, type Track } from '../lib/api'
import { formatCount } from '../lib/format'
import { EmptyState, Panel, Skeleton } from '../components/ui'
import { TrackList } from '../components/TrackList'
import { SearchIcon, CloseIcon, ShuffleIcon } from '../components/icons'
import type { PlaybackProps } from './Library'

/**
 * Search.
 *
 * Queries are debounced and every keystroke cancels the previous in-flight
 * request, so a fast typist does not get results for "bach" arriving after
 * "beethoven" and overwriting the correct answer.
 */
export function SearchPage(props: PlaybackProps) {
  const [query, setQuery] = useState('')
  const [results, setResults] = useState<Track[]>([])
  const [searching, setSearching] = useState(false)
  const inputRef = useRef<HTMLInputElement>(null)
  const abortRef = useRef<AbortController | null>(null)

  useEffect(() => {
    inputRef.current?.focus()
  }, [])

  useEffect(() => {
    const trimmed = query.trim()
    if (trimmed.length === 0) {
      setResults([])
      setSearching(false)
      return
    }

    setSearching(true)
    const timer = window.setTimeout(() => {
      abortRef.current?.abort()
      const controller = new AbortController()
      abortRef.current = controller

      void api
        .search(trimmed, controller.signal)
        .then((d) => {
          setResults(d.results)
        })
        .catch(() => {
          // An aborted request is the expected outcome of a fast typist.
        })
        .finally(() => {
          if (!abortRef.current?.signal.aborted) setSearching(false)
        })
    }, 220)

    return () => clearTimeout(timer)
  }, [query])

  return (
    <div>
      <div className="animate-rise mb-6">
        <p className="mb-1 text-xs font-semibold uppercase tracking-[0.18em] text-ink-400">Find</p>
        <h1 className="mb-5 text-3xl font-bold tracking-tight text-ink-100 sm:text-4xl">Search</h1>

        <div className="glass flex items-center gap-3 rounded-full px-5 py-3.5">
          <SearchIcon className="h-5 w-5 shrink-0 text-ink-400" />
          <input
            ref={inputRef}
            type="search"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Songs, albums or artists"
            className="min-w-0 flex-1 bg-transparent text-base text-ink-100 outline-none
              placeholder:text-ink-600"
            aria-label="Search your library"
          />
          {query && (
            <button
              type="button"
              onClick={() => {
                setQuery('')
                inputRef.current?.focus()
              }}
              aria-label="Clear search"
              className="pressable grid h-7 w-7 shrink-0 place-items-center rounded-full
                text-ink-400 transition hover:bg-white/10 hover:text-ink-100"
            >
              <CloseIcon className="h-4 w-4" />
            </button>
          )}
        </div>
      </div>

      {query.trim() === '' ? (
        <EmptyState
          title="Search your library"
          hint="Type a song title, an album, or an artist. Everything is matched against your own files."
        />
      ) : searching && results.length === 0 ? (
        <div className="space-y-2">
          {Array.from({ length: 6 }, (_, i) => (
            <Skeleton key={i} className="h-14" />
          ))}
        </div>
      ) : results.length === 0 ? (
        <EmptyState title={`Nothing found for "${query}"`} hint="Try a shorter or different spelling." />
      ) : (
        <>
          <div className="mb-3 flex items-center justify-between px-1">
            <p className="text-xs text-ink-600">{formatCount(results.length, 'result')}</p>
            <button
              type="button"
              onClick={() =>
                props.onPlayTracks([...results].sort(() => Math.random() - 0.5), 0)
              }
              className="pressable flex items-center gap-1.5 rounded-full bg-white/8 px-3 py-1.5
                text-xs font-medium text-ink-200 transition hover:bg-white/14"
            >
              <ShuffleIcon className="h-3.5 w-3.5" />
              Play all
            </button>
          </div>

          <Panel className="p-2 sm:p-3">
            <TrackList
              tracks={results}
              onPlay={(i) => props.onPlayTracks(results, i)}
              currentTrackId={props.snapshot.current?.id ?? null}
              isPlaying={props.snapshot.playing}
              favoriteIds={props.favoriteIds}
              onToggleFavorite={props.onToggleFavorite}
              onAddToPlaylist={props.onAddToPlaylist}
            />
          </Panel>
        </>
      )}
    </div>
  )
}

export function FavoritesPage(props: PlaybackProps) {
  const [tracks, setTracks] = useState<Track[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    let cancelled = false
    void api
      .favorites()
      .then((d) => {
        if (!cancelled) setTracks(d.tracks)
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [])

  if (loading) {
    return (
      <div className="space-y-2">
        {Array.from({ length: 5 }, (_, i) => (
          <Skeleton key={i} className="h-14" />
        ))}
      </div>
    )
  }

  return (
    <div>
      <div className="animate-rise mb-6">
        <p className="mb-1 text-xs font-semibold uppercase tracking-[0.18em] text-ink-400">Your collection</p>
        <h1 className="text-3xl font-bold tracking-tight text-ink-100 sm:text-4xl">Favourites</h1>
      </div>

      {tracks.length === 0 ? (
        <EmptyState
          title="No favourites yet"
          hint="Tap the heart next to any track to keep it here."
        />
      ) : (
        <>
          <div className="mb-3 flex items-center justify-between px-1">
            <p className="text-xs text-ink-600">{formatCount(tracks.length, 'track')}</p>
            <button
              type="button"
              onClick={() => props.onPlayTracks([...tracks].sort(() => Math.random() - 0.5), 0)}
              className="pressable flex items-center gap-1.5 rounded-full bg-white/8 px-3 py-1.5
                text-xs font-medium text-ink-200 transition hover:bg-white/14"
            >
              <ShuffleIcon className="h-3.5 w-3.5" />
              Play all
            </button>
          </div>
          <Panel className="p-2 sm:p-3">
            <TrackList
              tracks={tracks}
              onPlay={(i) => props.onPlayTracks(tracks, i)}
              currentTrackId={props.snapshot.current?.id ?? null}
              isPlaying={props.snapshot.playing}
              favoriteIds={props.favoriteIds}
              onToggleFavorite={props.onToggleFavorite}
              onAddToPlaylist={props.onAddToPlaylist}
            />
          </Panel>
        </>
      )}
    </div>
  )
}
