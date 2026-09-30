/** The Audiora mark: a glass disc with a waveform cut into it. */
export function Logo({ className = 'h-8 w-8' }: { className?: string }) {
  return (
    <svg viewBox="0 0 48 48" className={className} aria-label="Audiora" role="img">
      <defs>
        <linearGradient id="audiora-glass" x1="0" y1="0" x2="1" y2="1">
          <stop offset="0%" stopColor="oklch(0.82 0.14 305)" stopOpacity="0.95" />
          <stop offset="100%" stopColor="oklch(0.55 0.2 275)" stopOpacity="0.9" />
        </linearGradient>
        <linearGradient id="audiora-sheen" x1="0" y1="0" x2="0.4" y2="1">
          <stop offset="0%" stopColor="#fff" stopOpacity="0.55" />
          <stop offset="55%" stopColor="#fff" stopOpacity="0.06" />
          <stop offset="100%" stopColor="#fff" stopOpacity="0" />
        </linearGradient>
      </defs>

      <circle cx="24" cy="24" r="21" fill="url(#audiora-glass)" />
      <circle cx="24" cy="24" r="21" fill="url(#audiora-sheen)" />
      <circle
        cx="24"
        cy="24"
        r="20.4"
        fill="none"
        stroke="#fff"
        strokeOpacity="0.28"
        strokeWidth="1"
      />

      {/* Waveform bars, centred, reading as an equaliser. */}
      <g fill="oklch(0.13 0.02 285)">
        <rect x="13.5" y="21" width="2.6" height="6" rx="1.3" />
        <rect x="18.2" y="17.5" width="2.6" height="13" rx="1.3" />
        <rect x="22.9" y="14" width="2.6" height="20" rx="1.3" />
        <rect x="27.6" y="19" width="2.6" height="10" rx="1.3" />
        <rect x="32.3" y="22" width="2.6" height="4" rx="1.3" />
      </g>
    </svg>
  )
}
