import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, coverUrl, type Album, type Track, type LibraryStats } from '../lib/api'
import { formatBytes, formatCount, formatDuration, formatLongDuration } from '../lib/format'
import { Cover, EmptyState, PageHeader, Panel, Skeleton } from '../components/ui'
import { ShuffleIcon, PlayIcon } from '../components/icons'

interface Suggestions {
  recentlyAdded: Track[]
  topArtists: { name: string; plays: number }[]
}

export function HomePage({ onPlayTracks }: { onPlayTracks: (tracks: Track[], startAt?: number) => void }) {
  const [stats, setStats] = useState<LibraryStats | null>(null)
  const [albums, setAlbums] = useState<Album[]>([])
  const [suggestions, setSuggestions] = useState<Suggestions | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false

    async function load() {
      try {
        // Requests run concurrently; the library page is the first paint, so
        // waiting for all of them would make it feel slow.
        const [s, a, g] = await Promise.all([
          api.stats(),
          api.albums(24),
          api.suggestions(),
        ])
        if (cancelled) return
        setStats(s)
        setAlbums(a.albums)
        setSuggestions(g)
      } catch (err) {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : 'Could not load your library')
        }
      } finally {
        if (!cancelled) setLoading(false)
      }
    }

    void load()
    return () => {
      cancelled = true
    }
  }, [])

  // The greeting is a small thing, but it makes the app feel like a person
  // opened it rather than a page that loaded.
  const hour = new Date().getHours()
  const greeting = hour < 5 ? 'Late night' : hour < 12 ? 'Good morning' : hour < 18 ? 'Good afternoon' : 'Good evening'

  if (error) {
    return (
      <EmptyState
        title="Could not reach your library"
        hint={error}
        action={
          <button
            type="button"
            onClick={() => location.reload()}
            className="pressable mt-2 rounded-full bg-white/10 px-4 py-2 text-sm text-ink-200"
          >
            Try again
          </button>
        }
      />
    )
  }

  return (
    <div className="space-y-10">
      <PageHeader eyebrow={greeting} title="Listen Now" />

      {loading ? (
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-6">
          {Array.from({ length: 12 }, (_, i) => (
            <Skeleton key={i} className="aspect-square" />
          ))}
        </div>
      ) : (
        albums.length > 0 && (
          <section>
            <div className="mb-4 flex items-center justify-between">
              <h2 className="text-lg font-semibold tracking-tight text-ink-100">Recently added</h2>
              <Link
                to="/albums"
                className="text-xs font-medium text-ink-400 transition hover:text-ink-100"
              >
                See all
              </Link>
            </div>

            <div className="stagger grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-6">
              {albums.slice(0, 12).map((album, index) => (
                <AlbumCard key={album.id} album={album} index={index} />
              ))}
            </div>
          </section>
        )
      )}

      {loading && (
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
          {Array.from({ length: 4 }, (_, i) => (
            <Skeleton key={i} className="h-24" />
          ))}
        </div>
      )}

      {!loading && suggestions && suggestions.recentlyAdded.length > 0 && (
        <section>
          <h2 className="mb-4 text-lg font-semibold tracking-tight text-ink-100">Jump back in</h2>
          <Panel className="overflow-hidden p-1.5">
            {suggestions.recentlyAdded.slice(0, 6).map((track) => (
              <button
                key={track.id}
                type="button"
                onClick={() => onPlayTracks([track], 0)}
                className="pressable flex w-full items-center gap-3 rounded-2xl px-3 py-2.5 text-left
                  transition hover:bg-white/6"
              >
                <Cover
                  src={coverUrl(track.coverPath)}
                  alt=""
                  seed={track.albumId}
                  className="h-11 w-11 shrink-0"
                  rounded="rounded-lg"
                />
                <div className="min-w-0 flex-1">
                  <p className="truncate text-sm font-medium text-ink-100">{track.title}</p>
                  <p className="truncate text-xs text-ink-400">{track.artist}</p>
                </div>
                <span className="shrink-0 text-xs tabular-nums text-ink-600">
                  {formatDuration(track.durationMs)}
                </span>
              </button>
            ))}
          </Panel>
        </section>
      )}

      {!loading && suggestions && suggestions.topArtists.length > 0 && (
        <section>
          <h2 className="mb-4 text-lg font-semibold tracking-tight text-ink-100">Most played</h2>
          <div className="stagger grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {suggestions.topArtists.slice(0, 6).map((artist) => (
              <Panel
                key={artist.name}
                className="pressable flex items-center gap-3 p-3"
              >
                <div
                  className="grid h-11 w-11 shrink-0 place-items-center rounded-full text-sm font-bold text-ink-100"
                  style={{ background: 'linear-gradient(135deg, oklch(0.5 0.14 305), oklch(0.35 0.1 275))' }}
                >
                  {artist.name.charAt(0).toUpperCase()}
                </div>
                <div className="min-w-0">
                  <p className="truncate text-sm font-medium text-ink-100">{artist.name}</p>
                  <p className="text-xs text-ink-400">
                    {formatCount(artist.plays, 'play')}
                  </p>
                </div>
              </Panel>
            ))}
          </div>
        </section>
      )}

      {!loading && stats && (
        <Panel className="p-5">
          <h2 className="mb-4 text-sm font-semibold uppercase tracking-wider text-ink-400">
            Your library
          </h2>
          <dl className="grid grid-cols-2 gap-5 sm:grid-cols-5">
            <Stat label="Tracks" value={formatCount(stats.tracks, 'track')} />
            <Stat label="Albums" value={formatCount(stats.albums, 'album')} />
            <Stat label="Artists" value={formatCount(stats.artists, 'artist')} />
            <Stat label="Runtime" value={formatLongDuration(stats.durationMs)} />
            <Stat label="On disk" value={formatBytes(stats.sizeBytes)} />
          </dl>
        </Panel>
      )}

      {!loading && albums.length === 0 && (
        <EmptyState
          title="Your library is empty"
          hint="Point Audiora at a folder of music and run a scan from the admin settings. Supported formats are FLAC, MP3, WAV, M4A, AAC, OGG and Opus."
        />
      )}
    </div>
  )
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <dt className="text-[11px] font-medium uppercase tracking-wider text-ink-600">{label}</dt>
      <dd className="mt-0.5 truncate text-lg font-semibold text-ink-100">{value}</dd>
    </div>
  )
}

export function AlbumCard({ album, index = 0 }: { album: Album; index?: number }) {
  return (
    <Link
      to={`/album/${album.id}`}
      className="pressable group block"
      style={{ animationDelay: `${Math.min(index, 12) * 35}ms` }}
    >
      <div className="relative">
        <Cover
          src={coverUrl(album.coverPath)}
          alt={`${album.title} by ${album.artist}`}
          seed={album.id}
          className="aspect-square w-full shadow-[0_8px_28px_oklch(0_0_0/0.4)]"
          rounded="rounded-2xl"
        />
        <div
          className="pointer-events-none absolute bottom-2 right-2 grid h-9 w-9 place-items-center
            rounded-full bg-[var(--accent-live)] text-ink-950 opacity-0 shadow-xl
            transition-opacity duration-200 group-hover:opacity-100 max-md:opacity-100"
          aria-hidden="true"
        >
          <PlayIcon className="h-4 w-4 ml-0.5" />
        </div>
      </div>
      <p className="mt-2.5 truncate text-sm font-semibold text-ink-100">{album.title}</p>
      <p className="truncate text-xs text-ink-400">{album.artist}</p>
    </Link>
  )
}

export { ShuffleIcon }
