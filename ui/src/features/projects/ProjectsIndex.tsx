import { NotebookText, Plus } from 'lucide-react';
import { useState } from 'react';
import { useWorkspace } from '@/data/useWorkspace';
import { Page } from '@/shared/layout/Page';
import { NONE, formatRelative } from '@/shared/lib/format';
import { navigate, paths } from '@/shared/lib/paths';
import { connectionLabel, connectionTone, permissionLabel, permissionTone } from '@/shared/lib/status';
import { Button } from '@/shared/ui/Button';
import { EmptyState } from '@/shared/ui/EmptyState';
import { Status } from '@/shared/ui/StatusDot';
import { CreateProjectDialog } from './CreateProjectDialog';

export function ProjectsIndex() {
  const { projects } = useWorkspace();
  const [creating, setCreating] = useState(false);

  const createButton = (
    <Button variant="primary" size="sm" icon={Plus} onClick={() => setCreating(true)}>
      New project
    </Button>
  );

  return (
    <Page
      trail={[{ label: 'Projects' }]}
      meta={projects.length > 0 ? `${projects.length} canonical notebooks` : undefined}
      action={projects.length > 0 ? createButton : undefined}
    >
      {projects.length === 0 ? (
        <EmptyState
          icon={NotebookText}
          message="Register a canonical Colab notebook as a Project, attach Google Accounts, and lock one Account for MCP execution."
          action={createButton}
        />
      ) : (
        <ul className="cards-grid">
          {projects.map((project) => {
            const locked = project.lockedSession;
            return (
              <li key={project.id}>
                <a
                  href={paths.project(project.id)}
                  className="flex h-full flex-col justify-between gap-4 rounded-xl border border-line bg-surface p-4 transition-colors hover:border-line-strong hover:bg-hover"
                >
                  <div className="flex min-w-0 flex-col gap-1.5">
                    <div className="flex items-center justify-between gap-2">
                      <span className="min-w-0 truncate text-title font-semibold text-fg">
                        {project.name}
                      </span>
                      <Status
                        tone={locked ? 'solid' : 'hollow'}
                        label={locked ? 'Locked' : 'Unlocked'}
                      />
                    </div>
                    <span className="truncate font-mono text-caption text-subtle">
                      {project.notebook_id}
                    </span>
                  </div>

                  <div className="flex flex-col gap-1.5 border-t border-line pt-3 text-caption">
                    <div className="flex items-center justify-between gap-2">
                      <span className="text-subtle">Active editor</span>
                      <span className="truncate font-mono text-fg">
                        {project.lockedAccount
                          ? project.lockedAccount.account_email || project.lockedAccount.name
                          : NONE}
                      </span>
                    </div>
                    <div className="flex items-center justify-between gap-2">
                      <span className="text-subtle">Runtime</span>
                      {locked ? (
                        <Status
                          tone={connectionTone(locked.connection_state)}
                          label={connectionLabel(locked.connection_state)}
                        />
                      ) : (
                        <span className="font-mono text-subtle">Idle / Unlocked</span>
                      )}
                    </div>
                    {locked && locked.permission !== 'unknown' && (
                      <div className="flex items-center justify-between gap-2">
                        <span className="text-subtle">Permission</span>
                        <Status
                          tone={permissionTone(locked.permission)}
                          label={permissionLabel(locked.permission)}
                        />
                      </div>
                    )}
                    <div className="flex items-center justify-between gap-2 pt-1 text-subtle">
                      <span className="font-mono tabular-nums">
                        {project.sessions.length} {project.sessions.length === 1 ? 'account' : 'accounts'}
                      </span>
                      <span>{formatRelative(project.updated_at)}</span>
                    </div>
                  </div>
                </a>
              </li>
            );
          })}
        </ul>
      )}

      {creating && (
        <CreateProjectDialog
          onCreated={(id) => navigate(paths.project(id))}
          onClose={() => setCreating(false)}
        />
      )}
    </Page>
  );
}
