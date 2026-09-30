import { useEffect, useRef, useState } from 'react'
import { api, type Playlist, type ScanState, type User, type ProfileInfo } from '../lib/api'
import { useAuth } from '../lib/auth'
import { formatBytes, formatCount } from '../lib/format'
import { Panel } from '../components/ui'
import { describeProfile, type QualityPreference } from '../lib/quality'
import { RefreshIcon, TrashIcon, CloseIcon, MusicNoteIcon, CheckIcon } from '../components/icons'

export function SettingsPage({
  onLogout,
  quality,
  onQualityChange,
  profile,
}: {
  onLogout: () => void
  quality: QualityPreference
  onQualityChange: (value: QualityPreference) => void
  profile: string
}) {
  const { user } = useAuth()

  return (
    <div className="max-w-2xl space-y-6">
      <header className="animate-rise">
        <p className="mb-1 text-xs font-semibold uppercase tracking-[0.18em] text-ink-400">Account</p>
        <h1 className="text-3xl font-bold tracking-tight text-ink-100">Settings</h1>
      </header>

      <Panel className="p-5">
        <h2 className="mb-4 text-sm font-semibold uppercase tracking-wider text-ink-400">Signed in</h2>
        <dl className="space-y-2 text-sm">
          <div className="flex justify-between">
            <dt className="text-ink-500">Name</dt>
            <dd className="text-ink-100">{user?.name || '—'}</dd>
          </div>
          <div className="flex justify-between">
            <dt className="text-ink-500">Email</dt>
            <dd className="text-ink-100">{user?.email}</dd>
          </div>
          <div className="flex justify-between">
            <dt className="text-ink-500">Role</dt>
            <dd className="text-ink-100">{user?.isAdmin ? 'Administrator' : 'Listener'}</dd>
          </div>
        </dl>
        <button
          type="button"
          onClick={onLogout}
          className="pressable mt-5 rounded-full border border-white/12 px-4 py-2 text-sm
            text-ink-200 transition hover:bg-white/8"
        >
          Sign out
        </button>
      </Panel>

      <Panel className="p-5">
        <h2 className="mb-1 text-sm font-semibold uppercase tracking-wider text-ink-400">
          Audio quality
        </h2>
        <p className="mb-4 text-xs leading-relaxed text-ink-500">
          Lossless sends the original FLAC, WAV or M4A file straight to your device. The
          lower options are transcoded on the server once and then cached, so they cost
          less data and play instantly afterwards.
        </p>

        <div className="space-y-2">
          {(
            [
              ['auto', 'Automatic', 'Picks based on your connection and device.'],
              ['lossless', 'Lossless', 'Always the original file. Best on wifi.'],
              ['high', 'High', '320 kbps AAC, or 64 kbps Opus where supported.'],
              ['standard', 'Standard', '96 kbps AAC. A sensible default for mobile.'],
              ['data-saver', 'Data saver', '96 kbps AAC, regardless of connection.'],
            ] as const
          ).map(([value, label, hint]) => (
            <button
              key={value}
              type="button"
              onClick={() => onQualityChange(value)}
              className={`pressable flex w-full items-start gap-3 rounded-2xl p-3.5 text-left transition
                ${quality === value ? 'bg-white/12' : 'bg-white/4 hover:bg-white/8'}`}
            >
              <span
                className={`mt-0.5 grid h-4.5 w-4.5 shrink-0 place-items-center rounded-full border
                  ${quality === value
                    ? 'border-[var(--accent-live)] bg-[var(--accent-live)]'
                    : 'border-ink-600'}`}
              >
                {quality === value && <CheckIcon className="h-3 w-3 text-ink-950" />}
              </span>
              <span className="min-w-0">
                <span className="block text-sm font-medium text-ink-100">{label}</span>
                <span className="block text-xs leading-relaxed text-ink-400">{hint}</span>
              </span>
            </button>
          ))}
        </div>

        <p className="mt-3 text-xs text-ink-600">
          Currently streaming as <span className="text-ink-300">{describeProfile(profile)}</span>.
        </p>
      </Panel>

      {user?.isAdmin && <AdminPanel />}
    </div>
  )
}

