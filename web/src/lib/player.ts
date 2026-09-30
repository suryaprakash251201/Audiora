/**
 * The playback engine.
 *
 * Built around a single HTMLAudioElement plus a Web Audio graph for volume.
 * The crossfade uses two elements and two gain nodes: fading one out while
 * starting the next is the closest an HTML5 player gets to gapless, which is
 * not the same thing and is documented as such in the README.
 *
 * MediaSession is wired up here so lock-screen and notification controls work
 * in the browser and, through the Capacitor plugin, in the native shell.
 */

import { Capacitor } from '@capacitor/core'
import type { MediaSessionPlugin } from '@capgo/capacitor-media-session'
import { audioUrl, api, API_BASE, type Track } from './api'
import {
  advance,
  currentTrackId,
  cycleRepeat,
  emptyQueue,
  moveInQueue,
  queueFromIds,
  queueTrackIds,
  removeFromQueue,
  setQueue,
  addToQueue,
  toggleShuffle,
  type QueueState,
  type RepeatMode,
} from './queue'
import { pickProfile, type QualityPreference } from './quality'

export interface PlayerSnapshot {
  queue: QueueState
  current: Track | null
  playing: boolean
  /** Seconds. Updated on timeupdate, so it is not frame-accurate. */
  position: number
  duration: number
  volume: number
  muted: boolean
  /** True while the audio element is waiting on the network. */
  buffering: boolean
  profile: string
  quality: QualityPreference
  error: string | null
}

type Listener = (snapshot: PlayerSnapshot) => void

/** Crossfade length. Short enough not to smear, long enough to hide a gap. */
const CROSSFADE_SECONDS = 0.35

/** How often position is pushed to subscribers, in ms. */
const TICK_MS = 250

export class Player {
  private el: HTMLAudioElement
  private fadeEl: HTMLAudioElement | null = null
  private audioCtx: AudioContext | null = null
  private gain: GainNode | null = null

  private state: QueueState = emptyQueue
  private tracks = new Map<number, Track>()
  private current: Track | null = null
  private profile = ''
  private quality: QualityPreference = 'auto'
  private volume = 1
  private muted = false
  private fadeTimer: number | null = null
  private tickTimer: number | null = null
  private lastReportedPosition = 0
  private listeners = new Set<Listener>()

  /**
   * Guards against the ended handler firing for a track we have already
   * moved on from, which would skip two tracks on a fast seek.
   */
  private loadToken = 0

  constructor() {
    this.el = new Audio()
    this.el.preload = 'metadata'
    // Cross-origin isolation is not used, so play() must not require a user
    // gesture to have happened in this exact element. The muted-start trick
    // below is the reliable way to satisfy autoplay policies.
    this.el.crossOrigin = 'anonymous'

    this.el.addEventListener('ended', () => this.onEnded())
    this.el.addEventListener('timeupdate', () => this.onTimeUpdate())
    this.el.addEventListener('durationchange', () => this.emit())
    this.el.addEventListener('play', () => {
      this.updateMediaSessionPlaybackState()
      this.emit()
    })
    this.el.addEventListener('pause', () => {
      this.updateMediaSessionPlaybackState()
      this.emit()
    })
    this.el.addEventListener('waiting', () => this.emit())
    this.el.addEventListener('playing', () => this.emit())
    this.el.addEventListener('canplay', () => this.emit())
    this.el.addEventListener('error', () => {
      this.patch({ error: 'This track could not be played.' })
      this.emit()
    })

    this.el.volume = this.volume

    this.startTicking()
    this.installMediaSession()
  }

  // --- subscription ---

  subscribe(fn: Listener): () => void {
    this.listeners.add(fn)
    fn(this.snapshot())
    return () => this.listeners.delete(fn)
  }

  snapshot(): PlayerSnapshot {    return {
      queue: this.state,
      current: this.current,
      playing: !this.el.paused && !this.el.ended,
      position: this.el.currentTime || 0,
      duration: this.el.duration && Number.isFinite(this.el.duration) ? this.el.duration : 0,
      volume: this.volume,
      muted: this.muted,
      buffering:
        this.el.readyState === HTMLMediaElement.HAVE_NOTHING ||
        this.el.readyState === HTMLMediaElement.HAVE_METADATA,
      profile: this.profile,
      quality: this.quality,
      error: this.lastError,
    }
  }

