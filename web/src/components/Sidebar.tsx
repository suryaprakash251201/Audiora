import { NavLink } from 'react-router-dom'
import {
  AlbumIcon,
  ArtistIcon,
  HeartIcon,
  HomeIcon,
  LyricsIcon,
  PlaylistIcon,
  SearchIcon,
  SettingsIcon,
} from './icons'
import { Logo } from './Logo'
import type { Playlist } from '../lib/api'

interface NavItem {
  to: string
  label: string
  icon: typeof HomeIcon
  end?: boolean
}

const primary: NavItem[] = [
  { to: '/', label: 'Listen Now', icon: HomeIcon, end: true },
  { to: '/search', label: 'Search', icon: SearchIcon },
  { to: '/albums', label: 'Albums', icon: AlbumIcon },
  { to: '/artists', label: 'Artists', icon: ArtistIcon },
  { to: '/favorites', label: 'Favourites', icon: HeartIcon },
]

export function Sidebar({
  playlists,
  onNewPlaylist,
}: {
  playlists: Playlist[]
  onNewPlaylist: () => void
}) {
  return (
    <aside className="glass-solid hidden h-full w-[264px] shrink-0 flex-col rounded-none border-y-0 border-l-0 lg:flex">
      <div className="flex items-center gap-3 px-5 pb-5 pt-6">
        <Logo className="h-9 w-9" />
        <div>
          <p className="text-base font-bold leading-tight tracking-tight text-ink-100">Audiora</p>
          <p className="text-[11px] leading-tight text-ink-400">self-hosted</p>
        </div>
      </div>

      <nav className="scroll-pane flex-1 px-3 pb-4">
        <ul className="space-y-0.5">
          {primary.map((item) => (
            <li key={item.to}>
              <NavLink
                to={item.to}
                end={item.end}
                className={({ isActive }) =>
                  `pressable flex items-center gap-3 rounded-xl px-3 py-2.5 text-sm font-medium
                   transition ${
                     isActive
                       ? 'bg-white/12 text-ink-100 shadow-[inset_0_1px_0_0_oklch(1_0_0/0.12)]'
                       : 'text-ink-400 hover:bg-white/6 hover:text-ink-100'
                   }`
                }
              >
                <item.icon className="h-[18px] w-[18px]" />
                {item.label}
              </NavLink>
            </li>
          ))}
        </ul>

        <div className="mt-7 flex items-center justify-between px-3">
          <p className="text-[11px] font-semibold uppercase tracking-[0.16em] text-ink-600">
            Playlists
          </p>
          <button
            type="button"
            onClick={onNewPlaylist}
            aria-label="New playlist"
            className="pressable grid h-6 w-6 place-items-center rounded-full text-ink-400
              transition hover:bg-white/10 hover:text-ink-100"
          >
            <svg viewBox="0 0 24 24" className="h-3.5 w-3.5" fill="none" stroke="currentColor" strokeWidth="2">
              <path d="M12 5v14" strokeLinecap="round" />
              <path d="M5 12h14" strokeLinecap="round" />
            </svg>
          </button>
        </div>

        <ul className="mt-2 space-y-0.5">
          {playlists.length === 0 && (
            <li className="px-3 py-2 text-xs leading-relaxed text-ink-600">
              No playlists yet. Build one from any album.
            </li>
          )}
          {playlists.map((p) => (
            <li key={p.id}>
              <NavLink
                to={`/playlist/${p.id}`}
                className={({ isActive }) =>
                  `pressable flex items-center gap-3 rounded-xl px-3 py-2 text-sm transition ${
                    isActive
                      ? 'bg-white/12 text-ink-100'
                      : 'text-ink-400 hover:bg-white/6 hover:text-ink-100'
                  }`
                }
              >
                <PlaylistIcon className="h-4 w-4 shrink-0" />
                <span className="truncate">{p.name}</span>
              </NavLink>
            </li>
          ))}
        </ul>
      </nav>

      <div className="px-3 pb-4">
        <NavLink
          to="/settings"
          className={({ isActive }) =>
            `pressable flex items-center gap-3 rounded-xl px-3 py-2.5 text-sm font-medium transition ${
              isActive
                ? 'bg-white/12 text-ink-100'
                : 'text-ink-400 hover:bg-white/6 hover:text-ink-100'
            }`
          }
        >
          <SettingsIcon className="h-[18px] w-[18px]" />
          Settings
        </NavLink>
        <div className="mt-3 flex items-center gap-2 px-3 text-[11px] text-ink-600">
          <LyricsIcon className="h-3.5 w-3.5" />
          <span>FLAC · MP3 · WAV · M4A</span>
        </div>
      </div>
    </aside>
  )
}