function AdminPanel() {
  const [scan, setScan] = useState<ScanState | null>(null)
  const [users, setUsers] = useState<User[]>([])
  const [cache, setCache] = useState<{ sizeBytes: number; profiles: ProfileInfo[] } | null>(null)
  const [busy, setBusy] = useState(false)
  const [newUser, setNewUser] = useState({ email: '', password: '', name: '', isAdmin: false })
  const [message, setMessage] = useState<string | null>(null)
  const pollRef = useRef<number | null>(null)

  async function refresh() {
    try {
      const [s, u, c] = await Promise.all([api.scanState(), api.adminUsers(), api.cacheInfo()])
      setScan(s)
      setUsers(u.users)
      setCache(c)
    } catch {
      // Admin endpoints failing means the session lost its admin claim; the
      // page will re-render without this panel.
    }
  }

  useEffect(() => {
    void refresh()
  }, [])

  // Poll only while a scan is actually running, so an idle admin page makes
  // no requests.
  useEffect(() => {
    if (scan?.running) {
      pollRef.current = window.setInterval(() => void refresh(), 900)
    } else if (pollRef.current !== null) {
      clearInterval(pollRef.current)
      pollRef.current = null
    }
    return () => {
      if (pollRef.current !== null) clearInterval(pollRef.current)
    }
  }, [scan?.running])

  async function startScan() {
    setBusy(true)
    setMessage(null)
    try {
      await api.startScan()
      await refresh()
    } catch (err) {
      setMessage(err instanceof Error ? err.message : 'Could not start a scan')
    } finally {
      setBusy(false)
    }
  }

  async function createUser() {
    if (!newUser.email || !newUser.password) return
    setBusy(true)
    setMessage(null)
    try {
      await api.adminCreateUser(newUser)
      setNewUser({ email: '', password: '', name: '', isAdmin: false })
      await refresh()
      setMessage(`Created ${newUser.email}`)
    } catch (err) {
      setMessage(err instanceof Error ? err.message : 'Could not create that user')
    } finally {
      setBusy(false)
    }
  }

  async function deleteUser(id: number, email: string) {
    if (!confirm(`Delete ${email}? Their playlists and history go too.`)) return
    try {
      await api.adminDeleteUser(id)
      await refresh()
    } catch (err) {
      setMessage(err instanceof Error ? err.message : 'Could not delete that user')
    }
  }

  const percent = scan && scan.total > 0 ? (scan.processed / scan.total) * 100 : 0

  return (
    <>
      {/* Library scanning */}
      <Panel className="p-5">
        <div className="mb-4 flex items-start justify-between gap-3">
          <div>
            <h2 className="text-sm font-semibold uppercase tracking-wider text-ink-400">
              Library scan
            </h2>
            <p className="mt-1 text-xs leading-relaxed text-ink-500">
              Walks your music folder and adds anything new. Unchanged files are skipped, so
              repeat scans are fast.
            </p>
          </div>
          {scan?.running ? (
            <button
              type="button"
              onClick={() => void api.cancelScan().then(() => refresh())}
              className="pressable shrink-0 rounded-full border border-white/12 px-4 py-2 text-xs
                text-ink-200 transition hover:bg-white/8"
            >
              Cancel
            </button>
          ) : (
            <button
              type="button"
              onClick={startScan}
              disabled={busy}
              className="pressable flex shrink-0 items-center gap-2 rounded-full bg-[var(--accent-live)]
                px-4 py-2 text-xs font-semibold text-ink-950 transition hover:brightness-110
                disabled:opacity-50"
            >
              <RefreshIcon className="h-3.5 w-3.5" />
              Scan now
            </button>
          )}
        </div>

        {scan?.running && (
          <div className="animate-fade">
            <div className="mb-2 h-1.5 overflow-hidden rounded-full bg-white/8">
              <div
                className="h-full rounded-full bg-[var(--accent-live)] transition-[width] duration-300"
                style={{ width: `${percent}%` }}
              />
            </div>
            <p className="truncate text-xs text-ink-500">
              {scan.processed.toLocaleString()} of {scan.total.toLocaleString()} files · {scan.phase}
              {scan.currentFile && <span className="text-ink-600"> · {scan.currentFile}</span>}
            </p>
          </div>
        )}

        {scan && !scan.running && (
          <dl className="grid grid-cols-2 gap-3 text-xs sm:grid-cols-4">
            <Metric label="Added" value={scan.added} />
            <Metric label="Updated" value={scan.updated} />
            <Metric label="Removed" value={scan.removed} />
            <Metric label="Skipped" value={scan.skipped} />
          </dl>
        )}

        {scan?.error && (
          <p className="mt-3 rounded-lg border border-red-400/25 bg-red-500/10 px-3 py-2 text-xs text-red-200">
            {scan.error}
          </p>
        )}
      </Panel>

      {/* Transcode cache */}
      {cache && (
        <Panel className="p-5">
          <div className="flex items-start justify-between gap-3">
            <div>
              <h2 className="text-sm font-semibold uppercase tracking-wider text-ink-400">
                Transcode cache
              </h2>
              <p className="mt-1 text-xs text-ink-500">
                {formatBytes(cache.sizeBytes)} of generated audio. Clearing it frees disk; the
                next play of each track regenerates it.
              </p>
            </div>
            <button
              type="button"
              onClick={() => void api.clearCache().then(() => refresh())}
              className="pressable flex shrink-0 items-center gap-2 rounded-full border border-white/12
                px-4 py-2 text-xs text-ink-200 transition hover:bg-white/8"
            >
              <TrashIcon className="h-3.5 w-3.5" />
              Clear
            </button>
          </div>

          <div className="mt-3 flex flex-wrap gap-1.5">
            {cache.profiles.map((p) => (
              <span key={p.name} className="chip text-[11px]">
                {p.name === 'lossless' ? 'lossless' : p.description}
              </span>
            ))}
          </div>
        </Panel>
      )}

      {/* Users */}
      <Panel className="p-5">
        <h2 className="mb-1 text-sm font-semibold uppercase tracking-wider text-ink-400">
          People
        </h2>
        <p className="mb-4 text-xs text-ink-500">
          Everyone shares the same music library, but has their own favourites, playlists and
          listening history.
        </p>

        <ul className="mb-5 space-y-2">
          {users.map((u) => (
            <li
              key={u.id}
              className="flex items-center gap-3 rounded-2xl bg-white/4 p-3"
            >
              <div className="grid h-9 w-9 shrink-0 place-items-center rounded-full bg-white/10 text-sm font-semibold text-ink-100">
                {(u.name || u.email).charAt(0).toUpperCase()}
              </div>
              <div className="min-w-0 flex-1">
                <p className="truncate text-sm font-medium text-ink-100">
                  {u.name || u.email}
                  {u.isAdmin && (
                    <span className="ml-2 rounded-full bg-[var(--accent-live)]/20 px-2 py-0.5 text-[10px] font-semibold text-[var(--accent-live)]">
                      admin
                    </span>
                  )}
                </p>
                <p className="truncate text-xs text-ink-500">{u.email}</p>
              </div>
              <button
                type="button"
                onClick={() => deleteUser(u.id, u.email)}
                aria-label={`Delete ${u.email}`}
                className="pressable grid h-8 w-8 shrink-0 place-items-center rounded-full
                  text-ink-600 transition hover:bg-red-500/15 hover:text-red-300"
              >
                <TrashIcon className="h-4 w-4" />
              </button>
            </li>
          ))}
        </ul>

        <div className="space-y-2.5">
          <input
            type="email"
            value={newUser.email}
            onChange={(e) => setNewUser({ ...newUser, email: e.target.value })}
            placeholder="Email"
            className="w-full rounded-xl border border-white/10 bg-white/5 px-3.5 py-2.5 text-sm
              text-ink-100 outline-none placeholder:text-ink-600
              focus:border-[var(--accent-live)] focus:bg-white/8"
          />
          <input
            type="text"
            value={newUser.name}
            onChange={(e) => setNewUser({ ...newUser, name: e.target.value })}
            placeholder="Name (optional)"
            className="w-full rounded-xl border border-white/10 bg-white/5 px-3.5 py-2.5 text-sm
              text-ink-100 outline-none placeholder:text-ink-600
              focus:border-[var(--accent-live)] focus:bg-white/8"
          />
          <input
            type="password"
            value={newUser.password}
            onChange={(e) => setNewUser({ ...newUser, password: e.target.value })}
            placeholder="Password (at least 8 characters)"
            className="w-full rounded-xl border border-white/10 bg-white/5 px-3.5 py-2.5 text-sm
              text-ink-100 outline-none placeholder:text-ink-600
              focus:border-[var(--accent-live)] focus:bg-white/8"
          />
          <label className="flex items-center gap-2 text-xs text-ink-400">
            <input
              type="checkbox"
              checked={newUser.isAdmin}
              onChange={(e) => setNewUser({ ...newUser, isAdmin: e.target.checked })}
              className="h-4 w-4 accent-[var(--accent-live)]"
            />
            Grant administrator (can scan the library and manage people)
          </label>
          <button
            type="button"
            onClick={createUser}
            disabled={busy || !newUser.email || newUser.password.length < 8}
            className="pressable w-full rounded-xl bg-white/10 py-2.5 text-sm font-medium
              text-ink-100 transition hover:bg-white/16 disabled:opacity-40"
          >
            Add person
          </button>
          {message && <p className="text-xs text-ink-400">{message}</p>}
        </div>
      </Panel>
    </>
  )
}