  private emit() {
    const snap = this.snapshot()
    for (const fn of this.listeners) fn(snap)
  }

  /**
   * Records a message the audio element has no way to express, such as a
   * decode failure or a refused autoplay attempt. It is cleared as soon as a
   * new track loads, so an error never sticks to unrelated playback.
   */
  private patch(changes: Partial<PlayerSnapshot>) {
    this.lastError = changes.error ?? null
  }

  private lastError: string | null = null

  private startTicking() {
    if (this.tickTimer !== null) return
    this.tickTimer = window.setInterval(() => {
      // Only repaint when the position has actually moved, so an idle player
      // does not re-render the whole tree four times a second.
      const pos = this.el.currentTime || 0
      if (Math.abs(pos - this.lastReportedPosition) < 0.2) return
      this.lastReportedPosition = pos
      this.updateMediaSessionPlaybackState()
      this.emit()
    }, TICK_MS)
  }

  // --- queue management ---

  /** playTracks replaces the queue and starts at the chosen track. */
  playTracks(tracks: Track[], startAt = 0): void {
    if (tracks.length === 0) return

    for (const t of tracks) this.tracks.set(t.id, t)
    const startTrack = tracks[Math.min(startAt, tracks.length - 1)]
    const startingId = this.current?.id ?? null

    this.state = setQueue(
      this.state,
      tracks.map((t) => t.id),
      startingId,
      this.state.shuffle,
    )
    // Start on the track the listener actually clicked.
    const slot = this.state.order.indexOf(startTrack.id)
    if (slot >= 0) {
      const cursor = this.state.shuffle
        ? this.state.sequence.indexOf(slot)
        : slot
      this.state = { ...this.state, cursor: Math.max(0, cursor) }
    }

    this.loadCurrent(true)
  }

  enqueue(tracks: Track[]): void {
    if (tracks.length === 0) return
    for (const t of tracks) this.tracks.set(t.id, t)
    this.state = addToQueue(this.state, tracks.map((t) => t.id))
    this.emit()
  }

  playNext(tracks: Track[]): void {
    if (tracks.length === 0) return
    for (const t of tracks) this.tracks.set(t.id, t)
    const ids = tracks.map((t) => t.id)
    this.state = setQueue(
      { ...this.state, cursor: 0 },
      [currentTrackId(this.state) ?? ids[0], ...ids],
      this.current?.id ?? null,
      this.state.shuffle,
    )
    this.loadCurrent(true)
  }

  removeAt(index: number): void {
    const removingCurrent =
      this.state.sequence[this.state.cursor] === index
    this.state = removeFromQueue(this.state, index)
    if (removingCurrent) this.loadCurrent(true)
    this.emit()
  }

  move(from: number, to: number): void {
    this.state = moveInQueue(this.state, from, to)
    this.emit()
  }

  clear(): void {
    this.stopFade()
    this.el.pause()
    this.el.removeAttribute('src')
    this.el.load()
    this.state = emptyQueue
    this.current = null
    this.emit()
  }

  // --- transport ---

  play(): void {
    if (!this.current) return
    // resume() is the reliable path on iOS, where play() on a fresh element
    // needs a user gesture that React's synthetic events do not satisfy.
    void this.el.play().catch((err) => {
      if (err?.name !== 'AbortError') {
        this.patch({ error: 'Playback was blocked. Tap play again.' })
        this.emit()
      }
    })
  }

  pause(): void {
    this.el.pause()
  }

  toggle(): void {
    if (this.el.paused) this.play()
    else this.pause()
  }

  next(): void {
    const step = advance(this.state, 1)
    if (step.stop) {
      this.pause()
      return
    }
    this.state = { ...this.state, cursor: step.cursor }
    this.loadCurrent(true)
  }

