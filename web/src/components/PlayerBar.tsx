import { useEffect, useRef, useState } from 'react'
import type { PlayerSnapshot } from '../lib/player'
import { formatDuration } from '../lib/format'
import { coverUrl } from '../lib/api'
import { Cover, IconButton } from './ui'
import {
  LyricsIcon,
  MuteIcon,
  PauseIcon,
  PlayIcon,
  QueueIcon,
  RepeatIcon,
  RepeatOneIcon,
  ShuffleIcon,
  SkipBackIcon,
  SkipForwardIcon,
  VolumeIcon,
  VolumeLowIcon,
} from './icons'

/**
 * The persistent player bar.
 *
 * On a phone it sits above the tab bar as a floating pill; on a desktop it
 * spans the bottom. The seek bar is a range input rather than a custom
 * pointer-driven control, because that is what gives correct touch dragging,
 * keyboard support and screen-reader announcements for free.
 */
export function PlayerBar({
  snapshot,
  onToggle,
  onNext,
  onPrevious,
  onSeek,
  onVolume,
  onToggleMute,
  onToggleShuffle,
  onCycleRepeat,
  onOpenNowPlaying,
  onOpenQueue,
  onOpenLyrics,
}: {
  snapshot: PlayerSnapshot
  onToggle: () => void
  onNext: () => void
  onPrevious: () => void
  onSeek: (seconds: number) => void
  onVolume: (value: number) => void
  onToggleMute: () => void
  onToggleShuffle: () => void
  onCycleRepeat: () => void
  onOpenNowPlaying: () => void
  onOpenQueue: () => void
  onOpenLyrics: () => void
}) {
  const { current, playing, position, duration, volume, muted } = snapshot
  const [scrubbing, setScrubbing] = useState<number | null>(null)
  const barRef = useRef<HTMLDivElement>(null)

  const shown = scrubbing ?? position
  const progress = duration > 0 ? (shown / duration) * 100 : 0

  // Keyboard shortcuts, ignored while the user is typing in a field.
  useEffect(() => {
    function onKey(event: KeyboardEvent) {
      const target = event.target as HTMLElement | null
      if (
        target &&
        (target.tagName === 'INPUT' ||
          target.tagName === 'TEXTAREA' ||
          target.isContentEditable)
      ) {
        return
      }

      switch (event.key) {
        case ' ':
          event.preventDefault()
          onToggle()
          break
        case 'ArrowRight':
          if (event.shiftKey) onNext()
          else if (duration > 0) onSeek(Math.min(duration, shown + 5))
          break
        case 'ArrowLeft':
          if (event.shiftKey) onPrevious()
          else onSeek(Math.max(0, shown - 5))
          break
        case 'ArrowUp':
          event.preventDefault()
          onVolume(Math.min(1, volume + 0.05))
          break
        case 'ArrowDown':
          event.preventDefault()
          onVolume(Math.max(0, volume - 0.05))
          break
        case 'm':
          onToggleMute()
          break
        case 's':
          onToggleShuffle()
          break
        case 'r':
          onCycleRepeat()
          break
        case 'l':
          onOpenLyrics()
          break
        case 'q':
          onOpenQueue()
          break
      }
    }

    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [
    onToggle, onNext, onPrevious, onSeek, onVolume, onToggleMute,
    onToggleShuffle, onCycleRepeat, onOpenLyrics, onOpenQueue,
    duration, volume, shown,
  ])

  if (!current) return null

  return (
    <div
      ref={barRef}
      className="glass-solid pointer-events-auto border-x-0 border-b-0 px-3 pb-3 pt-2.5
        sm:px-4 lg:px-5"
      style={{ paddingBottom: 'max(0.75rem, env(safe-area-inset-bottom))' }}
    >
      {/* Mobile: the track itself is the button that opens Now Playing. */}
      <button
        type="button"
        onClick={onOpenNowPlaying}
        className="mb-2 flex w-full items-center gap-3 text-left lg:hidden"
      >
        <Cover
          src={coverUrl(current.coverPath)}
          alt=""
          seed={current.albumId}
          className="h-11 w-11 shrink-0"
          rounded="rounded-lg"
        />
        <div className="min-w-0 flex-1">
          <p className="truncate text-sm font-semibold text-ink-100">{current.title}</p>
          <p className="truncate text-xs text-ink-400">{current.artist}</p>
        </div>
        <div className="flex items-center gap-1" onClick={(e) => e.stopPropagation()}>
          <IconButton label={playing ? 'Pause' : 'Play'} onClick={onToggle} active={playing}>
            {playing ? <PauseIcon /> : <PlayIcon className="ml-0.5" />}
          </IconButton>
          <IconButton label="Next track" onClick={onNext}>
            <SkipForwardIcon />
          </IconButton>
        </div>
      </button>

      <div className="flex items-center gap-3 lg:gap-5">
        {/* Artwork, hidden on mobile because the row above covers it. */}
        <button
          type="button"
          onClick={onOpenNowPlaying}
          className="pressable hidden shrink-0 lg:block"
          aria-label={`Open ${current.title}`}
        >
          <Cover
            src={coverUrl(current.coverPath)}
            alt=""
            seed={current.albumId}
            className="h-14 w-14"
          />
        </button>

        <div className="hidden min-w-0 basis-[22%] flex-col lg:flex">
          <p className="truncate text-sm font-semibold text-ink-100">{current.title}</p>
          <p className="truncate text-xs text-ink-400">{current.artist}</p>
        </div>

        {/* Transport */}
        <div className="flex shrink-0 items-center gap-1 lg:gap-2">
          <IconButton
            label={snapshot.queue.shuffle ? 'Shuffle on' : 'Shuffle off'}
            onClick={onToggleShuffle}
            active={snapshot.queue.shuffle}
            size="sm"
            className="hidden sm:grid"
          >
            <ShuffleIcon className="h-[18px] w-[18px]" />
          </IconButton>

          <IconButton label="Previous track" onClick={onPrevious} size="sm" className="hidden sm:grid">
            <SkipBackIcon />
          </IconButton>
          <IconButton
            label={playing ? 'Pause' : 'Play'}
            onClick={onToggle}
            size="lg"
            className="bg-[var(--accent-live)] text-ink-950 hover:brightness-110"
          >
            {playing ? <PauseIcon className="h-6 w-6" /> : <PlayIcon className="h-6 w-6 ml-0.5" />}
          </IconButton>
          <IconButton label="Next track" onClick={onNext} size="sm" className="hidden sm:grid">
            <SkipForwardIcon />
          </IconButton>

          <IconButton
            label={
              snapshot.queue.repeat === 'one'
                ? 'Repeat one'
                : snapshot.queue.repeat === 'all'
                  ? 'Repeat all'
                  : 'Repeat off'
            }
            onClick={onCycleRepeat}
            active={snapshot.queue.repeat !== 'off'}
            size="sm"
            className="hidden sm:grid"
          >
            {snapshot.queue.repeat === 'one' ? <RepeatOneIcon /> : <RepeatIcon />}
          </IconButton>
        </div>

        {/* Seek */}
        <div className="flex min-w-0 flex-1 items-center gap-2.5">
          <span className="w-9 shrink-0 text-right text-[11px] tabular-nums text-ink-400">
            {formatDuration(shown * 1000)}
          </span>
          <input
            type="range"
            min={0}
            max={duration || 1}
            step={0.5}
            value={shown}
            onChange={(e) => setScrubbing(Number(e.target.value))}
            onPointerUp={() => {
              if (scrubbing !== null) onSeek(scrubbing)
              setScrubbing(null)
            }}
            onKeyUp={() => {
              if (scrubbing !== null) onSeek(scrubbing)
              setScrubbing(null)
            }}
            className="seek h-4 w-full"
            style={{ ['--progress' as string]: `${progress}%` }}
            aria-label="Seek"
            aria-valuetext={`${formatDuration(shown * 1000)} of ${formatDuration(duration * 1000)}`}
          />
          <span className="w-9 shrink-0 text-[11px] tabular-nums text-ink-400">
            {formatDuration(duration * 1000)}
          </span>
        </div>

        {/* Right-hand actions */}
        <div className="hidden shrink-0 items-center gap-1 lg:flex">
          {current.hasLyrics && (
            <IconButton label="Lyrics" onClick={onOpenLyrics} size="sm">
              <LyricsIcon className="h-[18px] w-[18px]" />
            </IconButton>
          )}
          <IconButton label="Queue" onClick={onOpenQueue} size="sm">
            <QueueIcon className="h-[18px] w-[18px]" />
          </IconButton>
          <div className="group flex items-center gap-2">
            <IconButton label={muted ? 'Unmute' : 'Mute'} onClick={onToggleMute} size="sm">
              {muted || volume === 0 ? (
                <MuteIcon className="h-[18px] w-[18px]" />
              ) : volume < 0.5 ? (
                <VolumeLowIcon className="h-[18px] w-[18px]" />
              ) : (
                <VolumeIcon className="h-[18px] w-[18px]" />
              )}
            </IconButton>
            <input
              type="range"
              min={0}
              max={1}
              step={0.01}
              value={muted ? 0 : volume}
              onChange={(e) => onVolume(Number(e.target.value))}
              className="seek h-4 w-20"
              style={{ ['--progress' as string]: `${(muted ? 0 : volume) * 100}%` }}
              aria-label="Volume"
            />
          </div>
        </div>
      </div>
    </div>
  )
}