function Metric({ label, value }: { label: string; value: number }) {
  return (
    <div>
      <dt className="text-[10px] font-semibold uppercase tracking-wider text-ink-600">{label}</dt>
      <dd className="mt-0.5 text-lg font-semibold tabular-nums text-ink-100">
        {value.toLocaleString()}
      </dd>
    </div>
  )
}

/**
 * The "add to playlist" sheet.
 *
 * Opened with a specific track (from a track row's menu) or with none at all
 * (from the sidebar's plus button or the queue sheet), in which case it
 * doubles as the "new playlist" dialog.
 */
export function PlaylistPicker({
  playlists,
  trackName,
  onClose,
  onPick,
  onCreate,
}: {
  playlists: Playlist[]
  trackName: string | null
  onClose: () => void
  onPick: (playlistId: number) => void
  onCreate: (name: string) => void
}) {
  const [name, setName] = useState('')
  const hasTrack = trackName !== null

  return (
    <div className="animate-fade fixed inset-0 z-50 flex items-end justify-center sm:items-center">
      <button
        type="button"
        aria-label="Close"
        className="absolute inset-0 bg-black/55 backdrop-blur-sm"
        onClick={onClose}
      />
      <Panel className="animate-rise relative z-10 max-h-[80vh] w-full max-w-sm overflow-hidden p-5">
        <div className="mb-4 flex items-center justify-between">
          <h2 className="text-base font-semibold text-ink-100">
            {hasTrack ? 'Add to playlist' : 'New playlist'}
          </h2>
          <button
            type="button"
            onClick={onClose}
            aria-label="Close"
            className="pressable grid h-8 w-8 place-items-center rounded-full text-ink-400
              transition hover:bg-white/10"
          >
            <CloseIcon className="h-4 w-4" />
          </button>
        </div>

        {hasTrack && (
          <p className="mb-3 truncate rounded-xl bg-white/6 px-3 py-2 text-xs text-ink-300">
            {trackName}
          </p>
        )}

        {playlists.length === 0 ? (
          <p className="mb-4 text-sm text-ink-400">
            You have no playlists yet. Create one below.
          </p>
        ) : (
          <ul className="scroll-pane mb-4 max-h-56 space-y-1">
            {playlists.map((p) => (
              <li key={p.id}>
                <button
                  type="button"
                  onClick={() => onPick(p.id)}
                  disabled={!hasTrack}
                  className="pressable flex w-full items-center gap-3 rounded-xl px-3 py-2.5
                    text-left transition hover:bg-white/8 disabled:opacity-40"
                >
                  <MusicNoteIcon className="h-4 w-4 shrink-0 text-ink-400" />
                  <span className="min-w-0 flex-1 truncate text-sm text-ink-100">{p.name}</span>
                  <span className="shrink-0 text-xs text-ink-600">
                    {formatCount(p.trackCount, 'track')}
                  </span>
                </button>
              </li>
            ))}
          </ul>
        )}

        <form
          onSubmit={(e) => {
            e.preventDefault()
            if (name.trim()) onCreate(name.trim())
          }}
          className="flex gap-2"
        >
          <input
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder={hasTrack ? 'Or create a new playlist' : 'Playlist name'}
            autoFocus={!hasTrack}
            className="min-w-0 flex-1 rounded-xl border border-white/10 bg-white/5 px-3.5 py-2.5
              text-sm text-ink-100 outline-none placeholder:text-ink-600
              focus:border-[var(--accent-live)] focus:bg-white/8"
          />
          <button
            type="submit"
            disabled={!name.trim()}
            className="pressable shrink-0 rounded-xl bg-[var(--accent-live)] px-4 py-2.5 text-sm
              font-semibold text-ink-950 transition hover:brightness-110 disabled:opacity-40"
          >
            Create
          </button>
        </form>
      </Panel>
    </div>
  )
}