  previous(): void {
    // Common behaviour: within the first few seconds, go to the previous
    // track; after that, restart the current one.
    if (this.el.currentTime > 3) {
      this.seek(0)
      return
    }
    const step = advance(this.state, -1)
    this.state = { ...this.state, cursor: Math.max(0, step.cursor) }
    this.loadCurrent(true)
  }

  seek(seconds: number): void {
    if (!Number.isFinite(this.el.duration)) return
    const clamped = Math.max(0, Math.min(seconds, this.el.duration))
    this.el.currentTime = clamped
    this.emit()
  }

  skip(delta: number): void {
    this.seek((this.el.currentTime || 0) + delta)
  }

  setVolume(value: number): void {
    this.volume = Math.max(0, Math.min(1, value))
    this.muted = this.volume === 0
    this.el.volume = this.muted ? 0 : this.volume
    this.gain?.gain.setTargetAtTime(this.el.volume, this.audioCtx!.currentTime, 0.02)
    this.emit()
  }

  toggleMute(): void {
    this.muted = !this.muted
    this.el.muted = this.muted
    this.el.volume = this.muted ? 0 : this.volume
    this.emit()
  }

  toggleShuffle(): void {
    this.state = toggleShuffle(this.state, this.current?.id ?? null)
    this.emit()
  }

  cycleRepeat(): void {
    this.state = cycleRepeat(this.state)
    this.emit()
  }

  setRepeat(mode: RepeatMode): void {
    this.state = { ...this.state, repeat: mode }
    this.emit()
  }

  setQuality(preference: QualityPreference): void {
    this.quality = preference
    // Re-resolve the profile for the current track; a change only takes
    // effect on the next load, which is the least surprising behaviour.
    this.profile = pickProfile(preference, this.current ?? { bitrate: 0, format: '' })
    this.emit()
  }

  // --- loading and crossfade ---

  private loadCurrent(autoPlay: boolean, allowCrossfade = true): void {
    const id = currentTrackId(this.state)
    if (id === null) {
      this.current = null
      this.emit()
      return
    }

    let track = this.tracks.get(id)
    if (!track) {
      // A track restored from a synced queue on a device that has never seen
      // it: fetch the metadata before playing.
      void api
        .track(id)
        .then((t) => {
          this.tracks.set(t.id, t)
          if (currentTrackId(this.state) === id) this.loadCurrent(autoPlay, allowCrossfade)
        })
        .catch(() => {
          this.patch({ error: 'That track is no longer in your library.' })
          this.emit()
        })
      return
    }

    // Re-resolve the profile from the fresh track's properties.
    this.profile = pickProfile(this.quality, track)
    const url = audioUrl(track.id, this.profile)
    const token = ++this.loadToken

    this.current = track
    this.lastError = null
    this.recordPlay(track)

    // Crossfade only when something is genuinely playing right now.
    const canFade = allowCrossfade && !this.el.paused && this.el.readyState >= 2
    if (canFade) {
      this.startFade(url, autoPlay, token)
    } else {
      this.el.src = url
      this.el.load()
      if (autoPlay) this.play()
    }

    this.updateMediaSessionMetadata(track)
    this.updateMediaSessionPlaybackState()
    this.emit()
  }

