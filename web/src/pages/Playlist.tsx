import { useEffect, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { api, coverUrl, type Playlist, type Track } from '../lib/api'
import { formatCount, formatLongDuration } from '../lib/format'
import { Cover, EmptyState, Panel, Skeleton } from '../components/ui'
import { TrackList } from '../components/TrackList'
import { MoreIcon, ShuffleIcon, TrashIcon } from '../components/icons'
import type { PlaybackProps } from './Library'

export function PlaylistPage(props: PlaybackProps & { onChanged: () => void }) {
  const { id } = useParams<{ id: string }>()
  const playlistId = Number(id)
  const navigate = useNavigate()
  const [playlist, setPlaylist] = useState<Playlist | null>(null)
  const [tracks, setTracks] = useState<Track[]>([])
  const [loading, setLoading] = useState(true)
  const [menuOpen, setMenuOpen] = useState(false)

  async function reload() {
    if (!Number.isFinite(playlistId)) return
    try {
      const d = await api.playlist(playlistId)
      setPlaylist(d.playlist)
      setTracks(d.tracks)
    } catch {
      navigate('/')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    setLoading(true)
    void reload()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [playlistId])

  async function removeTrack(track: Track, index: number) {
    if (!playlist) return
    // Optimistic: the row disappears immediately, and a failed request is
    // rolled back by reloading rather than by showing a toast.
    setTracks((prev) => prev.filter((_, i) => i !== index))
    try {
      await api.removeFromPlaylist(playlist.id, track.id)
      props.onChanged()
    } catch {
      void reload()
    }
  }

  async function deletePlaylist() {
    if (!playlist) return
    if (!confirm(`Delete "${playlist.name}"? This cannot be undone.`)) return
    try {
      await api.deletePlaylist(playlist.id)
      props.onChanged()
      navigate('/')
    } catch {
      setMenuOpen(false)
    }
  }

  if (loading) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-40 w-full" />
        {Array.from({ length: 5 }, (_, i) => (
          <Skeleton key={i} className="h-14" />
        ))}
      </div>
    )
  }

  if (!playlist) return <EmptyState title="Playlist not found" />

  return (
    <div>
      <div className="animate-rise mb-8 flex flex-col items-start gap-6 sm:flex-row sm:items-end">
        <div className="relative shrink-0">
          {tracks.length > 0 ? (
            // A four-tile collage reads as "a collection" far better than a
            // single album cover, which would imply one album.
            <div className="grid h-44 w-44 grid-cols-2 gap-0.5 overflow-hidden rounded-[1.5rem] shadow-[0_20px_60px_oklch(0_0_0/0.55)] sm:h-52 sm:w-52">
              {tracks.slice(0, 4).map((t, i) => (
                <Cover
                  key={`${t.id}-${i}`}
                  src={coverUrl(t.coverPath)}
                  alt=""
                  seed={t.id}
                  className="h-full w-full"
                  rounded="rounded-none"
                />
              ))}
              {tracks.length < 4 &&
                Array.from({ length: 4 - tracks.length }, (_, i) => (
                  <div
                    key={`filler-${i}`}
                    className="bg-white/5"
                    style={{ background: 'oklch(0.3 0.04 285)' }}
                  />
                ))}
            </div>
          ) : (
            <div
              className="grid h-44 w-44 place-items-center rounded-[1.5rem] sm:h-52 sm:w-52"
              style={{ background: 'linear-gradient(135deg, oklch(0.4 0.1 305), oklch(0.25 0.07 275))' }}
            />
          )}
        </div>

        <div className="min-w-0 flex-1">
          <p className="text-[11px] font-semibold uppercase tracking-[0.2em] text-ink-400">Playlist</p>
          <h1 className="mt-1.5 text-3xl font-bold tracking-tight text-ink-100 sm:text-5xl">
            {playlist.name}
          </h1>
          {playlist.description && (
            <p className="mt-2 max-w-prose text-sm text-ink-400">{playlist.description}</p>
          )}
          <p className="mt-2 text-xs text-ink-500">
            {formatCount(playlist.trackCount, 'track')} · {formatLongDuration(playlist.durationMs)}
          </p>

          <div className="mt-5 flex flex-wrap items-center gap-2.5">
            {tracks.length > 0 && (
              <>
                <button
                  type="button"
                  onClick={() => props.onPlayTracks(tracks, 0)}
                  className="pressable flex items-center gap-2 rounded-full bg-[var(--accent-live)] px-6
                    py-3 text-sm font-semibold text-ink-950 transition hover:brightness-110"
                >
                  <svg viewBox="0 0 24 24" className="h-4 w-4 ml-0.5" fill="currentColor" aria-hidden="true">
                    <path d="M7 4.5v15l13-7.5-13-7.5Z" />
                  </svg>
                  Play
                </button>
                <button
                  type="button"
                  onClick={() =>
                    props.onPlayTracks([...tracks].sort(() => Math.random() - 0.5), 0)
                  }
                  className="pressable flex items-center gap-2 rounded-full bg-white/10 px-5 py-3
                    text-sm font-medium text-ink-100 transition hover:bg-white/16"
                >
                  <ShuffleIcon className="h-4 w-4" />
                  Shuffle
                </button>
              </>
            )}

            <div className="relative">
              <button
                type="button"
                onClick={() => setMenuOpen((v) => !v)}
                aria-label="Playlist options"
                aria-expanded={menuOpen}
                className="pressable grid h-10 w-10 place-items-center rounded-full bg-white/8
                  text-ink-300 transition hover:bg-white/14"
              >
                <MoreIcon />
              </button>
              {menuOpen && (
                <>
                  <button
                    type="button"
                    aria-label="Close menu"
                    className="fixed inset-0 z-10 cursor-default"
                    onClick={() => setMenuOpen(false)}
                  />
                  <div className="glass-bright absolute right-0 top-full z-20 mt-1.5 w-48 overflow-hidden rounded-2xl p-1.5 shadow-2xl">
                    <button
                      type="button"
                      onClick={() => {
                        setMenuOpen(false)
                        const name = prompt('Rename playlist', playlist.name)
                        if (name && name.trim()) {
                          void api.updatePlaylist(playlist.id, { name: name.trim() }).then(() => {
                            void reload()
                            props.onChanged()
                          })
                        }
                      }}
                      className="flex w-full items-center gap-2.5 rounded-xl px-3 py-2 text-sm
                        text-ink-200 transition hover:bg-white/10"
                    >
                      Rename
                    </button>
                    <button
                      type="button"
                      onClick={deletePlaylist}
                      className="flex w-full items-center gap-2.5 rounded-xl px-3 py-2 text-sm
                        text-red-300 transition hover:bg-red-500/15"
                    >
                      <TrashIcon className="h-4 w-4" />
                      Delete playlist
                    </button>
                  </div>
                </>
              )}
            </div>
          </div>
        </div>
      </div>

      {tracks.length === 0 ? (
        <EmptyState
          title="This playlist is empty"
          hint="Use the ••• menu on any track and choose “Add to playlist”."
        />
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
            onRemove={removeTrack}
          />
        </Panel>
      )}
    </div>
  )
}
