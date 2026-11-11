import { ArrowLeftRight, Copy, ExternalLink, Lock, Trash2, Unlock } from 'lucide-react';
import type { DriveStatus, Session } from '@/api/types';
import { useControlPlane } from '@/data/ControlPlaneProvider';
import type { ProjectView } from '@/data/useWorkspace';
import { useWorkspace } from '@/data/useWorkspace';
import { formatComputeUnits, formatTier } from '@/shared/lib/format';
import { paths } from '@/shared/lib/paths';
import {
  connectionLabel,
  connectionTone,
  permissionLabel,
  permissionRemediation,
  permissionTone,
} from '@/shared/lib/status';
import { useNotice } from '@/shared/notice/NoticeProvider';
import { Button } from '@/shared/ui/Button';
import { useConfirm } from '@/shared/ui/ConfirmDialog';
import { IconButton } from '@/shared/ui/IconButton';
import { Status } from '@/shared/ui/StatusDot';
import { TBody, TableCard, Td, Th } from '@/shared/ui/Table';

interface SessionMatrixTableProps {
  project: ProjectView;
  lockedDriveStatus?: DriveStatus;
}

/**
 * Multi-Account Editor Matrix (Spec v2 §7.2 Section A):
 * Enforces Per-Project Single-Account Exclusive Lock, surfaces read-only/denied/OAuth
 * remediation banners, and provides Session Token (`gls_...`) & Chrome Tab governance.
 */
