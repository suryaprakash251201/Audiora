/**
 * Cross-device playback sync client.
 *
 * Opens a WebSocket, sends a hello frame, then relays state. Two behaviours
 * are worth calling out:
 *
 *  - The server elects a single controller. When another device takes over,
 *  it sends a "replaced" frame and this client pauses, so two devices never
 *  play the same album at once out of opposite speakers.
 *  - Sequence numbers guard against out-of-order delivery. A state frame
 *  older than one already applied is dropped.
 */

import { syncSocketUrl, type SyncState } from './api'
import type { Player } from './player'

/** A stable per-device id, so a reconnect is recognised as the same device. */
function deviceId(): string {
  const KEY = 'audiora.deviceId'
  let id = localStorage.getItem(KEY)
  if (!id) {
    id = `dev-${Math.random().toString(36).slice(2, 10)}-${Date.now().toString(36)}`
    localStorage.setItem(KEY, id)
  }
  return id
}

function deviceName(): string {
  const ua = navigator.userAgent
  if (/iphone|ipad|ipod/i.test(ua)) return 'iPhone'
  if (/android/i.test(ua)) return 'Android device'
  if (/mac os/i.test(ua)) return 'Mac'
  if (/windows/i.test(ua)) return 'Windows PC'
  if (/linux/i.test(ua)) return 'Linux'
  return 'Browser'
}

interface Envelope {
  type: string
  seq: number
  from: string
  data?: unknown
}

export class SyncClient {
  private socket: WebSocket | null = null
  private player: Player
  private lastSeq = 0
  private reconnectDelay = 1000
  private reconnectTimer: number | null = null
  private publishTimer: number | null = null
  private closed = false
  private device = deviceId()
  /** Set when this device explicitly started playback, so it can claim control. */
  private wantsControl = false

  constructor(player: Player) {
    this.player = player
  }

  connect(): void {
    this.closed = false
    this.open()

    // Publishing on every timeupdate would flood the socket. Position is
    // broadcast separately and far less often than state changes.
    this.player.subscribe(() => this.schedulePublish())
  }

  private open(): void {
    if (this.closed) return

    let socket: WebSocket
    try {
      socket = new WebSocket(syncSocketUrl(this.device))
    } catch {
      this.scheduleReconnect()
      return
    }
    this.socket = socket

    socket.addEventListener('open', () => {
      this.reconnectDelay = 1000
      socket.send(
        JSON.stringify({
          type: 'hello',
          from: this.device,
          device: deviceName(),
          claim: this.wantsControl,
        }),
      )
      // Announce our current state so a joining device syncs immediately.
      this.publish()
    })

    socket.addEventListener('message', (event) => {
      let envelope: Envelope
      try {
        envelope = JSON.parse(event.data as string) as Envelope
      } catch {
        return
      }
      this.handle(envelope)
    })

    socket.addEventListener('close', () => {
      this.socket = null
      this.scheduleReconnect()
    })

    socket.addEventListener('error', () => {
      // The close handler does the reconnect; nothing extra to do here.
    })
  }

  private handle(envelope: Envelope): void {
    switch (envelope.type) {
      case 'hello': {
        const payload = envelope.data as { state?: SyncState; controller?: string }
        if (payload?.state && envelope.seq > this.lastSeq) {
          this.lastSeq = envelope.seq
          // Adopt the other device's state only if it is actually playing;
          // otherwise the other device is just mirroring us.
          if (payload.state.playing) this.player.applyRemoteState(payload.state)
        }
        break
      }

      case 'state': {
        if (envelope.from === this.device) return
        if (envelope.seq <= this.lastSeq) return
        this.lastSeq = envelope.seq
        const state = envelope.data as SyncState
        if (state) this.player.applyRemoteState(state)
        break
      }

      case 'replaced': {
        // Another device took over. Stop, but keep the socket.
        this.wantsControl = false
        this.player.relinquishPlayback()
        break
      }

      case 'events': {
        // Reserved for server-side notices such as scan progress.
        break
      }
    }
  }

  /** Called when this device starts playback, so it becomes the controller. */
  claimControl(): void {
    this.wantsControl = true
    this.publish()
  }

  private schedulePublish(): void {
    if (this.publishTimer !== null) return
    this.publishTimer = window.setTimeout(() => {
      this.publishTimer = null
      this.publish()
    }, 700)
  }

  private publish(): void {
    if (this.socket?.readyState !== WebSocket.OPEN) return
    this.socket.send(
      JSON.stringify({
        type: 'state',
        from: this.device,
        data: this.player.syncPayload(),
      }),
    )
  }

  private scheduleReconnect(): void {
    if (this.closed || this.reconnectTimer !== null) return
    // Exponential backoff, capped, so a server restart does not produce a
    // reconnect storm from every open tab.
    const delay = Math.min(this.reconnectDelay, 30_000)
    this.reconnectTimer = window.setTimeout(() => {
      this.reconnectTimer = null
      this.reconnectDelay = Math.min(this.reconnectDelay * 2, 30_000)
      this.open()
    }, delay)
  }

  disconnect(): void {
    this.closed = true
    if (this.reconnectTimer !== null) {
      clearTimeout(this.reconnectTimer)
      this.reconnectTimer = null
    }
    if (this.publishTimer !== null) {
      clearTimeout(this.publishTimer)
      this.publishTimer = null
    }
    this.socket?.close()
    this.socket = null
  }
}
