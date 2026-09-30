/**
 * Typed client for the Audiora API.
 *
 * Two things make this more than a thin fetch wrapper:
 *
 *  - Tokens live in localStorage and are attached to every request. A 401
 *    triggers exactly one refresh attempt, with concurrent 401s sharing that
 *    single attempt rather than each firing their own rotation (which the
 *    server treats as a stolen token and answers by killing the session).
 *  - The audio URL builder appends an access token as a query parameter.
 *    <audio src> cannot carry an Authorization header, and the server needs
 *    to know who is asking.
 */

export const API_BASE = import.meta.env.VITE_API_BASE ?? ''

const ACCESS_KEY = 'audiora.accessToken'
const REFRESH_KEY = 'audiora.refreshToken'
const EXPIRY_KEY = 'audiora.expiresAt'

export interface User {
  id: number
  email: string
  name: string
  isAdmin: boolean
  createdAt: number
  lastLoginAt?: number
}

export interface Track {
  id: number
  title: string
  durationMs: number
  bitrate: number
  format: string
  hasLyrics: boolean
  trackNo?: number
  discNo: number
  year?: number
  sampleRate: number
  channels: number
  artistId: number
  albumId: number
  artist: string
  album: string
  coverPath?: string | null
  coverColor?: string | null
}

export interface Album {
  id: number
  artistId: number
  title: string
  year?: number
  coverPath?: string | null
  dominantColor?: string | null
  trackCount: number
  durationMs: number
  artist: string
}

export interface Artist {
  id: number
  name: string
  sortName: string
  albumCount: number
  trackCount: number
}

export interface Playlist {
  id: number
  name: string
  description: string
  trackCount: number
  durationMs: number
  coverPath?: string | null
  coverColor?: string | null
  createdAt: number
  updatedAt: number
}

export interface LibraryStats {
  artists: number
  albums: number
  tracks: number
  durationMs: number
  sizeBytes: number
}

export interface ScanState {
  running: boolean
  phase: string
  processed: number
  total: number
  added: number
  updated: number
  removed: number
  skipped: number
  currentFile: string
  error?: string
  startedAt?: number
  finishedAt?: number
}

/** A raised error carrying the HTTP status, so callers can branch on 401. */
export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
  ) {
    super(message)
    this.name = 'ApiError'
  }
}

interface Session {
  accessToken: string
  refreshToken: string
  expiresAt: number
}

export function loadSession(): Session | null {
  const accessToken = localStorage.getItem(ACCESS_KEY)
  if (!accessToken) return null
  return {
    accessToken,
    refreshToken: localStorage.getItem(REFRESH_KEY) ?? '',
    expiresAt: Number(localStorage.getItem(EXPIRY_KEY) ?? '0'),
  }
}

export function saveSession(s: Session) {
  localStorage.setItem(ACCESS_KEY, s.accessToken)
  localStorage.setItem(REFRESH_KEY, s.refreshToken)
  localStorage.setItem(EXPIRY_KEY, String(s.expiresAt))
}

export function clearSession() {
  localStorage.removeItem(ACCESS_KEY)
  localStorage.removeItem(REFRESH_KEY)
  localStorage.removeItem(EXPIRY_KEY)
}

/** Invoked when the session cannot be recovered, so the app can show login. */
let onAuthLost: (() => void) | null = null
export function setAuthLostHandler(fn: () => void) {
  onAuthLost = fn
}

/**
 * refreshInFlight deduplicates refresh attempts. Without this, a screen that
 * fires eight parallel requests would trigger eight refreshes, and the
 * server — which revokes a token on reuse — would kill the session.
 */
let refreshInFlight: Promise<boolean> | null = null

async function refreshSession(): Promise<boolean> {
  if (refreshInFlight) return refreshInFlight

  refreshInFlight = (async () => {
    const session = loadSession()
    if (!session?.refreshToken) return false
    try {
      const res = await fetch(`${API_BASE}/api/auth/refresh`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ refreshToken: session.refreshToken }),
      })
      if (!res.ok) return false
      const data = await res.json()
      saveSession({
        accessToken: data.accessToken,
        refreshToken: data.refreshToken,
        expiresAt: data.expiresAt,
      })
      return true
    } catch {
      return false
    } finally {
      // Cleared in a microtask so callers awaiting this promise all observe
      // the same result before a new attempt can begin.
      queueMicrotask(() => {
        refreshInFlight = null
      })
    }
  })()

  return refreshInFlight
}

