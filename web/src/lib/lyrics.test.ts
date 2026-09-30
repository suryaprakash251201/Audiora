import { describe, expect, it } from 'vitest'
import { parseLRC, activeIndex, lyricsToText } from './lyrics'

describe('parseLRC', () => {
  it('parses standard two-digit-fraction timestamps', () => {
    const lyrics = parseLRC('[00:12.00] First line\n[00:15.50] Second line')
    expect(lyrics?.synced).toBe(true)
    expect(lyrics?.lines).toEqual([
      { time: 12_000, text: 'First line' },
      { time: 15_500, text: 'Second line' },
    ])
  })

  it('parses three-digit milliseconds fractions', () => {
    const lyrics = parseLRC('[00:12.345] Millisecond precision')
    expect(lyrics?.lines[0].time).toBe(12_345)
  })

  it('parses hundredths and thousandths differently', () => {
    // ".5" is half a second; ".500" is also half a second, but ".05" is
    // fifty milliseconds. Getting this wrong shifts every line slightly.
    expect(parseLRC('[00:01.5] a')?.lines[0].time).toBe(1500)
    expect(parseLRC('[00:01.50] a')?.lines[0].time).toBe(1500)
    expect(parseLRC('[00:01.500] a')?.lines[0].time).toBe(1500)
    expect(parseLRC('[00:01.05] a')?.lines[0].time).toBe(1050)
    expect(parseLRC('[00:01.005] a')?.lines[0].time).toBe(1005)
  })

  it('handles timestamps with no fraction', () => {
    expect(parseLRC('[01:05] Verse')?.lines[0].time).toBe(65_000)
  })

  it('handles a line with several timestamps', () => {
    // Some taggers repeat a phrase by duplicating its timestamp.
    const lyrics = parseLRC('[00:10.00][01:20.00] Chorus')
    expect(lyrics?.lines).toEqual([
      { time: 10_000, text: 'Chorus' },
      { time: 80_000, text: 'Chorus' },
    ])
  })

  it('sorts out-of-order lines', () => {
    const lyrics = parseLRC('[00:30.00] Third\n[00:10.00] First\n[00:20.00] Second')
    expect(lyrics?.lines.map((l) => l.text)).toEqual(['First', 'Second', 'Third'])
  })

  it('reads metadata tags into meta', () => {
    const lyrics = parseLRC('[ar:Nina Simone]\n[ti:Sinnerman]\n[00:01.00] Line')
    expect(lyrics?.meta.ar).toBe('Nina Simone')
    expect(lyrics?.meta.ti).toBe('Sinnerman')
    expect(lyrics?.synced).toBe(true)
  })

  it('accepts unsynchronised plain text', () => {
    const lyrics = parseLRC('Just some words\nAnd more words')
    expect(lyrics?.synced).toBe(false)
    expect(lyrics?.lines).toHaveLength(2)
    expect(lyricsToText(lyrics)).toBe('Just some words\nAnd more words')
  })

  it('rejects empty and blank input', () => {
    expect(parseLRC('')).toBeNull()
    expect(parseLRC('   \n  \n ')).toBeNull()
  })

  it('accepts \r\n line endings', () => {
    const lyrics = parseLRC('[00:01.00] One\r\n[00:02.00] Two')
    expect(lyrics?.lines).toHaveLength(2)
  })

  it('keeps an empty timed line, for instrumental gaps', () => {
    const lyrics = parseLRC('[00:01.00] Verse\n[00:05.00]\n[00:09.00] Next')
    expect(lyrics?.lines).toHaveLength(3)
    expect(lyrics?.lines[1].text).toBe('')
  })

  it('ignores an implausible timestamp rather than producing a huge value', () => {
    // A tag like [99:99.99] is malformed; minutes are capped below.
    const lyrics = parseLRC('[00:99.00] Bad\n[00:02.00] Good')
    expect(lyrics?.lines).toHaveLength(1)
    expect(lyrics?.lines[0].text).toBe('Good')
  })
})

describe('activeIndex', () => {
  const synced = parseLRC('[00:00.00] Zero\n[00:10.00] Ten\n[00:20.00] Twenty\n[00:30.00] Thirty')!

  it('returns -1 before the first line', () => {
    // A first line at exactly 0s means there is no "before" at all.
    expect(activeIndex(synced, -500)).toBe(-1)
  })

  it('finds the line covering the current position', () => {
    expect(activeIndex(synced, 0)).toBe(0)
    expect(activeIndex(synced, 9_999)).toBe(0)
    expect(activeIndex(synced, 10_000)).toBe(1)
    expect(activeIndex(synced, 19_500)).toBe(1)
    expect(activeIndex(synced, 20_000)).toBe(2)
  })

  it('holds the last line past its end', () => {
    expect(activeIndex(synced, 29_999)).toBe(2)
    expect(activeIndex(synced, 30_000)).toBe(3)
    expect(activeIndex(synced, 999_999)).toBe(3)
  })

  it('returns -1 for unsynchronised lyrics', () => {
    const plain = parseLRC('One\nTwo')!
    expect(activeIndex(plain, 5000)).toBe(-1)
  })

  it('agrees with a linear scan over a long sheet', () => {
    // The binary search must match a naive implementation exactly, since a
    // mismatch would highlight the wrong line on long lyrics.
    const lines = Array.from({ length: 500 }, (_, i) => `[00:${String(i).padStart(2, '0')}.00] Line ${i}`)
    const long = parseLRC(lines.join('\n'))!
    for (let t = 0; t < 120_000; t += 977) {
      let expected = -1
      for (let i = 0; i < long.lines.length; i++) {
        if (long.lines[i].time <= t) expected = i
        else break
      }
      expect(activeIndex(long, t)).toBe(expected)
    }
  })
})
