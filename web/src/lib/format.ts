/** Formatting helpers shared across the UI. */

export function formatDuration(ms: number): string {
  if (!ms || ms < 0) return '0:00'
  const total = Math.floor(ms / 1000)
  const hours = Math.floor(total / 3600)
  const minutes = Math.floor((total % 3600) / 60)
  const seconds = total % 60

  if (hours > 0) {
    return `${hours}:${String(minutes).padStart(2, '0')}:${String(seconds).padStart(2, '0')}`
  }
  return `${minutes}:${String(seconds).padStart(2, '0')}`
}

/** A long runtime reads better as "4 hr 12 min" than as milliseconds. */
export function formatLongDuration(ms: number): string {
  const totalMinutes = Math.floor(ms / 60_000)
  const hours = Math.floor(totalMinutes / 60)
  const minutes = totalMinutes % 60
  if (hours === 0) return `${minutes} min`
  if (minutes === 0) return `${hours} hr`
  return `${hours} hr ${minutes} min`
}

export function formatBytes(bytes: number): string {
  if (!bytes || bytes < 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let value = bytes
  let unit = 0
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024
    unit++
  }
  return `${value.toFixed(value < 10 && unit > 0 ? 1 : 0)} ${units[unit]}`
}

export function formatCount(n: number, singular: string, plural = `${singular}s`): string {
  return `${n.toLocaleString()} ${n === 1 ? singular : plural}`
}

/**
 * Converts a #rrggbb colour to an OKLCH string, so artwork-derived accents
 * are perceptually consistent with the hand-picked theme colours.
 *
 * Kept dependency-free: this runs on every album card, and pulling in a colour
 * library for a single conversion would not be worth the bundle.
 */
export function hexToOklch(hex: string, lightnessShift = 0): string {
  const clean = hex.replace('#', '')
  if (clean.length !== 6) return 'oklch(0.72 0.17 305)'

  const r = parseInt(clean.slice(0, 2), 16) / 255
  const g = parseInt(clean.slice(2, 4), 16) / 255
  const b = parseInt(clean.slice(4, 6), 16) / 255

  // sRGB to linear, then to OKLab.
  const lin = (c: number) => (c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4)
  const lr = lin(r)
  const lg = lin(g)
  const lb = lin(b)

  const l = Math.cbrt(0.4122214708 * lr + 0.5363325363 * lg + 0.0514459929 * lb)
  const m = Math.cbrt(0.2119034982 * lr + 0.6806995451 * lg + 0.1073969566 * lb)
  const s = Math.cbrt(0.0883024619 * lr + 0.2817188376 * lg + 0.6299787005 * lb)

  const L = 0.2104542553 * l + 0.793617785 * m - 0.0040720468 * s
  const A = 1.9779984951 * l - 2.428592205 * m + 0.4505937099 * s
  const B = 0.0259040371 * l + 0.7827717662 * m - 0.808675766 * s

  const chroma = Math.sqrt(A * A + B * B)
  const hue = ((Math.atan2(B, A) * 180) / Math.PI + 360) % 360

  const adjustedL = Math.max(0, Math.min(1, L + lightnessShift))
  return `oklch(${adjustedL.toFixed(3)} ${chroma.toFixed(3)} ${hue.toFixed(1)})`
}

/**
 * A deterministic gradient for albums with no artwork, derived from the id so
 * the same album always looks the same.
 */
export function placeholderGradient(seed: string | number): string {
  let hash = 0
  const str = String(seed)
  for (let i = 0; i < str.length; i++) {
    hash = (hash << 5) - hash + str.charCodeAt(i)
    hash |= 0
  }
  const hue = Math.abs(hash) % 360
  return `linear-gradient(135deg, oklch(0.45 0.12 ${hue}) 0%, oklch(0.28 0.09 ${(hue + 55) % 360}) 100%)`
}