interface RequestOptions {
  method?: string
  body?: unknown
  /** Skip the Authorization header, for login and register. */
  anonymous?: boolean
  signal?: AbortSignal
}

async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const { method = 'GET', body, anonymous = false, signal } = options

  const headers: Record<string, string> = {}
  if (body !== undefined) headers['Content-Type'] = 'application/json'

  const session = loadSession()
  if (!anonymous && session) headers['Authorization'] = `Bearer ${session.accessToken}`

  const send = () =>
    fetch(`${API_BASE}${path}`, {
      method,
      headers,
      signal,
      body: body === undefined ? undefined : JSON.stringify(body),
    })

  let res = await send()

  // One transparent refresh-and-retry on expiry.
  if (res.status === 401 && !anonymous && session?.refreshToken) {
    if (await refreshSession()) {
      const retrySession = loadSession()
      if (retrySession) {
        headers['Authorization'] = `Bearer ${retrySession.accessToken}`
        res = await send()
      }
    }
    if (res.status === 401) {
      clearSession()
      onAuthLost?.()
    }
  }

  if (!res.ok) {
    let code = 'error'
    let message = res.statusText
    try {
      const data = await res.json()
      code = data.error ?? code
      message = data.message ?? message
    } catch {
      // A non-JSON error body (a proxy 502, say) leaves the defaults.
    }
    throw new ApiError(res.status, code, message)
  }

  if (res.status === 204) return undefined as T
  return res.json() as Promise<T>
}

// --- endpoints ---