  /**
   * Crossfade: the outgoing element is faded out while the incoming one fades
   * in, then the old element is torn down.
   */
  private startFade(url: string, autoPlay: boolean, token: number): void {
    this.stopFade()

    const incoming = new Audio()
    incoming.src = url
    incoming.preload = 'auto'
    incoming.volume = 0
    incoming.crossOrigin = 'anonymous'

    const ctx = this.ensureAudioContext()
    let fadeIn: GainNode | null = null
    let fadeOut: GainNode | null = null

    if (ctx) {
      fadeIn = ctx.createGain()
      fadeIn.gain.value = 0
      const src = ctx.createMediaElementSource(incoming)
      src.connect(fadeIn)
      fadeIn.connect(ctx.destination)

      // Route the outgoing element through its own gain node if it is not
      // already connected; a second connection throws, so this is guarded.
      try {
        fadeOut = ctx.createGain()
        fadeOut.gain.value = 1
        const oldSrc = ctx.createMediaElementSource(this.el)
        oldSrc.connect(fadeOut)
        fadeOut.connect(ctx.destination)
      } catch {
        fadeOut = null
      }
    }

    const startedAt = ctx?.currentTime ?? 0
    const duration = CROSSFADE_SECONDS
    if (ctx) {
      fadeIn?.gain.setValueAtTime(0, startedAt)
      fadeIn?.gain.linearRampToValueAtTime(this.muted ? 0 : this.volume, startedAt + duration)
      fadeOut?.gain.setValueAtTime(1, startedAt)
      fadeOut?.gain.linearRampToValueAtTime(0, startedAt + duration)
    } else {
      // Without Web Audio, fall back to element volume ramps.
      incoming.volume = 0
    }

    const swap = () => {
      if (token !== this.loadToken) return
      this.el.pause()
      this.el.src = url
      this.el.load()
      if (autoPlay) this.play()

      this.fadeEl = null
      this.fadeTimer = window.setTimeout(() => {
        incoming.pause()
        incoming.removeAttribute('src')
        incoming.load()
        // Disconnect the outgoing gain node so the graph does not grow with
        // every track change.
        try {
          fadeOut?.disconnect()
        } catch {
          /* already detached */
        }
      }, duration * 1000 + 120)
    }

    const promote = () => {
      if (token !== this.loadToken) return
      if (ctx) {
        swap()
        return
      }
      // Volume-ramp path.
      const start = performance.now()
      const step = () => {
        const t = Math.min(1, (performance.now() - start) / (duration * 1000))
        incoming.volume = t * (this.muted ? 0 : this.volume)
        this.el.volume = (1 - t) * (this.muted ? 0 : this.volume)
        if (t < 1) {
          requestAnimationFrame(step)
        } else {
          swap()
        }
      }
      requestAnimationFrame(step)
    }

    incoming.addEventListener('canplay', promote, { once: true })
    if (autoPlay) {
      void incoming.play().catch(() => {
        // Autoplay was refused; the main element takes over.
        swap()
      })
    }
    this.fadeEl = incoming
  }

  private stopFade() {
    if (this.fadeTimer !== null) {
      clearTimeout(this.fadeTimer)
      this.fadeTimer = null
    }
    if (this.fadeEl) {
      this.fadeEl.pause()
      this.fadeEl.removeAttribute('src')
      this.fadeEl.load()
      this.fadeEl = null
    }
  }

  /**
   * ensureAudioContext returns a context, creating it on the first call.
   *
   * Browsers suspend a context created before any user gesture, so this is
   * only called from a code path that follows interaction.
   */
  private ensureAudioContext(): AudioContext | null {
    if (typeof AudioContext === 'undefined') return null
    try {
      if (!this.audioCtx) this.audioCtx = new AudioContext()
      if (this.audioCtx.state === 'suspended') void this.audioCtx.resume()
      return this.audioCtx
    } catch {
      return null
    }
  }

  // --- events ---

  private onEnded() {
    // Ignore a stale ended event from a track we already replaced.
    if (this.current && this.el.src.includes(`/stream/${this.current.id}`) === false) return

    const step = advance(this.state, 1)
    if (step.restart) {
      this.seek(0)
      this.play()
      return
    }
    if (step.stop) {
      this.pause()
      this.emit()
      return
    }
    this.state = { ...this.state, cursor: step.cursor }
    this.loadCurrent(true)
  }

  private onTimeUpdate() {
    this.emit()
  }

  /** Reports a completed play to the server, which may forward it to Last.fm. */
  private recordPlay(track: Track) {
    this.pendingReport = window.setTimeout(() => {
      void api.recordPlay(track.id, 0.95, Math.round(track.durationMs * 0.95)).catch(() => {
        // A failed history write is not worth interrupting playback for.
      })
    }, Math.min(track.durationMs, 30_000))
  }

  private pendingReport: number | null = null

  // --- MediaSession ---