export function SessionMatrixTable({ project, lockedDriveStatus }: SessionMatrixTableProps) {
  const { accountById } = useWorkspace();
  const { lockSession, unlockSession, openSessionTab, deleteSession } = useControlPlane();
  const { notify, notifyError } = useNotice();
  const { confirm, dialog: confirmDialog } = useConfirm();

  const lockedSession = project.lockedSession;

  function handleLockOrSwitch(session: Session) {
    if (lockedSession && lockedSession.id !== session.id) {
      const prevAcct = accountById(lockedSession.profile_id);
      const nextAcct = accountById(session.profile_id);
      confirm({
        title: 'Switch exclusive project lock',
        message: `GoLab will flush SaveNotebook on "${prevAcct?.name ?? lockedSession.profile_id}", close its Colab tab to prevent multi-account merge conflicts, and lock "${nextAcct?.name ?? session.profile_id}" on "${project.name}".`,
        confirmLabel: 'Switch lock',
        onConfirm: async () => {
          await lockSession(session.id, { force_switch: true, open_tab: true });
        },
      });
      return;
    }
    void lockSession(session.id, { force_switch: false, open_tab: true });
  }

  function handleRemove(session: Session) {
    const acct = accountById(session.profile_id);
    confirm({
      title: 'Detach account session',
      message: `Remove "${acct?.name ?? session.name}" from "${project.name}" and close its browser tab?`,
      confirmLabel: 'Remove session',
      onConfirm: async () => {
        await deleteSession(session.id, true);
      },
    });
  }

  function copySessionToken(session: Session) {
    navigator.clipboard.writeText(session.session_token).then(
      () => notify('Session token (gls_) copied'),
      () => notifyError('Clipboard is not available'),
    );
  }

  const warningSessions = project.sessions.filter(
    (s) =>
      s.permission === 'view_only' ||
      s.permission === 'commenter' ||
      s.permission === 'denied' ||
      s.permission === 'auth_required',
  );

  const showDriveAlert =
    lockedDriveStatus &&
    (lockedDriveStatus.status === 'drive_auth_pending' ||
      lockedDriveStatus.status === 'corrupted_local_dir');

  return (
    <div className="flex flex-col gap-3">
      <TableCard label="Project account sessions matrix">
        <thead>
          <tr>
            <Th className="w-36">Lock</Th>
            <Th>Account</Th>
            <Th className="w-40">Permission</Th>
            <Th className="w-44">Connection layer</Th>
            <Th className="w-64 text-right">Governance</Th>
          </tr>
        </thead>
        <TBody>
          {project.sessions.map((session) => {
            const account = accountById(session.profile_id);
            const isLocked = project.locked_session_id === session.id || session.is_locked;
            const anotherLocked = Boolean(lockedSession && !isLocked);

            return (
              <tr key={session.id} className="transition-colors hover:bg-hover">
                <Td>
                  {isLocked ? (
                    <Status tone="solid" label="Locked" />
                  ) : (
                    <Status tone="hollow" label="Unlocked" />
                  )}
                </Td>
                <Td>
                  <div className="flex min-w-0 flex-col">
                    {account ? (
                      <a
                        href={paths.account(account.id)}
                        className="truncate font-medium text-fg hover:underline"
                      >
                        {account.name}
                      </a>
                    ) : (
                      <span className="truncate font-medium text-fg">{session.name}</span>
                    )}
                    <span className="truncate font-mono text-caption text-subtle">
                      {account
                        ? `${account.account_email || 'Not signed in'} · ${formatTier(account.tier)} · ${formatComputeUnits(account.ccu_balance)}`
                        : session.profile_id}
                    </span>
                  </div>
                </Td>
                <Td>
                  <Status
                    tone={permissionTone(session.permission)}
                    label={permissionLabel(session.permission)}
                  />
                </Td>
                <Td>
                  <Status
                    tone={connectionTone(session.connection_state)}
                    label={connectionLabel(session.connection_state)}
                  />
                </Td>
                <Td className="text-right">
                  <div className="inline-flex items-center justify-end gap-1.5">
                    {isLocked ? (
                      <Button
                        size="sm"
                        variant="secondary"
                        icon={Unlock}
                        onClick={() => void unlockSession(session.id)}
                      >
                        Unlock
                      </Button>
                    ) : (
                      <Button
                        size="sm"
                        variant={anotherLocked ? 'secondary' : 'primary'}
                        icon={anotherLocked ? ArrowLeftRight : Lock}
                        onClick={() => handleLockOrSwitch(session)}
                      >
                        {anotherLocked ? 'Switch lock' : 'Lock'}
                      </Button>
                    )}
                    <IconButton
                      size="sm"
                      label={`Open notebook tab in ${account?.name ?? 'Chrome'}`}
                      onClick={() => void openSessionTab(session.id)}
                    >
                      <ExternalLink aria-hidden className="size-4" strokeWidth={1.75} />
                    </IconButton>
                    <IconButton
                      size="sm"
                      label={`Copy session token (gls_) for ${account?.name ?? session.name}`}
                      onClick={() => copySessionToken(session)}
                    >
                      <Copy aria-hidden className="size-4" strokeWidth={1.75} />
                    </IconButton>
                    <IconButton
                      size="sm"
                      label={`Remove ${account?.name ?? session.name} session`}
                      onClick={() => handleRemove(session)}
                    >
                      <Trash2 aria-hidden className="size-4" strokeWidth={1.75} />
                    </IconButton>
                  </div>
                </Td>
              </tr>
            );
          })}
        </TBody>
      </TableCard>

      {warningSessions.map((session) => {
        const account = accountById(session.profile_id);
        const hint = permissionRemediation(session.permission);
        if (!hint) return null;
        return (
          <div
            key={`perm-${session.id}`}
            role="status"
            className="flex flex-col gap-1 rounded-lg border border-line-strong bg-raised px-4 py-3 text-caption text-fg"
          >
            <div className="font-mono font-semibold uppercase">
              △ {account?.name ?? session.profile_id} · {permissionLabel(session.permission)}
            </div>
            <p className="m-0 text-muted">{hint}</p>
          </div>
        );
      })}

      {showDriveAlert && lockedDriveStatus && (
        <div
          role="alert"
          className="flex flex-col gap-1 rounded-lg border border-line-strong bg-raised px-4 py-3 text-caption text-fg"
        >
          <div className="font-mono font-semibold uppercase">
            {lockedDriveStatus.status === 'corrupted_local_dir'
              ? '▪ GOLAB_DRIVE_LOCAL_SHADOW_DIR'
              : '△ GOOGLE DRIVE OAUTH CONSENT REQUIRED'}
          </div>
          <p className="m-0 text-muted">
            {lockedDriveStatus.action_required || lockedDriveStatus.message}
          </p>
        </div>
      )}

      {confirmDialog}
    </div>
  );
}
