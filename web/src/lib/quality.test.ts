import { describe, expect, it } from 'vitest'
import { pickProfile, type ConnectionInfo, type PlaybackCapabilities } from './quality'

const desktop: PlaybackCapabilities = { supportsOpus: true, isMobile: false }
const iphone: PlaybackCapabilities = { supportsOpus: false, isMobile: true }
const android: PlaybackCapabilities = { supportsOpus: true, isMobile: true }

const fast: ConnectionInfo = { effectiveType: '4g', downlink: 10, saveData: false }
const slow: ConnectionInfo = { effectiveType: '3g', downlink: 0.7, saveData: false }
const unknown: ConnectionInfo = {}

/** An empty string means lossless passthrough. */
const LOSSLESS = ''

describe('pickProfile', () => {
  describe('explicit preferences always win', () => {
    it('lossless ignores a terrible connection', () => {
      // The listener asked for it explicitly.
      expect(pickProfile('lossless', { bitrate: 1400, format: 'flac' }, slow, iphone)).toBe(LOSSLESS)
    })

    it('data-saver transcodes even on a fast connection', () => {
      expect(pickProfile('data-saver', { bitrate: 1400, format: 'flac' }, fast, desktop)).toBe('aac96')
    })

    it('high prefers Opus where it exists', () => {
      expect(pickProfile('high', { bitrate: 900, format: 'flac' }, fast, desktop)).toBe('opus64')
    })

    it('high falls back to AAC where Opus is unsupported', () => {
      expect(pickProfile('high', { bitrate: 900, format: 'flac' }, fast, iphone)).toBe('aac320')
    })
  })

  describe('automatic', () => {
    it('leaves an ordinary MP3 alone', () => {
      // Re-encoding a 192kbps MP3 to 96kbps would be a pure quality loss.
      expect(pickProfile('auto', { bitrate: 192, format: 'mp3' }, fast, desktop)).toBe(LOSSLESS)
    })

    it('leaves a small lossless file alone on a fast connection', () => {
      expect(pickProfile('auto', { bitrate: 700, format: 'flac' }, fast, desktop)).toBe(LOSSLESS)
    })

    it('always transcodes a WAV, even on a fast desktop', () => {
      // A WAV is ~10x the size of the same music in FLAC. No browser has a
      // good reason to receive one over the wire.
      expect(pickProfile('auto', { bitrate: 0, format: 'wav' }, fast, desktop)).not.toBe(LOSSLESS)
      expect(pickProfile('auto', { bitrate: 0, format: 'wav' }, unknown, desktop)).not.toBe(LOSSLESS)
    })

    it('keeps a high bitrate FLAC on a genuinely fast desktop link', () => {
      expect(pickProfile('auto', { bitrate: 1400, format: 'flac' }, fast, desktop)).toBe(LOSSLESS)
    })

    it('transcodes a high bitrate FLAC on a phone even on wifi', () => {
      // Mobile clients roam between networks unpredictably, so a phone is
      // given the smaller option regardless of the reported connection.
      expect(pickProfile('auto', { bitrate: 1400, format: 'flac' }, fast, iphone)).toBe('aac96')
      expect(pickProfile('auto', { bitrate: 1400, format: 'flac' }, fast, android)).toBe('aac96')
    })

    it('transcodes a high bitrate FLAC on a desktop with a weak link', () => {
      const weak: ConnectionInfo = { effectiveType: '4g', downlink: 2 }
      expect(pickProfile('auto', { bitrate: 1400, format: 'flac' }, weak, desktop)).toBe('aac96')
    })

    it('honours save-data even on a fast connection', () => {
      const saveData: ConnectionInfo = { effectiveType: '4g', downlink: 20, saveData: true }
      expect(pickProfile('auto', { bitrate: 1400, format: 'flac' }, saveData, desktop)).toBe('aac96')
    })

    it('transcodes a high bitrate FLAC on a desktop with an unknown connection', () => {
      // Not knowing the link is not a reason to send 90MB. Only a link we can
      // actually verify as fast earns lossless for a huge file.
      expect(pickProfile('auto', { bitrate: 1400, format: 'flac' }, unknown, desktop)).toBe('aac96')
    })

    it('sends a mid-size FLAC lossless on a desktop with an unknown connection', () => {
      // A 700kbps FLAC is a few MB; there is nothing to gain by transcoding
      // it whatever the connection.
      expect(pickProfile('auto', { bitrate: 700, format: 'flac' }, unknown, desktop)).toBe(LOSSLESS)
    })

    it('uses Opus on a slow connection where it is supported', () => {
      expect(pickProfile('auto', { bitrate: 1400, format: 'flac' }, slow, android)).toBe('opus64')
    })

    it('uses AAC on a slow connection without Opus', () => {
      expect(pickProfile('auto', { bitrate: 1400, format: 'flac' }, slow, iphone)).toBe('aac96')
    })

    it('transcodes on a phone with an unknown connection', () => {
      expect(pickProfile('auto', { bitrate: 1400, format: 'flac' }, unknown, iphone)).toBe('aac96')
    })

    it('transcodes when downlink is low even if effectiveType looks fine', () => {
      const mid: ConnectionInfo = { effectiveType: '4g', downlink: 1 }
      expect(pickProfile('auto', { bitrate: 1400, format: 'flac' }, mid, desktop)).toBe('aac96')
    })
  })

  it('always returns a profile the server knows', () => {
    // These names are the contract with the Go side; a typo here would
    // silently fall back to aac96 and hide the mistake.
    const known = new Set(['', 'aac96', 'opus64', 'aac320'])
    const cases: [Parameters<typeof pickProfile>[0], { bitrate: number; format: string }][] = [
      ['auto', { bitrate: 1400, format: 'flac' }],
      ['auto', { bitrate: 192, format: 'mp3' }],
      ['high', { bitrate: 900, format: 'flac' }],
      ['standard', { bitrate: 900, format: 'flac' }],
      ['data-saver', { bitrate: 900, format: 'flac' }],
      ['lossless', { bitrate: 900, format: 'flac' }],
    ]
    for (const [pref, track] of cases) {
      for (const conn of [fast, slow, unknown]) {
        for (const caps of [desktop, iphone, android]) {
          expect(known.has(pickProfile(pref, track, conn, caps))).toBe(true)
        }
      }
    }
  })
})
