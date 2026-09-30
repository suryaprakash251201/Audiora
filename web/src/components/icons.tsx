/**
 * Inline SVG icon set.
 *
 * Bundled rather than pulled from a package so the app ships one small chunk
 * with no icon-library dependency, and so stroke width and cap style stay
 * consistent with the design.
 */

type IconProps = { className?: string }

const base = {
  fill: 'none',
  stroke: 'currentColor',
  strokeWidth: 1.8,
  strokeLinecap: 'round' as const,
  strokeLinejoin: 'round' as const,
  viewBox: '0 0 24 24',
}

export const PlayIcon = ({ className = 'h-5 w-5' }: IconProps) => (
  <svg {...base} className={className} aria-hidden="true">
    <path d="M7 4.5v15l13-7.5-13-7.5Z" fill="currentColor" stroke="none" />
  </svg>
)

export const PauseIcon = ({ className = 'h-5 w-5' }: IconProps) => (
  <svg {...base} className={className} aria-hidden="true">
    <rect x="6" y="4" width="4" height="16" rx="1.4" fill="currentColor" stroke="none" />
    <rect x="14" y="4" width="4" height="16" rx="1.4" fill="currentColor" stroke="none" />
  </svg>
)

export const SkipBackIcon = ({ className = 'h-5 w-5' }: IconProps) => (
  <svg {...base} className={className} aria-hidden="true">
    <path d="M19 5v14L8 12l11-7Z" fill="currentColor" stroke="none" />
    <path d="M6 5v14" />
  </svg>
)

export const SkipForwardIcon = ({ className = 'h-5 w-5' }: IconProps) => (
  <svg {...base} className={className} aria-hidden="true">
    <path d="M5 5v14l11-7L5 5Z" fill="currentColor" stroke="none" />
    <path d="M18 5v14" />
  </svg>
)

export const ShuffleIcon = ({ className = 'h-5 w-5' }: IconProps) => (
  <svg {...base} className={className} aria-hidden="true">
    <path d="M17 3.5 21 7l-4 3.5" />
    <path d="M17 13.5 21 17l-4 3.5" />
    <path d="M3 7h3.5c1.6 0 2.6.8 3.4 2l2.2 3.4c.8 1.2 1.8 2 3.4 2H21" />
    <path d="M3 17h3.5c1.6 0 2.6-.8 3.4-2l.6-.9" />
    <path d="M14 8.6l.6-.9c.8-1.2 1.8-2 3.4-2H21" />
  </svg>
)

export const RepeatIcon = ({ className = 'h-5 w-5' }: IconProps) => (
  <svg {...base} className={className} aria-hidden="true">
    <path d="M17 2.5 20.5 6 17 9.5" />
    <path d="M3.5 12V9a3 3 0 0 1 3-3h14" />
    <path d="M7 21.5 3.5 18 7 14.5" />
    <path d="M20.5 12v3a3 3 0 0 1-3 3h-14" />
  </svg>
)

export const RepeatOneIcon = ({ className = 'h-5 w-5' }: IconProps) => (
  <svg {...base} className={className} aria-hidden="true">
    <path d="M17 2.5 20.5 6 17 9.5" />
    <path d="M3.5 12V9a3 3 0 0 1 3-3h14" />
    <path d="M7 21.5 3.5 18 7 14.5" />
    <path d="M20.5 12v3a3 3 0 0 1-3 3h-14" />
    <path d="M11.4 10.5 12.8 10v4.2" />
  </svg>
)

export const VolumeIcon = ({ className = 'h-5 w-5' }: IconProps) => (
  <svg {...base} className={className} aria-hidden="true">
    <path d="M11 5 6.5 9H3v6h3.5L11 19V5Z" />
    <path d="M15.5 8.8a4.2 4.2 0 0 1 0 6.4" />
    <path d="M18.2 6a8 8 0 0 1 0 12" />
  </svg>
)

export const VolumeLowIcon = ({ className = 'h-5 w-5' }: IconProps) => (
  <svg {...base} className={className} aria-hidden="true">
    <path d="M11 5 6.5 9H3v6h3.5L11 19V5Z" />
  </svg>
)

export const MuteIcon = ({ className = 'h-5 w-5' }: IconProps) => (
  <svg {...base} className={className} aria-hidden="true">
    <path d="M11 5 6.5 9H3v6h3.5L11 19V5Z" />
    <path d="m16 9.5 5 5" />
    <path d="m21 9.5-5 5" />
  </svg>
)

export const HeartIcon = ({
  className = 'h-5 w-5',
  filled = false,
}: IconProps & { filled?: boolean }) => (
  <svg {...base} className={className} aria-hidden="true">
    <path
      d="M12 20.5S3.5 15.2 3.5 9.2A4.7 4.7 0 0 1 12 6.4a4.7 4.7 0 0 1 8.5 2.8c0 6-8.5 11.3-8.5 11.3Z"
      fill={filled ? 'currentColor' : 'none'}
    />
  </svg>
)

export const HomeIcon = ({ className = 'h-5 w-5' }: IconProps) => (
  <svg {...base} className={className} aria-hidden="true">
    <path d="M3.5 10.5 12 3.5l8.5 7" />
    <path d="M5.5 9.5V20h13V9.5" />
  </svg>
)

export const AlbumIcon = ({ className = 'h-5 w-5' }: IconProps) => (
  <svg {...base} className={className} aria-hidden="true">
    <circle cx="12" cy="12" r="8.5" />
    <circle cx="12" cy="12" r="2.4" />
  </svg>
)

