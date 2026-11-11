import { Play, Plus, Square, UserRound } from 'lucide-react';
import { useState } from 'react';
import { useControlPlane } from '@/data/ControlPlaneProvider';
import { Page } from '@/shared/layout/Page';
import {
  NONE,
  formatBurnRate,
  formatComputeUnits,
  formatRelative,
  formatTier,
} from '@/shared/lib/format';
import { navigate, paths } from '@/shared/lib/paths';
import { Avatar } from '@/shared/ui/Avatar';
import { Button } from '@/shared/ui/Button';
import { EmptyState } from '@/shared/ui/EmptyState';
import { IconButton } from '@/shared/ui/IconButton';
import { Status } from '@/shared/ui/StatusDot';
import { TBody, TableCard, Td, Th } from '@/shared/ui/Table';
import { CreateAccountDialog } from './CreateAccountDialog';

export function AccountsIndex() {
  const { profiles, sessions, launchProfile, stopProfile } = useControlPlane();
  const [creating, setCreating] = useState(false);

  const createButton = (
    <Button variant="primary" size="sm" icon={Plus} onClick={() => setCreating(true)}>
      New account
    </Button>
  );

  return (
    <Page
      trail={[{ label: 'Accounts' }]}
      meta={profiles.length > 0 ? `${profiles.length} Chrome profiles` : undefined}
      action={profiles.length > 0 ? createButton : undefined}
    >
      {profiles.length === 0 ? (
        <EmptyState
          icon={UserRound}
          message="An Account is an isolated Chrome profile signed into Google. Attach Accounts to Projects to run Colab notebooks safely."
          action={createButton}
        />
      ) : (
        <TableCard label="Registered Chrome account profiles">
          <thead>
            <tr>
              <Th>Account</Th>
              <Th className="w-28">Tier</Th>
              <Th className="w-40">Compute Units</Th>
              <Th className="w-36">Chrome process</Th>
              <Th className="w-28 text-right">Sessions</Th>
              <Th className="w-32 text-right">Updated</Th>
              <Th className="w-16 pl-0 text-right">
                <span className="sr-only">Process control</span>
              </Th>
            </tr>
          </thead>
          <TBody>
            {profiles.map((account) => {
              const attachedCount = sessions.filter((s) => s.profile_id === account.id).length;
              const lockedCount = sessions.filter(
                (s) => s.profile_id === account.id && s.is_locked,
              ).length;

              return (
                <tr key={account.id} className="transition-colors hover:bg-hover">
                  <Td className="py-2">
                    <a href={paths.account(account.id)} className="flex min-w-0 items-center gap-3">
                      <Avatar name={account.name} />
                      <span className="flex min-w-0 flex-col">
                        <span className="truncate font-medium text-fg hover:underline">
                          {account.name}
                        </span>
                        <span className="truncate font-mono text-caption text-subtle">
                          {account.account_email || `:${account.cdp_port}`}
                        </span>
                      </span>
                    </a>
                  </Td>
                  <Td className="text-muted">{formatTier(account.tier)}</Td>
                  <Td className="font-mono text-caption text-fg">
                    <div>{formatComputeUnits(account.ccu_balance)}</div>
                    <div className="text-subtle">{formatBurnRate(account.ccu_burn_rate)}</div>
                  </Td>
                  <Td>
                    <Status
                      tone={account.is_running ? 'solid' : 'hollow'}
                      label={
                        account.is_running
                          ? account.chrome_pid > 0
                            ? `PID ${account.chrome_pid}`
                            : 'Running'
                          : 'Stopped'
                      }
                    />
                  </Td>
                  <Td className="text-right font-mono text-caption tabular-nums text-muted">
                    {attachedCount}
                    {lockedCount > 0 ? ` (${lockedCount} ●)` : ''}
                  </Td>
                  <Td className="text-right text-muted">
                    {formatRelative(account.last_telemetry_at ?? account.updated_at) || NONE}
                  </Td>
                  <Td className="pl-0 text-right">
                    {account.is_running ? (
                      <IconButton
                        label={`Stop ${account.name} browser`}
                        onClick={() => void stopProfile(account.id)}
                      >
                        <Square aria-hidden className="size-4" strokeWidth={1.75} />
                      </IconButton>
                    ) : (
                      <IconButton
                        label={`Launch ${account.name} browser`}
                        onClick={() => void launchProfile(account.id)}
                      >
                        <Play aria-hidden className="size-4" strokeWidth={1.75} />
                      </IconButton>
                    )}
                  </Td>
                </tr>
              );
            })}
          </TBody>
        </TableCard>
      )}

      {creating && (
        <CreateAccountDialog
          onCreated={(id) => navigate(paths.account(id))}
          onClose={() => setCreating(false)}
        />
      )}
    </Page>
  );
}