export const api = {
  // Auth
  login: (email: string, password: string) =>
    request<Session & { user: User }>('/api/auth/login', {
      method: 'POST',
      body: { email, password },
      anonymous: true,
    }),
  register: (email: string, password: string, name: string) =>
    request<Session & { user: User }>('/api/auth/register', {
      method: 'POST',
      body: { email, password, name },
      anonymous: true,
    }),
  logout: (refreshToken: string) =>
    request<void>('/api/auth/logout', { method: 'POST', body: { refreshToken } }),
  me: () => request<User>('/api/auth/me'),

  // Library
  stats: () => request<LibraryStats>('/api/library/stats'),
  albums: (limit = 100, offset = 0) =>
    request<{ albums: Album[] }>(`/api/library/albums?limit=${limit}&offset=${offset}`),
  album: (id: number) =>
    request<{ album: Album; tracks: Track[] }>(`/api/library/albums/${id}`),
  artists: (limit = 200, offset = 0) =>
    request<{ artists: Artist[] }>(`/api/library/artists?limit=${limit}&offset=${offset}`),
  artist: (id: number) =>
    request<{ artist: Artist; albums: Album[]; tracks: Track[] }>(`/api/library/artists/${id}`),
  track: (id: number) => request<Track>(`/api/library/tracks/${id}`),
  lyrics: (id: number) =>
    request<{ lyrics: string | null; source: string }>(`/api/library/tracks/${id}/lyrics`),
  search: (q: string, signal?: AbortSignal) =>
    request<{ results: Track[]; query: string }>(
      `/api/search?q=${encodeURIComponent(q)}&limit=40`,
      { signal },
    ),
  suggestions: () =>
    request<{ recentlyAdded: Track[]; topArtists: { name: string; plays: number }[] }>(
      '/api/suggestions',
    ),

  // Playlists and favourites
  playlists: () => request<{ playlists: Playlist[] }>('/api/playlists'),
  createPlaylist: (name: string, description = '', trackIds: number[] = []) =>
    request<{ playlist: Playlist; tracks: Track[] }>('/api/playlists', {
      method: 'POST',
      body: { name, description, trackIds },
    }),
  playlist: (id: number) =>
    request<{ playlist: Playlist; tracks: Track[] }>(`/api/playlists/${id}`),
  updatePlaylist: (id: number, patch: { name?: string; description?: string }) =>
    request<{ playlist: Playlist; tracks: Track[] }>(`/api/playlists/${id}`, {
      method: 'PATCH',
      body: patch,
    }),
  deletePlaylist: (id: number) =>
    request<void>(`/api/playlists/${id}`, { method: 'DELETE' }),
  addToPlaylist: (id: number, trackIds: number[]) =>
    request<{ playlist: Playlist; tracks: Track[] }>(`/api/playlists/${id}/tracks`, {
      method: 'POST',
      body: { trackIds },
    }),
  removeFromPlaylist: (id: number, trackId: number) =>
    request<void>(`/api/playlists/${id}/tracks/${trackId}`, { method: 'DELETE' }),
  reorderPlaylist: (id: number, trackIds: number[]) =>
    request<{ playlist: Playlist; tracks: Track[] }>(`/api/playlists/${id}/reorder`, {
      method: 'POST',
      body: { trackIds },
    }),

  favorites: () => request<{ tracks: Track[] }>('/api/favorites'),
  addFavorite: (trackId: number) =>
    request<void>(`/api/favorites/${trackId}`, { method: 'POST' }),
  removeFavorite: (trackId: number) =>
    request<void>(`/api/favorites/${trackId}`, { method: 'DELETE' }),

  // History
  recordPlay: (trackId: number, completion: number, positionMs: number) =>
    request<{ recorded: boolean; scrobbled: boolean }>('/api/history', {
      method: 'POST',
      body: { trackId, completion, positionMs },
    }),
  recentHistory: (limit = 20) =>
    request<{ tracks: Track[] }>(`/api/history/recent?limit=${limit}`),
  historyStats: () =>
    request<{ totalPlays: number; totalDurationMs: number; playsThisWeek: number }>(
      '/api/history/stats',
    ),

  // Sync
  syncState: () =>
    request<{ state: SyncState; controller: string; seq: number }>('/api/sync/state'),

  // Admin
  adminUsers: () => request<{ users: User[] }>('/api/admin/users'),
  adminCreateUser: (body: { email: string; password: string; name: string; isAdmin: boolean }) =>
    request<User>('/api/admin/users', { method: 'POST', body }),
  adminDeleteUser: (id: number) => request<void>(`/api/admin/users/${id}`, { method: 'DELETE' }),
  adminResetPassword: (id: number, password: string) =>
    request<void>(`/api/admin/users/${id}/password`, { method: 'POST', body: { password } }),
  startScan: () => request<ScanState>('/api/admin/scan', { method: 'POST' }),
  scanState: () => request<ScanState>('/api/admin/scan'),
  cancelScan: () => request<ScanState>('/api/admin/scan', { method: 'DELETE' }),
  cacheInfo: () =>
    request<{ sizeBytes: number; directory: string; prewarm: boolean; profiles: ProfileInfo[] }>(
      '/api/admin/cache',
    ),
  clearCache: () => request<{ removed: number }>('/api/admin/cache', { method: 'DELETE' }),
}

export interface ProfileInfo {
  name: string
  mimeType: string
  kbps: number
  description: string
}

export interface SyncState {
  trackId: number
  positionMs: number
  playing: boolean
  volume: number
  shuffle: boolean
  repeat: 'off' | 'all' | 'one'
  queue: number[]
  updatedAt: number
  profile: string
}

// --- media URLs ---

/**
 * audioUrl builds a playable URL for a track.
 *
 * The access token rides in the query string because an <audio> element
 * cannot send an Authorization header. profile is left empty for the
 * untouched original file.
 */
export function audioUrl(trackId: number, profile?: string): string {
  const session = loadSession()
  const params = new URLSearchParams()
  if (profile) params.set('profile', profile)
  if (session) params.set('t', session.accessToken)
  const qs = params.toString()
  return `${API_BASE}/api/stream/${trackId}${qs ? `?${qs}` : ''}`
}

export function coverUrl(coverPath?: string | null): string | null {
  if (!coverPath) return null
  return `${API_BASE}/api/covers/${coverPath}`
}

/**
 * The WebSocket URL for cross-device sync.
 *
 * The access token is a query parameter because the WebSocket constructor
 * cannot set headers. It is derived from the page origin so the same code
 * works behind Caddy in production and the Vite proxy in development.
 */
export function syncSocketUrl(deviceId: string): string {
  const session = loadSession()
  const params = new URLSearchParams({ device: deviceId })
  if (session) params.set('t', session.accessToken)
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:'
  return `${proto}//${location.host}/api/sync/ws?${params.toString()}`
}