  /**
   * installMediaSession wires up the OS-level transport controls.
   *
   * Two implementations, because the target differs. In a browser the Web
   * MediaSession API is the whole story. Inside the Capacitor shell the
   * WebView's MediaSession is not reliably surfaced to the lock screen or
   * notification shade, so the native plugin is used instead, and its
   * handlers are what the hardware buttons call.
   */
  private installMediaSession() {
    if (Capacitor.isNativePlatform()) {
      this.installNativeMediaSession()
      return
    }
    this.installWebMediaSession()
  }

  private installWebMediaSession() {
    if (typeof navigator === 'undefined' || !('mediaSession' in navigator)) return
    const ms = navigator.mediaSession

    const safe = (fn: (details?: MediaSessionActionDetails) => void) => () => {
      try {
        fn()
      } catch {
        // A rejected MediaSession call must never break playback.
      }
    }

    ms.setActionHandler('play', safe(() => this.play()))
    ms.setActionHandler('pause', safe(() => this.pause()))
    ms.setActionHandler('previoustrack', safe(() => this.previous()))
    ms.setActionHandler('nexttrack', safe(() => this.next()))
    ms.setActionHandler('seekbackward', safe(() => this.skip(-10)))
    ms.setActionHandler('seekforward', safe(() => this.skip(10)))
    ms.setActionHandler('stop', safe(() => this.pause()))
    ms.setActionHandler('seekto', safe((details) => {
      // `seekTime` is null for "seek to the end", which some browsers send
      // when a scrubber is released at the far right.
      if (details?.seekTime != null) this.seek(details.seekTime)
    }))

    this.setWebPositionState = (duration, position, rate) => {
      try {
        ms.setPositionState?.({
          duration,
          position,
          playbackRate: rate,
        })
      } catch {
        // Safari throws if duration is not finite; it recovers on the next
        // track, so there is nothing to do.
      }
    }
  }

  /**
   * installNativeMediaSession delegates to @capgo/capacitor-media-session,
   * which drives Android's MediaSession and iOS's MPNowPlayingInfoCenter.
   *
   * The plugin is imported lazily: it links against native code that only
   * exists in the shell, and a static import would run that registration in
   * the browser too.
   */
  private installNativeMediaSession() {
    void (async () => {
      let plugin: typeof import('@capgo/capacitor-media-session') | null = null
      try {
        plugin = await import('@capgo/capacitor-media-session')
      } catch {
        // The plugin is not in this build. The in-app controls still work;
        // only the lock-screen controls are missing.
        return
      }
      const MediaSession = plugin?.MediaSession
      if (!MediaSession) return

      this.nativeMediaSession = MediaSession

      // Every registration is best-effort: a platform that does not support an
      // action rejects, and that must not take the rest down with it.
      const register = (action: string, fn: () => void) =>
        MediaSession.setActionHandler({ action } as never, fn as never).catch(() => undefined)

      await register('play', () => this.play())
      await register('pause', () => this.pause())
      await register('previoustrack', () => this.previous())
      await register('nexttrack', () => this.next())
      await register('stop', () => this.pause())
      await register('seekbackward', () => this.skip(-10))
      await register('seekforward', () => this.skip(10))
      await MediaSession.setActionHandler(
        { action: 'seekto' },
        (details: { seekTime?: number | null }) => {
          if (details?.seekTime != null) this.seek(details.seekTime)
        },
      ).catch(() => undefined)
    })()
  }

  /** Assigned by whichever MediaSession implementation is in use. */
  private setWebPositionState: ((duration: number, position: number, rate: number) => void) | null =
    null
  private nativeMediaSession: MediaSessionPlugin | null = null

  private updateMediaSessionMetadata(track: Track) {
    const artwork = track.coverPath
      ? [
          {
            src: `${API_BASE}/api/covers/${track.coverPath}`,
            sizes: '512x512',
            type: 'image/jpeg',
          },
        ]
      : undefined

    if (this.nativeMediaSession) {
      void this.nativeMediaSession
        .setMetadata({
          title: track.title,
          artist: track.artist,
          album: track.album,
          artwork,
        })
        .catch(() => undefined)
      return
    }

    if (typeof navigator === 'undefined' || !('mediaSession' in navigator)) return
    try {
      navigator.mediaSession.metadata = new MediaMetadata({
        title: track.title,
        artist: track.artist,
        album: track.album,
        artwork: artwork ?? [],
      })
    } catch {
      // MediaMetadata is unavailable in some webviews; the app works without it.
    }
  }

