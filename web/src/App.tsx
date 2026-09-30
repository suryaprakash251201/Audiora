import { useCallback, useEffect, useMemo, useState } from 'react'
import { NavLink, Route, Routes, useLocation, useNavigate } from 'react-router-dom'
import { AuthProvider, useAuth } from './lib/auth'
import { player, type PlayerSnapshot } from './lib/player'
import { SyncClient } from './lib/sync'
import { api, type Playlist, type Track } from './lib/api'
import type { QualityPreference } from './lib/quality'

import { Sidebar } from './components/Sidebar'
import { PlayerBar } from './components/PlayerBar'
import { NowPlaying } from './components/NowPlaying'
import { QueueSheet, registerTracks, trackFromCache } from './components/QueueSheet'
import { useAccentColor } from './components/ui'
import { AlbumIcon, ArtistIcon, HeartIcon, HomeIcon, SearchIcon, SettingsIcon } from './components/icons'

import { SignIn } from './pages/SignIn'
import { HomePage } from './pages/Home'
import { AlbumsPage, AlbumPage, ArtistsPage, ArtistPage } from './pages/Library'
import { SearchPage, FavoritesPage } from './pages/Browse'
import { PlaylistPage } from './pages/Playlist'
import { SettingsPage, PlaylistPicker } from './pages/Settings'

export default function App() {
  return (
    <AuthProvider>
      <Shell />
    </AuthProvider>
  )
}

