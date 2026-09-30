import { useEffect, useMemo, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { api, coverUrl, type Album, type Artist, type Track } from '../lib/api'
import { formatCount, formatLongDuration } from '../lib/format'
import { Cover, EmptyState, Panel, Skeleton } from '../components/ui'
import { TrackList } from '../components/TrackList'
import { AlbumCard } from './Home'
import { ShuffleIcon } from '../components/icons'
import type { PlayerSnapshot } from '../lib/player'

/** Shared handler bundle so every list page can wire the player the same way. */
export interface PlaybackProps {
  snapshot: PlayerSnapshot
  favoriteIds: Set<number>
  onPlayTracks: (tracks: Track[], startAt?: number) => void
  onToggleFavorite: (track: Track) => void
  onAddToPlaylist: (track: Track) => void
}

/**
 * The album grid. It only needs the shared PlaybackProps type, not the
 * values, so the props argument is left un-destructured on purpose.
 */
export function AlbumsPage(props: PlaybackProps) {
  const [albums, setAlbums] = useState<Album[]>([])
  const [loading, setLoading] = useState(true)
  const [sort, setSort] = useState<'recent' | 'title' | 'artist'>('recent')
  void props

  useEffect(() => {
    let cancelled = false
    void api
      .albums(500)
      .then((d) => {
        if (!cancelled) setAlbums(d.albums)
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [])

  // The server already sorts by year then artist; client-side sorting is only
  // needed for the alternative orders the user picks.
  const sorted = useMemo(() => {
    const copy = [...albums]
    switch (sort) {
      case 'title':
        return copy.sort((a, b) => a.title.localeCompare(b.title))
      case 'artist':
        return copy.sort((a, b) => `${a.artist}${a.title}`.localeCompare(`${b.artist}${b.title}`))
      default:
        return copy
    }
  }, [albums, sort])

  return (
    <div>
      <div className="animate-rise mb-6 flex flex-wrap items-end justify-between gap-4">
        <div>
          <p className="mb-1 text-xs font-semibold uppercase tracking-[0.18em] text-ink-400">Library</p>
          <h1 className="text-3xl font-bold tracking-tight text-ink-100 sm:text-4xl">Albums</h1>
        </div>
        <div className="flex gap-1.5">
          {(['recent', 'title', 'artist'] as const).map((option) => (
            <button
              key={option}
              type="button"
              onClick={() => setSort(option)}
              data-active={sort === option}
              className="chip capitalize"
            >
              {option}
            </button>
          ))}
        </div>
      </div>

      {loading ? (
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-6">
          {Array.from({ length: 18 }, (_, i) => (
            <Skeleton key={i} className="aspect-square" />
          ))}
        </div>
      ) : sorted.length === 0 ? (
        <EmptyState title="No albums yet" hint="Run a scan from settings to import your music." />
      ) : (
        <>
          <p className="mb-4 text-xs text-ink-600">{formatCount(sorted.length, 'album')}</p>
          <div className="stagger grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-6">
            {sorted.map((album, i) => (
              <AlbumCard key={album.id} album={album} index={i} />
            ))}
          </div>
        </>
      )}
    </div>
  )
}

export function AlbumPage(props: PlaybackProps) {
  const { id } = useParams<{ id: string }>()
  const albumId = Number(id)
  const [album, setAlbum] = useState<Album | null>(null)
  const [tracks, setTracks] = useState<Track[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    if (!Number.isFinite(albumId)) return
    let cancelled = false
    setLoading(true)

    void api
      .album(albumId)
      .then((d) => {
        if (cancelled) return
        setAlbum(d.album)
        setTracks(d.tracks)
      })
      .catch(() => undefined)
      .finally(() => {
        if (!cancelled) setLoading(false)
      })

    return () => {
      cancelled = true
    }
  }, [albumId])

  if (loading) {
    return (
      <div className="space-y-6">
        <div className="flex gap-6">
          <Skeleton className="h-48 w-48 shrink-0" />
          <div className="flex-1 space-y-3">
            <Skeleton className="h-4 w-24" />
            <Skeleton className="h-10 w-2/3" />
            <Skeleton className="h-4 w-40" />
          </div>
        </div>
        <div className="space-y-2">
          {Array.from({ length: 8 }, (_, i) => (
            <Skeleton key={i} className="h-14" />
          ))}
        </div>
      </div>
    )
  }

  if (!album) {
    return <EmptyState title="Album not found" hint="It may have been removed by a rescan." />
  }

  const playAll = () => props.onPlayTracks(tracks, 0)
  const shuffleAll = () => {
    const shuffled = [...tracks].sort(() => Math.random() - 0.5)
    props.onPlayTracks(shuffled, 0)
  }

  return (
    <div>
      {/* Hero */}
      <div className="animate-rise mb-8 flex flex-col gap-6 sm:flex-row sm:items-end">
        <div className="relative shrink-0 self-start sm:self-auto">
          <Cover
            src={coverUrl(album.coverPath)}
            alt={`${album.title} cover`}
            seed={album.id}
            className="h-44 w-44 shadow-[0_20px_60px_oklch(0_0_0/0.55)] sm:h-52 sm:w-52"
            rounded="rounded-[1.5rem]"
          />
        </div>

        <div className="min-w-0 flex-1">
          <p className="text-[11px] font-semibold uppercase tracking-[0.2em] text-ink-400">Album</p>
          <h1 className="mt-1.5 text-3xl font-bold leading-tight tracking-tight text-ink-100 sm:text-5xl">
            {album.title}
          </h1>
          <p className="mt-3 text-sm text-ink-300">
            <Link
              to={`/artist/${album.artistId}`}
              className="font-medium transition hover:text-ink-100"
            >
              {album.artist}
            </Link>
            {album.year && <span className="text-ink-600"> · {album.year}</span>}
          </p>
          <p className="mt-1 text-xs text-ink-500">
            {formatCount(album.trackCount, 'track')} · {formatLongDuration(album.durationMs)}
          </p>

          <div className="mt-5 flex flex-wrap items-center gap-2.5">
            <button
              type="button"
              onClick={playAll}
              className="pressable flex items-center gap-2 rounded-full bg-[var(--accent-live)]
                px-6 py-3 text-sm font-semibold text-ink-950 shadow-lg transition hover:brightness-110"
            >
              <svg viewBox="0 0 24 24" className="h-4 w-4 ml-0.5" fill="currentColor" aria-hidden="true">
                <path d="M7 4.5v15l13-7.5-13-7.5Z" />
              </svg>
              Play
            </button>
            <button
              type="button"
              onClick={shuffleAll}
              className="pressable flex items-center gap-2 rounded-full bg-white/10 px-5 py-3
                text-sm font-medium text-ink-100 transition hover:bg-white/16"
            >
              <ShuffleIcon className="h-4 w-4" />
              Shuffle
            </button>
          </div>
        </div>
      </div>

      <Panel className="p-2 sm:p-3">
        <TrackList
          tracks={tracks}
          onPlay={(i) => props.onPlayTracks(tracks, i)}
          onPlayAll={() => props.onPlayTracks(tracks, 0)}
          currentTrackId={props.snapshot.current?.id ?? null}
          isPlaying={props.snapshot.playing}
          favoriteIds={props.favoriteIds}
          onToggleFavorite={props.onToggleFavorite}
          onAddToPlaylist={props.onAddToPlaylist}
          showAlbum={false}
          showCover={false}
        />
      </Panel>
    </div>
  )
}

export function ArtistsPage(props: PlaybackProps) {
  // The artist list renders cards that link to ArtistPage, so it needs
  // nothing from the shared playback props.
  void props
  const [artists, setArtists] = useState<Artist[]>([])
  const [loading, setLoading] = useState(true)
  const [filter, setFilter] = useState('')

  useEffect(() => {
    let cancelled = false
    void api
      .artists(500)
      .then((d) => {
        if (!cancelled) setArtists(d.artists)
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [])

  const filtered = filter
    ? artists.filter((a) => a.name.toLowerCase().includes(filter.toLowerCase()))
    : artists

  return (
    <div>
      <div className="animate-rise mb-6 flex flex-wrap items-end justify-between gap-4">
        <div>
          <p className="mb-1 text-xs font-semibold uppercase tracking-[0.18em] text-ink-400">Library</p>
          <h1 className="text-3xl font-bold tracking-tight text-ink-100 sm:text-4xl">Artists</h1>
        </div>
        <input
          type="search"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          placeholder="Filter artists"
          className="w-full max-w-xs rounded-full border border-white/10 bg-white/5 px-4 py-2
            text-sm text-ink-100 outline-none transition
            placeholder:text-ink-600 focus:border-[var(--accent-live)] focus:bg-white/8 sm:w-64"
        />
      </div>

      {loading ? (
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {Array.from({ length: 12 }, (_, i) => (
            <Skeleton key={i} className="h-20" />
          ))}
        </div>
      ) : filtered.length === 0 ? (
        <EmptyState title="No artists found" hint="Try a different filter, or run a scan." />
      ) : (
        <>
          <p className="mb-4 text-xs text-ink-600">{formatCount(filtered.length, 'artist')}</p>
          <div className="stagger grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {filtered.map((artist, i) => (
              <Link
                key={artist.id}
                to={`/artist/${artist.id}`}
                className="pressable glass flex items-center gap-4 p-4"
                style={{ animationDelay: `${Math.min(i, 12) * 30}ms` }}
              >
                <div
                  className="grid h-14 w-14 shrink-0 place-items-center rounded-full text-xl
                    font-bold text-ink-100"
                  style={{
                    background: `linear-gradient(135deg, oklch(0.5 0.14 ${(i * 47) % 360}), oklch(0.3 0.1 ${(i * 47 + 60) % 360}))`,
                  }}
                >
                  {artist.name.charAt(0).toUpperCase()}
                </div>
                <div className="min-w-0">
                  <p className="truncate text-sm font-semibold text-ink-100">{artist.name}</p>
                  <p className="text-xs text-ink-400">
                    {formatCount(artist.albumCount, 'album')} · {formatCount(artist.trackCount, 'track')}
                  </p>
                </div>
              </Link>
            ))}
          </div>
        </>
      )}
    </div>
  )
}

export function ArtistPage(props: PlaybackProps) {
  const { id } = useParams<{ id: string }>()
  const artistId = Number(id)
  const [artist, setArtist] = useState<Artist | null>(null)
  const [albums, setAlbums] = useState<Album[]>([])
  const [tracks, setTracks] = useState<Track[]>([])
  const [tab, setTab] = useState<'albums' | 'tracks'>('albums')
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    if (!Number.isFinite(artistId)) return
    let cancelled = false
    setLoading(true)

    void api
      .artist(artistId)
      .then((d) => {
        if (cancelled) return
        setArtist(d.artist)
        setAlbums(d.albums)
        setTracks(d.tracks)
      })
      .catch(() => undefined)
      .finally(() => {
        if (!cancelled) setLoading(false)
      })

    return () => {
      cancelled = true
    }
  }, [artistId])

  if (loading) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-24 w-64" />
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-4">
          {Array.from({ length: 8 }, (_, i) => (
            <Skeleton key={i} className="aspect-square" />
          ))}
        </div>
      </div>
    )
  }

  if (!artist) {
    return <EmptyState title="Artist not found" />
  }

  const totalMs = tracks.reduce((sum, t) => sum + t.durationMs, 0)

  return (
    <div>
      <div className="animate-rise mb-6 flex flex-col items-start gap-5 sm:flex-row sm:items-center">
        <div
          className="grid h-28 w-28 shrink-0 place-items-center rounded-full text-4xl font-bold text-ink-100"
          style={{
            background: 'linear-gradient(135deg, oklch(0.5 0.14 305), oklch(0.3 0.1 275))',
          }}
        >
          {artist.name.charAt(0).toUpperCase()}
        </div>
        <div>
          <p className="text-[11px] font-semibold uppercase tracking-[0.2em] text-ink-400">Artist</p>
          <h1 className="mt-1 text-3xl font-bold tracking-tight text-ink-100 sm:text-5xl">
            {artist.name}
          </h1>
          <p className="mt-2 text-xs text-ink-500">
            {formatCount(albums.length, 'album')} · {formatCount(tracks.length, 'track')} ·{' '}
            {formatLongDuration(totalMs)}
          </p>
          <div className="mt-4 flex gap-2.5">
            <button
              type="button"
              onClick={() => props.onPlayTracks(tracks, 0)}
              className="pressable flex items-center gap-2 rounded-full bg-[var(--accent-live)] px-6
                py-2.5 text-sm font-semibold text-ink-950 transition hover:brightness-110"
            >
              <svg viewBox="0 0 24 24" className="h-4 w-4 ml-0.5" fill="currentColor" aria-hidden="true">
                <path d="M7 4.5v15l13-7.5-13-7.5Z" />
              </svg>
              Play
            </button>
            <button
              type="button"
              onClick={() => props.onPlayTracks([...tracks].sort(() => Math.random() - 0.5), 0)}
              className="pressable flex items-center gap-2 rounded-full bg-white/10 px-5 py-2.5
                text-sm font-medium text-ink-100 transition hover:bg-white/16"
            >
              <ShuffleIcon className="h-4 w-4" />
              Shuffle
            </button>
          </div>
        </div>
      </div>

      <div className="mb-5 flex gap-1.5">
        <button type="button" data-active={tab === 'albums'} onClick={() => setTab('albums')} className="chip">
          Albums
        </button>
        <button type="button" data-active={tab === 'tracks'} onClick={() => setTab('tracks')} className="chip">
          Tracks
        </button>
      </div>

      {tab === 'albums' ? (
        <div className="stagger grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5">
          {albums.map((album, i) => (
            <AlbumCard key={album.id} album={album} index={i} />
          ))}
        </div>
      ) : (
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
      )}
    </div>
  )
}