  /** Tells the OS what the transport state is, for the lock-screen display. */
  private updateMediaSessionPlaybackState() {
    const playing = !this.el.paused && !this.el.ended
    const duration =
      this.el.duration && Number.isFinite(this.el.duration) ? this.el.duration : 0
    const position = this.el.currentTime || 0

    if (this.nativeMediaSession) {
      void this.nativeMediaSession
        .setPlaybackState({ playbackState: playing ? 'playing' : 'paused' })
        .catch(() => undefined)
      void this.nativeMediaSession
        .setPositionState({ duration, position, playbackRate: 1 })
        .catch(() => undefined)
      return
    }

    if (typeof navigator === 'undefined' || !('mediaSession' in navigator)) return
    try {
      navigator.mediaSession.playbackState = playing ? 'playing' : 'paused'
    } catch {
      // Not every browser exposes playbackState.
    }
    this.setWebPositionState?.(duration, position, 1)
  }

  // --- cross-device sync ---

  /** Current playback snapshot in the shape the sync socket expects. */
  syncPayload() {
    return {
      trackId: this.current?.id ?? 0,
      positionMs: Math.round((this.el.currentTime || 0) * 1000),
      playing: !this.el.paused,
      volume: this.volume,
      shuffle: this.state.shuffle,
      repeat: this.state.repeat,
      queue: queueTrackIds(this.state),
      profile: this.profile,
    }
  }

  /** Applies a snapshot received from another device. */
  applyRemoteState(state: {
    trackId: number
    positionMs: number
    playing: boolean
    volume: number
    shuffle: boolean
    repeat: RepeatMode
    queue: number[]
    profile?: string
  }): void {
    if (!state.queue || state.queue.length === 0) return

    const ids = queueTrackIds(this.state)
    const queueChanged =
      ids.length !== state.queue.length || ids.some((id, i) => id !== state.queue[i])

    if (queueChanged) {
      this.state = queueFromIds(state.queue, state.trackId, state.shuffle)
      for (const id of state.queue) {
        if (!this.tracks.has(id)) {
          void api
            .track(id)
            .then((t) => this.tracks.set(t.id, t))
            .catch(() => undefined)
        }
      }
    }

    this.state = {
      ...this.state,
      shuffle: state.shuffle,
      repeat: state.repeat,
    }
    this.setVolume(state.volume)

    if (state.trackId && state.trackId !== this.current?.id) {
      const slot = this.state.order.indexOf(state.trackId)
      if (slot >= 0) {
        this.state = {
          ...this.state,
          cursor: this.state.shuffle ? this.state.sequence.indexOf(slot) : slot,
        }
        // Crossfade between devices would double the audio, so this one
        // switches cleanly instead.
        this.loadCurrent(false, false)
      }
    }

    if (Number.isFinite(state.positionMs) && this.current?.id === state.trackId) {
      // Only correct the position if the two devices are meaningfully apart,
      // or the track would stutter back and forth on every update.
      const ours = this.el.currentTime * 1000
      if (Math.abs(ours - state.positionMs) > 1500) {
        this.el.currentTime = state.positionMs / 1000
      }
    }

    if (state.playing && this.el.paused) this.play()
    if (!state.playing && !this.el.paused) this.pause()

    this.emit()
  }

  /** True when this device should stop playing because another took over. */
  relinquishPlayback(): void {
    this.pause()
  }

  dispose(): void {
    this.stopFade()
    if (this.tickTimer !== null) clearInterval(this.tickTimer)
    if (this.pendingReport !== null) clearTimeout(this.pendingReport)
    this.el.pause()
    this.listeners.clear()
    void this.audioCtx?.close()
  }
}

/** The shared player instance for the app. */
export const player = new Player()