function Shell() {
  const { user, loading, logout } = useAuth()
  const location = useLocation()
  const navigate = useNavigate()

  const [snapshot, setSnapshot] = useState<PlayerSnapshot>(() => player.snapshot())
  const [playlists, setPlaylists] = useState<Playlist[]>([])
  const [favoriteIds, setFavoriteIds] = useState<Set<number>>(new Set())
  const [nowPlayingOpen, setNowPlayingOpen] = useState(false)
  const [queueOpen, setQueueOpen] = useState(false)
  // The picker can be opened with a specific track (from a track row's menu)
  // or with none at all (from the sidebar's plus button or the queue sheet),
  // so openness and the track are tracked separately rather than with a
  // sentinel value.
  const [picker, setPicker] = useState<{ open: boolean; track: Track | null }>({
    open: false,
    track: null,
  })
  const [quality, setQuality] = useState<QualityPreference>('auto')

  // A single subscription drives every piece of the UI that shows playback
  // state, so they can never disagree about what is playing.
  useEffect(() => player.subscribe(setSnapshot), [])

  // Load the data every page needs: the sidebar's playlists and the heart
  // states in track lists.
  const refreshLibraryBits = useCallback(async () => {
    try {
      const [p, f] = await Promise.all([api.playlists(), api.favorites()])
      setPlaylists(p.playlists)
      setFavoriteIds(new Set(f.tracks.map((t) => t.id)))
    } catch {
      // A failed refresh is not worth interrupting the session for; the next
      // navigation or mutation will retry.
    }
  }, [])

  useEffect(() => {
    if (!user) return
    void refreshLibraryBits()
  }, [user, refreshLibraryBits])

  // Cross-device sync, only once there is a session to sync.
  useEffect(() => {
    if (!user) return
    const client = new SyncClient(player)
    client.connect()
    return () => client.disconnect()
  }, [user])

  // The theme follows the artwork of whatever is playing.
  useAccentColor(snapshot.current?.coverColor ?? null)

  // Apply the stored quality preference to the player on boot.
  useEffect(() => {
    const stored = localStorage.getItem('audiora.quality') as QualityPreference | null
    if (stored) {
      setQuality(stored)
      player.setQuality(stored)
    }
  }, [])

  // Close the Now Playing sheet when navigating, so it does not sit on top of
  // a new page on mobile.
  useEffect(() => {
    setNowPlayingOpen(false)
  }, [location.pathname])

  const onPlayTracks = useCallback((tracks: Track[], startAt = 0) => {
    registerTracks(tracks)
    player.playTracks(tracks, startAt)
  }, [])

  const onToggleFavorite = useCallback(
    async (track: Track) => {
      // Optimistic, with a rollback, so tapping the heart feels instant.
      const wasFavorite = favoriteIds.has(track.id)
      setFavoriteIds((prev) => {
        const next = new Set(prev)
        if (wasFavorite) next.delete(track.id)
        else next.add(track.id)
        return next
      })
      try {
        if (wasFavorite) await api.removeFavorite(track.id)
        else await api.addFavorite(track.id)
      } catch {
        setFavoriteIds((prev) => {
          const next = new Set(prev)
          if (wasFavorite) next.add(track.id)
          else next.delete(track.id)
          return next
        })
      }
    },
    [favoriteIds],
  )

  const playbackProps = useMemo(
    () => ({
      snapshot,
      favoriteIds,
      onPlayTracks,
      onToggleFavorite,
      onAddToPlaylist: (track: Track) => setPicker({ open: true, track }),
    }),
    [snapshot, favoriteIds, onPlayTracks, onToggleFavorite],
  )

  function onQualityChange(next: QualityPreference) {
    setQuality(next)
    localStorage.setItem('audiora.quality', next)
    player.setQuality(next)
  }

  async function pickPlaylist(playlistId: number) {
    const track = picker.track
    setPicker({ open: false, track: null })
    if (!track) return
    try {
      await api.addToPlaylist(playlistId, [track.id])
      await refreshLibraryBits()
    } catch {
      // Nothing useful to show here; the picker closing is the signal.
    }
  }

  async function createPlaylistWithTrack(name: string) {
    const track = picker.track
    setPicker({ open: false, track: null })
    try {
      // With no track selected this is just "new playlist".
      const result = await api.createPlaylist(name, '', track ? [track.id] : [])
      await refreshLibraryBits()
      // Creating from the sidebar or the queue should take the listener to it.
      if (!track) navigate(`/playlist/${result.playlist.id}`)
    } catch {
      // Same as above.
    }
  }

  if (loading) {
    return (
      <div className="grid h-full place-items-center">
        <div className="ambient" aria-hidden="true" />
        <div className="animate-fade flex flex-col items-center gap-3">
          <div className="h-8 w-8 animate-spin rounded-full border-2 border-white/20 border-t-[var(--accent-live)]" />
          <p className="text-sm text-ink-400">Loading…</p>
        </div>
      </div>
    )
  }

  if (!user) {
    return (
      <>
        <div className="ambient" aria-hidden="true" />
        <SignIn />
      </>
    )
  }

  const hasBar = Boolean(snapshot.current)

  return (
    <div className="relative flex h-full flex-col">
      <div className="ambient" aria-hidden="true" />

      <div className="flex min-h-0 flex-1">
        <Sidebar playlists={playlists} onNewPlaylist={() => setPicker({ open: true, track: null })} />

        <main
          className="scroll-pane min-w-0 flex-1 px-4 pb-6 sm:px-6 lg:px-8"
          style={{
            paddingTop: 'max(1.5rem, env(safe-area-inset-top))',
            // Room for the player bar plus the mobile tab bar.
            paddingBottom: hasBar ? '9.5rem' : '5.5rem',
          }}
        >
          <Routes>
            <Route path="/" element={<HomePage onPlayTracks={onPlayTracks} />} />
            <Route path="/search" element={<SearchPage {...playbackProps} />} />
            <Route path="/albums" element={<AlbumsPage {...playbackProps} />} />
            <Route path="/album/:id" element={<AlbumPage {...playbackProps} />} />
            <Route path="/artists" element={<ArtistsPage {...playbackProps} />} />
            <Route path="/artist/:id" element={<ArtistPage {...playbackProps} />} />
            <Route path="/favorites" element={<FavoritesPage {...playbackProps} />} />
            <Route
              path="/playlist/:id"
              element={<PlaylistPage {...playbackProps} onChanged={refreshLibraryBits} />}
            />
            <Route
              path="/settings"
              element={
                <SettingsPage
                  onLogout={logout}
                  quality={quality}
                  onQualityChange={onQualityChange}
                  profile={snapshot.profile}
                />
              }
            />
            <Route path="*" element={<HomePage onPlayTracks={onPlayTracks} />} />
          </Routes>
        </main>
      </div>

      {/* Player bar and mobile tab bar stack at the bottom. */}
      {hasBar && (
        <div className="pointer-events-none fixed inset-x-0 bottom-0 z-30">
          <PlayerBar
            snapshot={snapshot}
            onToggle={() => player.toggle()}
            onNext={() => player.next()}
            onPrevious={() => player.previous()}
            onSeek={(s) => player.seek(s)}
            onVolume={(v) => player.setVolume(v)}
            onToggleMute={() => player.toggleMute()}
            onToggleShuffle={() => player.toggleShuffle()}
            onCycleRepeat={() => player.cycleRepeat()}
            onOpenNowPlaying={() => setNowPlayingOpen(true)}
            onOpenQueue={() => setQueueOpen(true)}
            onOpenLyrics={() => setNowPlayingOpen(true)}
          />
          <MobileTabBar />
        </div>
      )}

      {!hasBar && (
        <div className="pointer-events-none fixed inset-x-0 bottom-0 z-30">
          <MobileTabBar />
        </div>
      )}

      {nowPlayingOpen && snapshot.current && (
        <NowPlaying
          snapshot={snapshot}
          onClose={() => setNowPlayingOpen(false)}
          onToggle={() => player.toggle()}
          onNext={() => player.next()}
          onPrevious={() => player.previous()}
          onSeek={(s) => player.seek(s)}
          isFavorite={favoriteIds.has(snapshot.current!.id)}
          onToggleFavorite={() => onToggleFavorite(snapshot.current!)}
        />
      )}

      {queueOpen && (
        <QueueSheet
          snapshot={snapshot}
          onClose={() => setQueueOpen(false)}
          onPlayAt={(index) => {
            const slot = snapshot.queue.sequence[index]
            const trackId = slot === undefined ? null : snapshot.queue.order[slot]
            if (trackId != null) {
              const track = trackFromCache(trackId)
              if (track) onPlayTracks([track], 0)
            }
            setQueueOpen(false)
          }}
          onRemoveAt={(index) => player.removeAt(index)}
          onMove={(from, to) => player.move(from, to)}
          onClear={() => player.clear()}
          onSaveAsPlaylist={() => {
            setQueueOpen(false)
            setPicker({ open: true, track: null })
          }}
        />
      )}

      {picker.open && (
        <PlaylistPicker
          playlists={playlists}
          trackName={picker.track?.title ?? null}
          onClose={() => setPicker({ open: false, track: null })}
          onPick={pickPlaylist}
          onCreate={createPlaylistWithTrack}
        />
      )}
    </div>
  )
}

