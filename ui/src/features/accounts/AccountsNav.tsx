import { Plus } from 'lucide-react';
import { useState } from 'react';
import { useControlPlane } from '@/data/ControlPlaneProvider';
import { ROW } from '@/shared/layout/Sidebar';
import { useShell } from '@/shared/layout/ShellProvider';
import { cn } from '@/shared/lib/cn';
import { formatComputeUnits } from '@/shared/lib/format';
import { useLocation } from '@/shared/lib/location';
import { navigate, paths } from '@/shared/lib/paths';
import { IconButton } from '@/shared/ui/IconButton';
import { StatusDot } from '@/shared/ui/StatusDot';
import { CreateAccountDialog } from './CreateAccountDialog';

/**
 * Contextual Sidebar for Accounts:
 * Lists all Chrome Profiles with process glyph (`●` Chrome up / `○` stopped),
 * cached CCU balance (`15.25 CU`), and a "New account" trigger.
 */
export function AccountsNav() {
  const { profiles } = useControlPlane();
  const { segments } = useLocation();
  const { dismissOnCompact } = useShell();
  const [creating, setCreating] = useState(false);

  const selectedId = segments[0] === 'accounts' ? segments[1] : undefined;

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center justify-between">
        <a
          href={paths.accounts}
          onClick={dismissOnCompact}
          className="font-mono text-caption font-semibold uppercase tracking-wide text-fg hover:underline"
        >
          Accounts ({profiles.length})
        </a>
        <IconButton size="sm" label="New account" onClick={() => setCreating(true)}>
          <Plus aria-hidden className="size-4" strokeWidth={1.75} />
        </IconButton>
      </div>

      {profiles.length === 0 ? (
        <p className="m-0 py-2 text-caption text-subtle">No accounts registered.</p>
      ) : (
        <ul className="flex flex-col gap-0.5">
          {profiles.map((account) => {
            const active = account.id === selectedId;
            return (
              <li key={account.id}>
                <a
                  href={paths.account(account.id)}
                  aria-current={active ? 'page' : undefined}
                  onClick={dismissOnCompact}
                  className={cn(
                    ROW,
                    'gap-2.5 px-2.5',
                    active ? 'bg-hover font-medium text-fg' : 'text-muted hover:bg-hover hover:text-fg',
                  )}
                >
                  <StatusDot
                    tone={account.is_running ? 'solid' : 'hollow'}
                    label={account.is_running ? 'Chrome running' : 'Chrome stopped'}
                  />
                  <span className="min-w-0 flex-1 truncate">{account.name}</span>
                  <span className="shrink-0 font-mono text-caption tabular-nums text-subtle">
                    {formatComputeUnits(account.ccu_balance)}
                  </span>
                </a>
              </li>
            );
          })}
        </ul>
      )}

      {creating && (
        <CreateAccountDialog
          onCreated={(id) => navigate(paths.account(id))}
          onClose={() => setCreating(false)}
        />
      )}
    </div>
  );
}
