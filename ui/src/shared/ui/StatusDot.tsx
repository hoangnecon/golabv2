import { cn } from '@/shared/lib/cn';

/**
 * Pure monochrome glyph tones (Spec v2 §7.1):
 * - solid   `●` : idle / owner / editor / running_chrome / locked / success
 * - active  `◎` : busy / connecting / executing
 * - hollow  `○` : unlocked / runtime_disconnected / browser_stopped / tab_closed
 * - warn    `△` : view_only / commenter / denied / auth_required / drive_auth_pending
 * - error   `▪` : failed tool call / corrupted_local_dir / offline
 */
export type Tone = 'solid' | 'active' | 'hollow' | 'warn' | 'error';

const GLYPH: Record<Tone, string> = {
  solid: '●',
  active: '◎',
  hollow: '○',
  warn: '△',
  error: '▪',
};

const TONE_CLASS: Record<Tone, string> = {
  solid: 'text-fg',
  active: 'text-fg',
  hollow: 'text-subtle',
  warn: 'text-fg',
  error: 'text-fg',
};

interface StatusDotProps {
  tone: Tone;
  /** Accessible spoken label; shape alone never carries meaning. */
  label: string;
  className?: string;
}

export function StatusDot({ tone, label, className }: StatusDotProps) {
  return (
    <span
      role="img"
      aria-label={label}
      title={label}
      className={cn(
        'inline-flex shrink-0 select-none items-center justify-center font-mono text-caption leading-none',
        TONE_CLASS[tone],
        className,
      )}
    >
      {GLYPH[tone]}
    </span>
  );
}

/** Monochrome glyph paired with uppercase monospace label. */
export function Status({ tone, label, className }: StatusDotProps) {
  return (
    <span className={cn('inline-flex items-center gap-1.5 font-mono text-caption uppercase tracking-wide', className)}>
      <StatusDot tone={tone} label={label} />
      <span aria-hidden className={tone === 'hollow' ? 'text-muted' : 'text-fg'}>
        {label}
      </span>
    </span>
  );
}
