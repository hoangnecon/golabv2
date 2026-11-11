import { Plus } from 'lucide-react';
import { useState } from 'react';
import { useWorkspace } from '@/data/useWorkspace';
import { ROW } from '@/shared/layout/Sidebar';
import { cn } from '@/shared/lib/cn';
import { useLocation } from '@/shared/lib/location';
import { navigate, paths } from '@/shared/lib/paths';
import { useShell } from '@/shared/layout/ShellProvider';
import { SearchInput } from '@/shared/ui/Field';
import { IconButton } from '@/shared/ui/IconButton';
import { StatusDot } from '@/shared/ui/StatusDot';
import { CreateProjectDialog } from './CreateProjectDialog';

/**
 * Contextual Sidebar for the Projects section:
 * Searchable UUID-keyed project list showing `●` if locked (+ locked account short name)
 * or `○` if unlocked, plus a quick "New project" dialog trigger.
 */
export function ProjectsNav() {
  const { projects } = useWorkspace();
  const { segments } = useLocation();
  const { dismissOnCompact } = useShell();
  const [query, setQuery] = useState('');
  const [creating, setCreating] = useState(false);

  const selectedId = segments[0] === 'projects' ? segments[1] : undefined;
  const needle = query.trim().toLowerCase();
  const filtered = needle
    ? projects.filter(
        (p) =>
          p.name.toLowerCase().includes(needle) ||
          p.notebook_id.toLowerCase().includes(needle) ||
          (p.lockedAccount?.name ?? '').toLowerCase().includes(needle),
      )
    : projects;

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center justify-between">
        <a
          href={paths.projects}
          onClick={dismissOnCompact}
          className="font-mono text-caption font-semibold uppercase tracking-wide text-fg hover:underline"
        >
          Projects ({projects.length})
        </a>
        <IconButton size="sm" label="New project" onClick={() => setCreating(true)}>
          <Plus aria-hidden className="size-4" strokeWidth={1.75} />
        </IconButton>
      </div>

      <search aria-label="Filter projects">
        <SearchInput
          label="Search projects"
          placeholder="Filter notebooks…"
          value={query}
          onChange={(event) => setQuery(event.target.value)}
        />
      </search>

      {filtered.length === 0 ? (
        <p className="m-0 py-2 text-caption text-subtle">
          {projects.length === 0 ? 'No projects registered yet.' : 'No matching projects.'}
        </p>
      ) : (
        <ul className="flex flex-col gap-0.5">
          {filtered.map((project) => {
            const active = project.id === selectedId;
            const locked = Boolean(project.lockedSession);
            return (
              <li key={project.id}>
                <a
                  href={paths.project(project.id)}
                  aria-current={active ? 'page' : undefined}
                  onClick={dismissOnCompact}
                  className={cn(
                    ROW,
                    'gap-2.5 px-2.5',
                    active ? 'bg-hover font-medium text-fg' : 'text-muted hover:bg-hover hover:text-fg',
                  )}
                >
                  <StatusDot
                    tone={locked ? 'solid' : 'hollow'}
                    label={locked ? `Locked to ${project.lockedAccount?.name ?? 'account'}` : 'Unlocked'}
                  />
                  <span className="min-w-0 flex-1 truncate">{project.name}</span>
                  {project.lockedAccount && (
                    <span className="max-w-20 shrink-0 truncate font-mono text-caption text-subtle">
                      {project.lockedAccount.name}
                    </span>
                  )}
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
    </div>
  );
}
