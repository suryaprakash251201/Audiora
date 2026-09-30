import { useEffect, useRef, useState } from 'react'
import type { PlayerSnapshot } from '../lib/player'
import { coverUrl, api, type Track } from '../lib/api'
import { activeIndex, parseLRC, type Lyrics } from '../lib/lyrics'
import { formatDuration } from '../lib/format'
import { CloseIcon, HeartIcon, LyricsIcon } from './icons'
import { IconButton } from './ui'

/**
 * The full-screen Now Playing sheet.
 *
 * The backdrop is a scaled, blurred copy of the album art rather than a
 * backdrop-filter over it. A filter on a full-screen element forces the
 * compositor to re-blur on every frame of the animated mesh, which is the
 * single most expensive thing a glass UI can do on a phone. Scaling the
 * artwork up and letting it be blurry is visually near-identical and free.
 */
export function NowPlaying({
  snapshot,
  onClose,
  onToggle,
  onNext,
  onPrevious,
  onSeek,
  isFavorite,
  onToggleFavorite,
}: {
  snapshot: PlayerSnapshot
  onClose: () => void
  onToggle: () => void
  onNext: () => void
  onPrevious: () => void
  onSeek: (seconds: number) => void
  isFavorite: boolean
  onToggleFavorite: () => void
}) {
  const { current, playing, position, duration } = snapshot
  const [showLyrics, setShowLyrics] = useState(false)
  const [lyrics, setLyrics] = useState<Lyrics | null>(null)
  const [loadingLyrics, setLoadingLyrics] = useState(false)
  const [lineIndex, setLineIndex] = useState(-1)
  const lineRefs = useRef<(HTMLParagraphElement | null)[]>([])

  // Load lyrics whenever the track changes, so the pane is ready before the
  // listener thinks to open it.
  useEffect(() => {
    if (!current) return
    let cancelled = false
    setLyrics(null)
    setLineIndex(-1)

    if (!current.hasLyrics) {
      setShowLyrics(false)
      return
    }

    setLoadingLyrics(true)
    void api
      .lyrics(current.id)
      .then((data) => {
        if (cancelled || !data.lyrics) return
        setLyrics(parseLRC(data.lyrics))
      })
      .catch(() => undefined)
      .finally(() => {
        if (!cancelled) setLoadingLyrics(false)
      })

    return () => {
      cancelled = true
    }
  }, [current?.id, current?.hasLyrics])

  // Highlight the active line. Guarded on synced lyrics, because an
  // unsynchronised sheet has no lines to highlight.
  useEffect(() => {
    if (!lyrics?.synced) return
    setLineIndex(activeIndex(lyrics, position * 1000))
  }, [lyrics, position])

  // Keep the active line centred as it changes.
  useEffect(() => {
    if (lineIndex < 0) return
    const el = lineRefs.current[lineIndex]
    el?.scrollIntoView({ behavior: 'smooth', block: 'center' })
  }, [lineIndex])

  useEffect(() => {
    function onKey(event: KeyboardEvent) {
      if (event.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  if (!current) return null

  const art = coverUrl(current.coverPath)
  const progress = duration > 0 ? (position / duration) * 100 : 0

  return (
    <div className="animate-fade fixed inset-0 z-40 overflow-hidden" role="dialog" aria-modal="true">
      {/* Blurred artwork backdrop. Two layers, so the colour underneath is
          never fully hidden and the sheet reads as glass over art. */}
      {art && (
        <>
          <div
            className="absolute inset-0 scale-125 bg-cover bg-center blur-3xl"
            style={{ backgroundImage: `url(${art})`, opacity: 0.55 }}
            aria-hidden="true"
          />
          <div
            className="absolute inset-0"
            style={{
              background:
                'linear-gradient(to bottom, oklch(0.13 0.015 285 / 0.55) 0%, oklch(0.13 0.015 285 / 0.86) 45%, oklch(0.13 0.015 285 / 0.97) 100%)',
            }}
            aria-hidden="true"
          />
        </>
      )}
      {!art && <div className="ambient" aria-hidden="true" />}

      <div className="relative flex h-full flex-col">
        {/* Header */}
        <header
          className="flex shrink-0 items-center justify-between px-4 py-3"
          style={{ paddingTop: 'max(0.75rem, env(safe-area-inset-top))' }}
        >
          <IconButton label="Close" onClick={onClose}>
            <CloseIcon />
          </IconButton>
          <div className="min-w-0 text-center">
            <p className="text-[10px] font-semibold uppercase tracking-[0.2em] text-ink-400">
              Playing from
            </p>
            <p className="truncate text-xs text-ink-200">{current.album}</p>
          </div>
          <IconButton
            label={isFavorite ? 'Remove from favourites' : 'Add to favourites'}
            onClick={onToggleFavorite}
            active={isFavorite}
          >
            <HeartIcon filled={isFavorite} />
          </IconButton>
        </header>

        <div className="scroll-pane flex min-h-0 flex-1 flex-col">
          {/* Artwork, or the lyric sheet when it is open. */}
          <div className="flex min-h-0 flex-1 flex-col items-center justify-center px-6 pb-4">
            {showLyrics && current.hasLyrics ? (
              <div className="glass-bright w-full max-w-2xl flex-1 overflow-hidden rounded-[var(--radius-glass)] p-5">
                {loadingLyrics && (
                  <p className="pt-10 text-center text-sm text-ink-400">Loading lyrics…</p>
                )}
                {!loadingLyrics && !lyrics && (
                  <p className="pt-10 text-center text-sm text-ink-400">
                    No lyrics found for this track.
                  </p>
                )}
                {lyrics && (
                  <div className="scroll-pane h-full max-h-[52vh] text-center">
                    {lyrics.lines.map((line, i) => (
                      <p
                        key={`${line.time}-${i}`}
                        ref={(el) => {
                          lineRefs.current[i] = el
                        }}
                        className={`px-2 py-2.5 text-lg font-semibold leading-snug transition-all duration-300 ${
                          i === lineIndex
                            ? 'scale-[1.03] text-ink-100'
                            : 'text-ink-600'
                        }`}
                      >
                        {line.text || '♪'}
                      </p>
                    ))}
                  </div>
                )}
              </div>
            ) : (
              <button
                type="button"
                onClick={() => current.hasLyrics && setShowLyrics(true)}
                className="pressable w-full max-w-sm"
                aria-label={current.hasLyrics ? 'Show lyrics' : undefined}
              >
                {art ? (
                  <img
                    src={art}
                    alt={`${current.album} cover`}
                    className="aspect-square w-full max-w-[min(78vw,360px)] rounded-[1.75rem]
                      object-cover shadow-[0_24px_70px_oklch(0_0_0/0.6)]"
                  />
                ) : (
                  <div
                    className="aspect-square w-full max-w-[min(78vw,360px)] rounded-[1.75rem]"
                    style={{ background: 'linear-gradient(135deg, oklch(0.4 0.12 305), oklch(0.24 0.08 275))' }}
                  />
                )}
              </button>
            )}
          </div>

          {/* Metadata and controls */}
          <div className="shrink-0 px-6 pb-6" style={{ paddingBottom: 'max(1.5rem, env(safe-area-inset-bottom))' }}>
            <div className="mx-auto w-full max-w-xl">
              <h2 className="truncate text-2xl font-bold tracking-tight text-ink-100">
                {current.title}
              </h2>
              <p className="mt-0.5 truncate text-sm text-ink-300">{current.artist}</p>

              {showLyrics && current.hasLyrics && (
                <button
                  type="button"
                  onClick={() => setShowLyrics(false)}
                  className="pressable mt-3 flex items-center gap-2 rounded-full bg-white/10 px-3 py-1.5
                    text-xs font-medium text-ink-200 transition hover:bg-white/16"
                >
                  <CloseIcon className="h-3.5 w-3.5" />
                  Back to artwork
                </button>
              )}

              {/* Seek */}
              <div className="mt-5 flex items-center gap-3">
                <span className="w-10 shrink-0 text-right text-xs tabular-nums text-ink-400">
                  {formatDuration(position * 1000)}
                </span>
                <input
                  type="range"
                  min={0}
                  max={duration || 1}
                  step={0.5}
                  value={position}
                  onChange={(e) => onSeek(Number(e.target.value))}
                  className="seek h-5 w-full"
                  style={{ ['--progress' as string]: `${progress}%` }}
                  aria-label="Seek"
                />
                <span className="w-10 shrink-0 text-xs tabular-nums text-ink-400">
                  {formatDuration(duration * 1000)}
                </span>
              </div>

              {/* Transport */}
              <div className="mt-3 flex items-center justify-center gap-3">
                <IconButton label="Previous track" onClick={onPrevious} size="lg">
                  <svg viewBox="0 0 24 24" className="h-6 w-6" fill="currentColor" aria-hidden="true">
                    <path d="M19 5v14L8 12l11-7Z" />
                    <path d="M6 5v14" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" />
                  </svg>
                </IconButton>

                <IconButton
                  label={playing ? 'Pause' : 'Play'}
                  onClick={onToggle}
                  size="lg"
                  className="h-16 w-16 bg-[var(--accent-live)] text-ink-950 hover:brightness-110"
                >
                  {playing ? (
                    <svg viewBox="0 0 24 24" className="h-7 w-7" fill="currentColor" aria-hidden="true">
                      <rect x="6" y="4" width="4" height="16" rx="1.4" />
                      <rect x="14" y="4" width="4" height="16" rx="1.4" />
                    </svg>
                  ) : (
                    <svg viewBox="0 0 24 24" className="ml-1 h-7 w-7" fill="currentColor" aria-hidden="true">
                      <path d="M7 4.5v15l13-7.5-13-7.5Z" />
                    </svg>
                  )}
                </IconButton>

                <IconButton label="Next track" onClick={onNext} size="lg">
                  <svg viewBox="0 0 24 24" className="h-6 w-6" fill="currentColor" aria-hidden="true">
                    <path d="M5 5v14l11-7L5 5Z" />
                    <path d="M18 5v14" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" />
                  </svg>
                </IconButton>
              </div>

              {current.hasLyrics && !showLyrics && (
                <div className="mt-4 flex justify-center">
                  <button
                    type="button"
                    onClick={() => setShowLyrics(true)}
                    className="pressable flex items-center gap-2 rounded-full bg-white/10 px-4 py-2
                      text-xs font-medium text-ink-200 transition hover:bg-white/16"
                  >
                    <LyricsIcon className="h-4 w-4" />
                    Show lyrics
                  </button>
                </div>
              )}
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}

export type { Track }