export const ArtistIcon = ({ className = 'h-5 w-5' }: IconProps) => (
  <svg {...base} className={className} aria-hidden="true">
    <circle cx="12" cy="8" r="4" />
    <path d="M4.5 20.5c0-3.9 3.4-6.5 7.5-6.5s7.5 2.6 7.5 6.5" />
  </svg>
)

export const PlaylistIcon = ({ className = 'h-5 w-5' }: IconProps) => (
  <svg {...base} className={className} aria-hidden="true">
    <path d="M4 7h11" />
    <path d="M4 12h11" />
    <path d="M4 17h7" />
    <circle cx="17.5" cy="16.5" r="2.5" />
    <path d="M20 16.5V7l-2.5 1" />
  </svg>
)

export const SearchIcon = ({ className = 'h-5 w-5' }: IconProps) => (
  <svg {...base} className={className} aria-hidden="true">
    <circle cx="10.5" cy="10.5" r="6.5" />
    <path d="m15.5 15.5 4.5 4.5" />
  </svg>
)

export const QueueIcon = ({ className = 'h-5 w-5' }: IconProps) => (
  <svg {...base} className={className} aria-hidden="true">
    <path d="M4 7h11" />
    <path d="M4 12h11" />
    <path d="M4 17h7" />
    <path d="M18.5 5v9.5" />
    <circle cx="17" cy="16" r="2" />
  </svg>
)

export const LyricsIcon = ({ className = 'h-5 w-5' }: IconProps) => (
  <svg {...base} className={className} aria-hidden="true">
    <path d="M5 4.5h14" />
    <path d="M5 9.5h9" />
    <path d="M5 14.5h11" />
    <path d="M5 19.5h6" />
  </svg>
)

export const SettingsIcon = ({ className = 'h-5 w-5' }: IconProps) => (
  <svg {...base} className={className} aria-hidden="true">
    <circle cx="12" cy="12" r="3" />
    <path d="M19.4 15a1.6 1.6 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.6 1.6 0 0 0-1.8-.3 1.6 1.6 0 0 0-1 1.5v.2a2 2 0 1 1-4 0v-.1a1.6 1.6 0 0 0-1-1.5 1.6 1.6 0 0 0-1.8.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.6 1.6 0 0 0 .3-1.8 1.6 1.6 0 0 0-1.5-1H2a2 2 0 1 1 0-4h.1a1.6 1.6 0 0 0 1.5-1 1.6 1.6 0 0 0-.3-1.8l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.6 1.6 0 0 0 1.8.3H8a1.6 1.6 0 0 0 1-1.5V2a2 2 0 1 1 4 0v.1a1.6 1.6 0 0 0 1 1.5 1.6 1.6 0 0 0 1.8-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.6 1.6 0 0 0-.3 1.8V8a1.6 1.6 0 0 0 1.5 1h.2a2 2 0 1 1 0 4h-.1a1.6 1.6 0 0 0-1.5 1Z" />
  </svg>
)

export const ChevronRightIcon = ({ className = 'h-5 w-5' }: IconProps) => (
  <svg {...base} className={className} aria-hidden="true">
    <path d="m9 5 7 7-7 7" />
  </svg>
)

export const ChevronDownIcon = ({ className = 'h-5 w-5' }: IconProps) => (
  <svg {...base} className={className} aria-hidden="true">
    <path d="m5 9 7 7 7-7" />
  </svg>
)

export const CloseIcon = ({ className = 'h-5 w-5' }: IconProps) => (
  <svg {...base} className={className} aria-hidden="true">
    <path d="m6 6 12 12" />
    <path d="m18 6-12 12" />
  </svg>
)

export const MoreIcon = ({ className = 'h-5 w-5' }: IconProps) => (
  <svg {...base} className={className} aria-hidden="true">
    <circle cx="5" cy="12" r="1.4" fill="currentColor" />
    <circle cx="12" cy="12" r="1.4" fill="currentColor" />
    <circle cx="19" cy="12" r="1.4" fill="currentColor" />
  </svg>
)

export const PlusIcon = ({ className = 'h-5 w-5' }: IconProps) => (
  <svg {...base} className={className} aria-hidden="true">
    <path d="M12 5v14" />
    <path d="M5 12h14" />
  </svg>
)

export const RefreshIcon = ({ className = 'h-5 w-5' }: IconProps) => (
  <svg {...base} className={className} aria-hidden="true">
    <path d="M20 11a8 8 0 1 0-1.5 5.5" />
    <path d="M20 5v6h-6" />
  </svg>
)

export const TrashIcon = ({ className = 'h-5 w-5' }: IconProps) => (
  <svg {...base} className={className} aria-hidden="true">
    <path d="M4.5 7h15" />
    <path d="M9 7V4.5h6V7" />
    <path d="M6.5 7l.8 12.5h9.4L17.5 7" />
    <path d="M10.5 11v5" />
    <path d="M13.5 11v5" />
  </svg>
)

export const MusicNoteIcon = ({ className = 'h-5 w-5' }: IconProps) => (
  <svg viewBox="0 0 24 24" fill="currentColor" className={className} aria-hidden="true">
    <path d="M12 3v10.55A4 4 0 1 0 14 17V7h4V3h-6Z" />
  </svg>
)

export const CheckIcon = ({ className = 'h-5 w-5' }: IconProps) => (
  <svg {...base} className={className} strokeWidth={3} aria-hidden="true">
    <path d="m4.5 12.5 5 5 10-11" />
  </svg>
)
