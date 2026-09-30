/**
 * Chooses which audio profile to request for the current connection.
 *
 * The point is bandwidth. A 40MB FLAC is fine on wifi and ruinous on a phone's
 * mobile data, so the profile adapts to what the Network Information API
 * reports, what the device can actually decode, and whether the user asked
 * for something specific.
 */

export type QualityPreference = 'auto' | 'lossless' | 'high' | 'standard' | 'data-saver'

/**
 * WAV is the case that matters most: it is effectively uncompressed, often
 * 40MB for four minutes. No client should pull that over a metered link.
 */
const HUGE_BITRATE_THRESHOLD_KBPS = 1200

export interface ConnectionInfo {
  effectiveType?: string
  saveData?: boolean
  downlink?: number
}

export interface PlaybackCapabilities {
  /** Can the UA decode Opus? Safari and iOS cannot. */
  supportsOpus: boolean
  /** True inside the Capacitor shell, where we know it is a phone or tablet. */
  isMobile: boolean
}

/** A crude but effective form-factor sniff. */
export function detectCapabilities(): PlaybackCapabilities {
  if (typeof navigator === 'undefined') {
    return { supportsOpus: false, isMobile: false }
  }
  const audio = document.createElement('audio')
  return {
    supportsOpus:
      audio.canPlayType('audio/ogg; codecs="opus"') !== '' ||
      audio.canPlayType('audio/webm; codecs="opus"') !== '',
    isMobile: /android|iphone|ipad|ipod|mobile/i.test(navigator.userAgent),
  }
}

export function readConnection(): ConnectionInfo {
  if (typeof navigator === 'undefined') return {}
  // Effective types are ordered slowest-first by the spec: 'slow-2g' is worst.
  const c = (navigator as Navigator & { connection?: ConnectionInfo }).connection
  if (!c) return {}
  return {
    effectiveType: c.effectiveType,
    saveData: c.saveData,
    downlink: c.downlink,
  }
}

/**
 * pickProfile returns the profile name to put in the stream URL.
 *
 * Returns the empty string for lossless, which the server reads as "serve the
 * original file untouched".
 */
export function pickProfile(
  preference: QualityPreference,
  track: { bitrate: number; format: string },
  connection: ConnectionInfo = readConnection(),
  capabilities: PlaybackCapabilities = detectCapabilities(),
): string {
  // An explicit user choice always wins.
  if (preference === 'lossless') return ''
  if (preference === 'high') return capabilities.supportsOpus ? 'opus64' : 'aac320'
  if (preference === 'standard') return 'aac96'
  if (preference === 'data-saver') return 'aac96'

  // A user on data-saver or a bad connection gets the smallest option.
  if (connection.saveData) return 'aac96'

  const slowTypes = ['slow-2g', '2g', '3g']
  const isSlow = connection.effectiveType ? slowTypes.includes(connection.effectiveType) : false
  // Opus at 64 is kinder than AAC at 96 wherever the browser supports it.
  const efficient = isSlow && capabilities.supportsOpus ? 'opus64' : 'aac96'

  const isUncompressed = ['wav', 'pcm_s16le', 'pcm_s24le', 'pcm_s32le'].includes(
    track.format.toLowerCase(),
  )
  const isHuge = track.bitrate >= HUGE_BITRATE_THRESHOLD_KBPS

  if (isSlow || (connection.downlink !== undefined && connection.downlink < 1.5)) {
    return efficient
  }

  // An uncompressed WAV is roughly ten times the size of the same music in
  // FLAC and there is no good reason for a browser to receive one over the
  // wire, however fast the link is. Transcode it in automatic mode.
  if (isUncompressed) {
    return efficient
  }

  // An ordinary or mid-size source is not worth re-encoding: it would be a
  // pure quality loss for no meaningful size benefit.
  if (!isHuge) return ''

  // A high-bitrate lossless file is worth keeping, but only on a link that
  // can carry it. On a phone, prefer the smaller option even on wifi, since
  // mobile clients roam between networks unpredictably.
  if (capabilities.isMobile) return efficient
  if (connection.effectiveType === '4g' && (connection.downlink ?? 0) >= 4) return ''

  return efficient
}

/** Human-readable label for the quality picker. */
export function describeProfile(profile: string): string {
  switch (profile) {
    case '':
    case 'lossless':
      return 'Lossless'
    case 'aac320':
      return 'High (320 kbps)'
    case 'aac96':
      return 'Standard (96 kbps)'
    case 'opus64':
      return 'Efficient (64 kbps Opus)'
    default:
      return profile
  }
}
