import { RotateCcw } from 'lucide-react';
import { useControlPlane } from '@/data/ControlPlaneProvider';
import { replaceParams, useLocation } from '@/shared/lib/location';
import { Button } from '@/shared/ui/Button';
import { SearchInput, Select } from '@/shared/ui/Field';

/**
 * Contextual Sidebar for Global Audit Logs (`<search>` landmark):
 * Faceted filters synced to `#/logs?project=&account=&session=&status=&tool=&q=`.
 */
export function LogsNav() {
  const { projects, profiles, sessions, activity } = useControlPlane();
  const { params } = useLocation();

  const q = params.get('q') ?? '';
  const project = params.get('project') ?? '';
  const account = params.get('account') ?? '';
  const session = params.get('session') ?? '';
  const status = params.get('status') ?? '';
  const tool = params.get('tool') ?? '';

  const toolNames = [...new Set(activity.map((entry) => entry.tool_name))].sort();
  const filteredSessions = project
    ? sessions.filter((s) => s.project_id === project)
    : sessions;

  const hasFilters = Boolean(q || project || account || session || status || tool);

  return (
    <search aria-label="Audit log filters" className="flex flex-col gap-3">
      <div className="flex items-center justify-between">
        <span className="font-mono text-caption font-semibold uppercase tracking-wide text-fg">
          Log filters
        </span>
        {hasFilters && (
          <Button
            size="sm"
            variant="ghost"
            icon={RotateCcw}
            onClick={() =>
              replaceParams({
                q: '',
                project: '',
                account: '',
                session: '',
                status: '',
                tool: '',
              })
            }
          >
            Reset
          </Button>
        )}
      </div>

      <SearchInput
        label="Search tool or error"
        placeholder="Tool, error code, message…"
        value={q}
        onChange={(event) => replaceParams({ q: event.target.value })}
      />

      <Select
        label="Project"
        value={project}
        onChange={(event) => replaceParams({ project: event.target.value, session: '' })}
      >
        <option value="">All projects</option>
        {projects.map((p) => (
          <option key={p.id} value={p.id}>
            {p.name}
          </option>
        ))}
      </Select>

      <Select
        label="Account"
        value={account}
        onChange={(event) => replaceParams({ account: event.target.value })}
      >
        <option value="">All accounts</option>
        {profiles.map((p) => (
          <option key={p.id} value={p.id}>
            {p.name}
          </option>
        ))}
      </Select>

      <Select
        label="Session"
        value={session}
        onChange={(event) => replaceParams({ session: event.target.value })}
      >
        <option value="">All sessions</option>
        {filteredSessions.map((s) => (
          <option key={s.id} value={s.id}>
            {s.name}
          </option>
        ))}
      </Select>

      <Select
        label="Status"
        value={status}
        onChange={(event) => replaceParams({ status: event.target.value })}
      >
        <option value="">All statuses</option>
        <option value="success">● Success</option>
        <option value="error">▪ Error</option>
      </Select>

      <Select
        label="Tool name"
        value={tool}
        onChange={(event) => replaceParams({ tool: event.target.value })}
      >
        <option value="">All 43 MCP tools</option>
        {toolNames.map((name) => (
          <option key={name} value={name}>
            {name}
          </option>
        ))}
      </Select>
    </search>
  );
}