/** Bottom tab bar, phone only. */
function MobileTabBar() {
  const tabs = [
    { to: '/', label: 'Home', icon: HomeIcon, end: true },
    { to: '/search', label: 'Search', icon: SearchIcon },
    { to: '/albums', label: 'Albums', icon: AlbumIcon },
    { to: '/artists', label: 'Artists', icon: ArtistIcon },
    { to: '/favorites', label: 'Loved', icon: HeartIcon },
    { to: '/settings', label: 'Settings', icon: SettingsIcon },
  ]

  return (
    <nav
      className="glass-solid pointer-events-auto border-x-0 border-b-0 px-safe lg:hidden"
      style={{ paddingBottom: 'max(0.35rem, env(safe-area-inset-bottom))' }}
    >
      <ul className="flex items-stretch justify-around">
        {tabs.map((tab) => (
          <li key={tab.to} className="flex-1">
            <NavLink
              to={tab.to}
              end={tab.end}
              className={({ isActive }) =>
                `flex flex-col items-center gap-0.5 py-2 text-[10px] font-medium transition ${
                  isActive ? 'text-[var(--accent-live)]' : 'text-ink-500'
                }`
              }
            >
              <tab.icon className="h-[22px] w-[22px]" />
              {tab.label}
            </NavLink>
          </li>
        ))}
      </ul>
    </nav>
  )
}
