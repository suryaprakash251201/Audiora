import { useEffect, useRef, useState, type ReactNode } from 'react'
import { hexToOklch, placeholderGradient } from '../lib/format'

/**
 * A cover image that falls back to a deterministic gradient when the album
 * has no artwork, and never leaves a broken image icon in the grid.
 */
export function Cover({
  src,
  alt,
  seed,
  className = '',
  rounded = 'rounded-xl',
}: {
  src: string | null
  alt: string
  seed: string | number
  className?: string
  rounded?: string
}) {
  const [failed, setFailed] = useState(false)

  if (!src || failed) {
    return (
      <div
        className={`${rounded} ${className} grid place-items-center`}
        style={{ background: placeholderGradient(seed) }}
        aria-hidden="true"
      >
        <MusicGlyph className="h-1/3 w-1/3 max-h-10 max-w-10 opacity-40" />
      </div>
    )
  }

  return (
    <img
      src={src}
      alt={alt}
      loading="lazy"
      decoding="async"
      onError={() => setFailed(true)}
      className={`${rounded} ${className} object-cover`}
    />
  )
}

function MusicGlyph({ className = '' }: { className?: string }) {
  return (
    <svg viewBox="0 0 24 24" fill="currentColor" className={className} aria-hidden="true">
      <path d="M12 3v10.55A4 4 0 1 0 14 17V7h4V3h-6Z" />
    </svg>
  )
}

/** The equaliser shown on the currently playing row. */
export function PlayingIndicator() {
  return (
    <span className="eq" aria-label="Now playing" role="status">
      <span />
      <span />
      <span />
    </span>
  )
}

/** A glass card wrapper with consistent padding and rounding. */
export function Panel({
  children,
  className = '',
  as: Tag = 'section',
}: {
  children: ReactNode
  className?: string
  as?: 'section' | 'div' | 'aside'
}) {
  return <Tag className={`glass rounded-[var(--radius-glass)] ${className}`}>{children}</Tag>
}

/** An icon button with a consistent hit area and pressed state. */
export function IconButton({
  onClick,
  label,
  children,
  className = '',
  active = false,
  size = 'md',
  disabled = false,
}: {
  onClick?: () => void
  label: string
  children: ReactNode
  className?: string
  active?: boolean
  size?: 'sm' | 'md' | 'lg'
  disabled?: boolean
}) {
  const dimensions = size === 'sm' ? 'h-8 w-8' : size === 'lg' ? 'h-14 w-14' : 'h-10 w-10'
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      aria-label={label}
      aria-pressed={active}
      title={label}
      className={`pressable grid ${dimensions} place-items-center rounded-full
        disabled:cursor-not-allowed disabled:opacity-40
        ${active ? 'text-[var(--accent-live)]' : 'text-ink-200 hover:text-ink-100'}
        ${className}`}
    >
      {children}
    </button>
  )
}

/** Shown when a list has nothing in it. */
export function EmptyState({
  title,
  hint,
  action,
}: {
  title: string
  hint?: string
  action?: ReactNode
}) {
  return (
    <div className="animate-fade flex flex-col items-center justify-center gap-3 px-6 py-16 text-center">
      <div className="glass grid h-16 w-16 place-items-center rounded-full text-ink-400">
        <MusicGlyph className="h-7 w-7" />
      </div>
      <h3 className="text-lg font-semibold text-ink-100">{title}</h3>
      {hint && <p className="max-w-sm text-sm text-ink-400">{hint}</p>}
      {action}
    </div>
  )
}

/** A skeleton tile that holds space while content loads. */
export function Skeleton({ className = '' }: { className?: string }) {
  return <div className={`animate-pulse rounded-xl bg-white/5 ${className}`} aria-hidden="true" />
}

/** A page heading with an optional large title treatment. */
export function PageHeader({
  eyebrow,
  title,
  subtitle,
  children,
}: {
  eyebrow?: string
  title: string
  subtitle?: string
  children?: ReactNode
}) {
  return (
    <header className="animate-rise mb-6 flex flex-wrap items-end justify-between gap-4">
      <div className="min-w-0">
        {eyebrow && (
          <p className="mb-1 text-xs font-semibold uppercase tracking-[0.18em] text-ink-400">
            {eyebrow}
          </p>
        )}
        <h1 className="truncate text-3xl font-bold tracking-tight text-ink-100 sm:text-4xl">
          {title}
        </h1>
        {subtitle && <p className="mt-1 text-sm text-ink-400">{subtitle}</p>}
      </div>
      {children}
    </header>
  )
}

/** Applies a theme colour by writing CSS custom properties on :root. */
export function useAccentColor(color: string | null | undefined) {
  const previous = useRef<string | null>(null)

  useEffect(() => {
    if (!color) return
    const root = document.documentElement
    const next = hexToOklch(color, 0.08)
    const soft = hexToOklch(color, 0.16)

    if (previous.current === null) {
      previous.current = root.style.getPropertyValue('--accent-live')
    }
    root.style.setProperty('--accent-live', next)
    root.style.setProperty('--accent-live-soft', soft)
    root.style.setProperty('--accent-glow', `${next.replace('oklch', 'oklch').replace(')', ' / 0.35)')}`)

    return () => {
      if (previous.current) {
        root.style.setProperty('--accent-live', previous.current)
        previous.current = null
      }
    }
  }, [color])
}
