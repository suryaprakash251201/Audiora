/**
 * LRC lyric parsing.
 *
 * Timed LRC is the format Audiora reads: the scanner picks up sidecar .lrc
 * files and embedded lyric tags, stores them raw, and this module turns them
 * into something the lyrics pane can scroll. Unsynchronised plain text is
 * accepted too and rendered without timings.
 *
 * The parser is strict about the parts that are ambiguous and lenient about
 * the parts that vary wildly between taggers, because a slightly wrong
 * timestamp is far less annoying than a missing lyric.
 */

export interface LyricLine {
  /** Milliseconds from the start of the track. */
  time: number
  text: string
}

export interface Lyrics {
  lines: LyricLine[]
  /** True when at least one line carried a timestamp. */
  synced: boolean
  /** Optional metadata tags found in the header. */
  meta: Record<string, string>
}

/** Matches one or more [mm:ss.xx] or [mm:ss] or [mm:ss.xxx] stamps. */
const TIME_TAG = /\[(\d{1,3}):(\d{1,2})(?:[.:](\d{1,3}))?\]/g

const META_TAG = /^\[([a-z]+):\s*(.*?)\s*\]$/i

/**
 * Recognises a timestamp tag that failed validation, so it can be discarded
 * rather than mistaken for lyric text. Deliberately loose about the values:
 * this only needs to tell "[00:99.00]" apart from "[ar:Artist]".
 */
const MALFORMED_STAMP = /\[\d+:\d+(?:[.:]\d+)?\]/

/**
 * Parses a lyric document. Returns null when there is nothing to show, so
 * callers can treat "no lyrics" as an ordinary state.
 */
export function parseLRC(raw: string): Lyrics | null {
  if (!raw || !raw.trim()) return null

  const lines: LyricLine[] = []
  const meta: Record<string, string> = {}
  let sawTimestamp = false

  for (const rawLine of raw.split(/\r\n|\r|\n/)) {
    const line = rawLine.trim()
    if (!line) continue

    // Collect every timestamp on this line. Some taggers write two stamps for
    // a repeated phrase, which is how it should be rendered.
    TIME_TAG.lastIndex = 0
    const stamps: number[] = []
    let match: RegExpExecArray | null
    let lastEnd = 0

    while ((match = TIME_TAG.exec(line)) !== null) {
      const minutes = Number(match[1])
      const seconds = Number(match[2])
      // The fraction is either hundredths or thousandths depending on the
      // number of digits, which is the whole reason this is not a plain parse.
      const fracDigits = match[3]?.length ?? 0
      const fraction = match[3] ? Number(match[3]) / 10 ** fracDigits : 0

      if (seconds < 60 && minutes < 600) {
        stamps.push(Math.round((minutes * 60 + seconds + fraction) * 1000))
      }
      lastEnd = match.index + match[0].length
    }

    if (stamps.length > 0) {
      sawTimestamp = true
      // The text is whatever follows the last timestamp on the line.
      const text = line.slice(lastEnd).trim()
      for (const time of stamps) {
        lines.push({ time, text })
      }
      continue
    }

    // A line that looks like it was meant to carry a timestamp but has an
    // impossible value ("[00:99.00]") is dropped. Keeping it would mix an
    // unsynchronised line into an otherwise timed sheet and the client would
    // render it at position zero, which reads as a bug.
    if (looksLikeTimestamp(line)) {
      continue
    }

    // A bare [key: value] line before any timestamp is metadata.
    const metaMatch = line.match(META_TAG)
    if (metaMatch) {
      meta[metaMatch[1].toLowerCase()] = metaMatch[2]
      continue
    }

    // No timestamp and no metadata: an unsynchronised lyric. Kept with a
    // sentinel time so it still appears in the list.
    if (!sawTimestamp) {
      lines.push({ time: -1, text: line })
    }
  }

  if (lines.length === 0) return null

  lines.sort((a, b) => a.time - b.time)
  return { lines, synced: sawTimestamp, meta }
}

function looksLikeTimestamp(line: string): boolean {
  return MALFORMED_STAMP.test(line)
}

/**
 * activeIndex returns the index of the line that should be highlighted at
 * the given position, or -1 before the first line starts.
 *
 * Binary search, because this runs on every timeupdate and a long lyric
 * sheet would otherwise make the scroll visibly stutter.
 */
export function activeIndex(lyrics: Lyrics, positionMs: number): number {
  if (!lyrics.synced || lyrics.lines.length === 0) return -1

  let lo = 0
  let hi = lyrics.lines.length - 1
  let found = -1

  while (lo <= hi) {
    const mid = (lo + hi) >> 1
    if (lyrics.lines[mid].time <= positionMs) {
      found = mid
      lo = mid + 1
    } else {
      hi = mid - 1
    }
  }
  return found
}

/** Flattens synced lyrics to plain text, for the metadata or a fallback view. */
export function lyricsToText(lyrics: Lyrics | null): string {
  if (!lyrics) return ''
  return lyrics.lines.map((l) => l.text).join('\n')
}
