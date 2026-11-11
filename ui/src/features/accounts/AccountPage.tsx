import { Pencil, Play, RefreshCw, Square, Trash2, UserRound } from 'lucide-react';
import { useEffect, useState } from 'react';
import type { ActivityLog } from '@/api/types';
import { useControlPlane } from '@/data/ControlPlaneProvider';
import { useWorkspace } from '@/data/useWorkspace';
import { CallDetail } from '@/domain/logs/CallDetail';
import { CallsTable } from '@/domain/logs/CallsTable';
import { Detail, Metrics, Panel, PanelActions, PanelBody, Section } from '@/shared/layout/Detail';
import { Page } from '@/shared/layout/Page';
import {
  NONE,
  formatBurnRate,
  formatComputeUnits,
  formatTier,
  formatTimestamp,
} from '@/shared/lib/format';
import { navigate, paths } from '@/shared/lib/paths';
import { connectionLabel, connectionTone, permissionLabel, permissionTone } from '@/shared/lib/status';
import { Button } from '@/shared/ui/Button';
import { useConfirm } from '@/shared/ui/ConfirmDialog';
import { EmptyState } from '@/shared/ui/EmptyState';
import { PropertyList } from '@/shared/ui/PropertyList';
import { Status } from '@/shared/ui/StatusDot';
import { TBody, TableCard, Td, Th } from '@/shared/ui/Table';
import { EditAccountDialog } from './EditAccountDialog';

const PROTECTED_ACCOUNT = 'default';

