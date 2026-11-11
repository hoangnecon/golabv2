import { ExternalLink, KeyRound, NotebookText, Plus, RefreshCw, Trash2 } from 'lucide-react';
import { useEffect, useState } from 'react';
import type { ActivityLog } from '@/api/types';
import { useControlPlane } from '@/data/ControlPlaneProvider';
import { useWorkspace } from '@/data/useWorkspace';
import { CallDetail } from '@/domain/logs/CallDetail';
import { CallsTable } from '@/domain/logs/CallsTable';
import { Section } from '@/shared/layout/Detail';
import { Page } from '@/shared/layout/Page';
import { navigate, paths } from '@/shared/lib/paths';
import { Button, ButtonLink } from '@/shared/ui/Button';
import { useConfirm } from '@/shared/ui/ConfirmDialog';
import { EmptyState } from '@/shared/ui/EmptyState';
import { Status } from '@/shared/ui/StatusDot';
import { AddSessionDialog } from './AddSessionDialog';
import { CellsOutlinePanel } from './CellsOutlinePanel';
import { McpConfigDialog } from './McpConfigDialog';
import { SessionMatrixTable } from './SessionMatrixTable';
import { TelemetryStrip } from './TelemetryStrip';

export function ProjectPage({ projectId }: { projectId: string }) {
  const { projectById, observationOf, callsOfProject } = useWorkspace();
  const { deleteProject, fetchSessionObservation } = useControlPlane();
  const { confirm, dialog: confirmDialog } = useConfirm();

  const [addingSession, setAddingSession] = useState(false);
  const [mcpModalOpen, setMcpModalOpen] = useState(false);
  const [selectedCallId, setSelectedCallId] = useState<number | null>(null);

  const project = projectById(projectId);
  const lockedSessionId = project?.lockedSession?.id;

  useEffect(() => {
    if (lockedSessionId) {
      void fetchSessionObservation(lockedSessionId, true);
    }
  }, [lockedSessionId, fetchSessionObservation]);

  useEffect(() => {
    setSelectedCallId(null);
  }, [projectId]);

  if (!project) {
    return (
      <Page trail={[{ label: 'Projects', href: paths.projects }, { label: 'Not found' }]}>
        <EmptyState icon={NotebookText} message="This project does not exist." />
      </Page>
    );
  }

  const observation = observationOf(lockedSessionId);
  const calls = callsOfProject(project.id);
  const selectedCall: ActivityLog | undefined =
    selectedCallId !== null ? calls.find((c) => c.id === selectedCallId) : undefined;

  function handleDeleteProject() {
    if (!project) return;
    confirm({
      title: 'Delete project',
      message: `Delete "${project.name}" and detach its ${project.sessions.length} account session(s)?`,
      confirmLabel: 'Delete project',
      onConfirm: async () => {
        if (await deleteProject(project.id, true)) {
          navigate(paths.projects);
        }
      },
    });
  }

  const lockBanner = project.lockedSession ? (
    <Status
      tone="solid"
      label={`Locked: ${project.lockedAccount?.account_email || project.lockedAccount?.name || project.lockedSession.profile_id}`}
    />
  ) : (
    <Status tone="hollow" label="Unlocked" />
  );

  return (
    <Page
      trail={[{ label: 'Projects', href: paths.projects }, { label: project.name }]}
      meta={lockBanner}
      action={
        <>
          <ButtonLink
            size="sm"
            variant="secondary"
            icon={ExternalLink}
            href={project.notebook_url}
            target="_blank"
            rel="noreferrer"
          >
            Open in Colab
          </ButtonLink>
          <Button
            size="sm"
            variant="primary"
            icon={KeyRound}
            onClick={() => setMcpModalOpen(true)}
          >
            MCP Config
          </Button>
          <Button size="sm" variant="ghost" icon={Trash2} onClick={handleDeleteProject}>
            Delete
          </Button>
        </>
      }
      inspector={selectedCall ? <CallDetail entry={selectedCall} /> : undefined}
      inspectorTitle={selectedCall ? `Call #${selectedCall.id}` : 'Payload inspector'}
      onCloseInspector={() => setSelectedCallId(null)}
    >
      <div className="flex flex-col gap-8">
        <div className="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-line bg-surface px-4 py-2.5 text-caption">
          <span className="min-w-0 truncate font-mono text-muted">{project.notebook_url}</span>
          <span className="shrink-0 font-mono text-subtle">
            Token: {project.project_token.slice(0, 12)}…
          </span>
        </div>

        {/* Section A: Multi-Account Editor Matrix */}
        <Section
          title="Account sessions & exclusive lock"
          count={project.sessions.length}
          action={
            <Button size="sm" variant="secondary" icon={Plus} onClick={() => setAddingSession(true)}>
              Attach account
            </Button>
          }
        >
          <SessionMatrixTable
            project={project}
            lockedDriveStatus={observation?.drive_status}
          />
        </Section>

        {/* Section B: Read-Only Live Telemetry & Notebook Snapshot for Locked Account */}
        <Section
          title="Live locked session observability (read-only)"
          action={
            lockedSessionId ? (
              <Button
                size="sm"
                variant="ghost"
                icon={RefreshCw}
                onClick={() => void fetchSessionObservation(lockedSessionId, false)}
              >
                Refresh snapshot
              </Button>
            ) : undefined
          }
        >
          {project.lockedSession ? (
            <div className="flex flex-col gap-4">
              <TelemetryStrip
                observation={observation}
                lockedAccount={project.lockedAccount}
              />
              <CellsOutlinePanel
                outline={observation?.cells_outline}
                running={observation?.running_cells}
              />
            </div>
          ) : (
            <EmptyState message="Project is currently unlocked. Lock one Account Session above to enable MCP routing and live read-only telemetry." />
          )}
        </Section>

        {/* Section C: Recent MCP Tool Calls */}
        <Section title="Recent MCP tool calls" count={calls.length}>
          {calls.length === 0 ? (
            <EmptyState message="No MCP tool calls recorded for this project yet." />
          ) : (
            <CallsTable
              entries={calls}
              selectedId={selectedCallId}
              onSelect={(entry) =>
                setSelectedCallId((prev) => (prev === entry.id ? null : entry.id))
              }
              hideProject
            />
          )}
        </Section>
      </div>

      {addingSession && (
        <AddSessionDialog project={project} onClose={() => setAddingSession(false)} />
      )}
      {mcpModalOpen && (
        <McpConfigDialog project={project} onClose={() => setMcpModalOpen(false)} />
      )}
      {confirmDialog}
    </Page>
  );
}