export function AccountPage({ accountId }: { accountId: string }) {
  const { accountById, sessionsOfAccount, projectById, quotaOf, callsOfAccount } = useWorkspace();
  const { launchProfile, stopProfile, deleteProfile, refreshQuota } = useControlPlane();
  const { confirm, dialog: confirmDialog } = useConfirm();

  const [editing, setEditing] = useState(false);
  const [selectedCallId, setSelectedCallId] = useState<number | null>(null);

  const account = accountById(accountId);

  useEffect(() => {
    if (account?.is_running) {
      void refreshQuota(account.id, true);
    }
  }, [account?.id, account?.is_running, refreshQuota]);

  useEffect(() => {
    setSelectedCallId(null);
  }, [accountId]);

  if (!account) {
    return (
      <Page trail={[{ label: 'Accounts', href: paths.accounts }, { label: 'Not found' }]}>
        <EmptyState icon={UserRound} message="This account profile does not exist." />
      </Page>
    );
  }

  const attachedSessions = sessionsOfAccount(account.id);
  const holdsLock = attachedSessions.some((s) => s.is_locked);
  const quota = quotaOf(account.id);
  const activeVms = quota?.active_vms ?? [];
  const calls = callsOfAccount(account.id);
  const selectedCall: ActivityLog | undefined =
    selectedCallId !== null ? calls.find((c) => c.id === selectedCallId) : undefined;

  const cu = quota?.compute_units;
  const balance = cu?.balance ?? account.ccu_balance;
  const burn = cu?.consumption_rate_hourly ?? account.ccu_burn_rate;
  const eligibleGpus = cu?.eligible_gpus?.length ? cu.eligible_gpus : account.eligible_gpus;
  const eligibleTpus = cu?.eligible_tpus?.length ? cu.eligible_tpus : account.eligible_tpus;

  const deleteDisabledReason =
    account.id === PROTECTED_ACCOUNT
      ? 'The default account profile cannot be deleted.'
      : holdsLock
        ? 'Unlock or switch active Project Locks before deleting this account.'
        : undefined;

  function handleDelete() {
    if (!account || deleteDisabledReason) return;
    confirm({
      title: 'Delete account profile',
      message: `Remove "${account.name}" and detach its ${attachedSessions.length} project session(s)?`,
      confirmLabel: 'Delete account',
      onConfirm: async () => {
        if (await deleteProfile(account.id)) {
          navigate(paths.accounts);
        }
      },
    });
  }

  return (
    <Page
      trail={[{ label: 'Accounts', href: paths.accounts }, { label: account.name }]}
      meta={
        <Status
          tone={account.is_running ? 'solid' : 'hollow'}
          label={
            account.is_running
              ? account.chrome_pid > 0
                ? `Running PID ${account.chrome_pid}`
                : 'Running'
              : 'Stopped'
          }
        />
      }
      action={
        <>
          <Button
            size="sm"
            variant="secondary"
            icon={RefreshCw}
            onClick={() => void refreshQuota(account.id, false)}
          >
            Refresh quota
          </Button>
          {account.is_running ? (
            <Button
              size="sm"
              variant="secondary"
              icon={Square}
              onClick={() => void stopProfile(account.id)}
            >
              Stop browser
            </Button>
          ) : (
            <Button
              size="sm"
              variant="primary"
              icon={Play}
              onClick={() => void launchProfile(account.id)}
            >
              Launch browser
            </Button>
          )}
        </>
      }
      inspector={selectedCall ? <CallDetail entry={selectedCall} /> : undefined}
      inspectorTitle={selectedCall ? `Call #${selectedCall.id}` : 'Payload inspector'}
      onCloseInspector={() => setSelectedCallId(null)}
    >
      <div className="flex flex-col gap-8">
        <Metrics
          items={[
            {
              label: 'Compute Units',
              value: formatComputeUnits(balance),
              detail: `Burn ${formatBurnRate(burn)} · ${formatTier(cu?.tier ?? account.tier)}`,
            },
            {
              label: 'Eligible GPUs',
              value: eligibleGpus.length > 0 ? eligibleGpus.join(', ') : NONE,
              detail: `Eligible TPUs: ${eligibleTpus.length > 0 ? eligibleTpus.join(', ') : NONE}`,
            },
            {
              label: 'Active Colab VMs',
              value: String(activeVms.length),
              detail: `${attachedSessions.length} attached project session(s)`,
            },
          ]}
        />

        <Detail
          main={
            <>
              <Section title="Active Colab VM assignments" count={activeVms.length}>
                {activeVms.length === 0 ? (
                  <EmptyState message="No active Colab VM assignments reported for this Google Account. Launch Chrome and click Refresh Quota while a Colab tab is open." />
                ) : (
                  <TableCard label="Active Colab VM assignments">
                    <thead>
                      <tr>
                        <Th>Notebook</Th>
                        <Th className="w-36">Accelerator</Th>
                        <Th className="w-32">Shape</Th>
                        <Th className="w-36">Kernel ID</Th>
                      </tr>
                    </thead>
                    <TBody>
                      {activeVms.map((vm) => (
                        <tr key={vm.session_id || vm.kernel_id || vm.file_id}>
                          <Td>
                            <div className="flex flex-col">
                              <span className="font-medium text-fg">{vm.title || vm.file_id}</span>
                              <span className="font-mono text-caption text-subtle">{vm.file_id}</span>
                            </div>
                          </Td>
                          <Td className="font-mono text-caption text-fg">
                            {vm.accelerator_model || vm.accelerator || 'CPU'}
                          </Td>
                          <Td className="font-mono text-caption text-muted">{vm.shape || NONE}</Td>
                          <Td className="font-mono text-caption text-subtle">
                            {vm.kernel_id ? vm.kernel_id.slice(0, 12) : NONE}
                          </Td>
                        </tr>
                      ))}
                    </TBody>
                  </TableCard>
                )}
              </Section>

              <Section title="Attached project sessions" count={attachedSessions.length}>
                {attachedSessions.length === 0 ? (
                  <EmptyState message="This account is not attached to any project yet." />
                ) : (
                  <TableCard label="Attached project sessions">
                    <thead>
                      <tr>
                        <Th>Project</Th>
                        <Th className="w-32">Lock</Th>
                        <Th className="w-36">Permission</Th>
                        <Th className="w-40">Connection</Th>
                      </tr>
                    </thead>
                    <TBody>
                      {attachedSessions.map((session) => {
                        const project = projectById(session.project_id);
                        return (
                          <tr key={session.id} className="transition-colors hover:bg-hover">
                            <Td>
                              {project ? (
                                <a
                                  href={paths.project(project.id)}
                                  className="font-medium text-fg hover:underline"
                                >
                                  {project.name}
                                </a>
                              ) : (
                                <span className="font-mono text-caption">{session.project_id}</span>
                              )}
                            </Td>
                            <Td>
                              <Status
                                tone={session.is_locked ? 'solid' : 'hollow'}
                                label={session.is_locked ? 'Locked' : 'Unlocked'}
                              />
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
                          </tr>
                        );
                      })}
                    </TBody>
                  </TableCard>
                )}
              </Section>

              <Section title="Account MCP tool calls" count={calls.length}>
                {calls.length === 0 ? (
                  <EmptyState message="No MCP tool calls recorded for this account." />
                ) : (
                  <CallsTable
                    entries={calls}
                    selectedId={selectedCallId}
                    onSelect={(entry) =>
                      setSelectedCallId((prev) => (prev === entry.id ? null : entry.id))
                    }
                    hideAccount
                  />
                )}
              </Section>
            </>
          }
          rail={
            <>
              <Section title="Profile metadata">
                <Panel>
                  <PanelBody>
                    <PropertyList
                      items={[
                        { term: 'Google email', value: account.account_email || NONE },
                        { term: 'Display name', value: account.display_name || NONE },
                        {
                          term: 'CDP port',
                          value: <span className="font-mono text-caption">:{account.cdp_port}</span>,
                        },
                        {
                          term: 'Chrome PID',
                          value: (
                            <span className="font-mono text-caption">
                              {account.chrome_pid > 0 ? account.chrome_pid : NONE}
                            </span>
                          ),
                        },
                        {
                          term: 'User data dir',
                          value: (
                            <span className="truncate font-mono text-caption">
                              {account.user_data_dir}
                            </span>
                          ),
                        },
                        { term: 'Created', value: formatTimestamp(account.created_at) },
                      ]}
                    />
                  </PanelBody>
                </Panel>
              </Section>

              <Section title="Governance">
                <Panel>
                  <PanelActions>
                    <Button variant="menu" icon={Pencil} onClick={() => setEditing(true)}>
                      Edit account metadata
                    </Button>
                    <Button
                      variant="menu"
                      icon={RefreshCw}
                      onClick={() => void refreshQuota(account.id, false)}
                    >
                      Sync CCU & active VMs
                    </Button>
                  </PanelActions>
                  <PanelActions separated>
                    <Button
                      variant="menu"
                      icon={Trash2}
                      disabled={Boolean(deleteDisabledReason)}
                      title={deleteDisabledReason}
                      onClick={handleDelete}
                    >
                      Delete account
                    </Button>
                  </PanelActions>
                </Panel>
              </Section>
            </>
          }
        />
      </div>

      {editing && <EditAccountDialog account={account} onClose={() => setEditing(false)} />}
      {confirmDialog}
    </Page>
  );
}
