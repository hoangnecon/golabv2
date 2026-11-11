import type { RouteProps } from '@/shared/lib/location';
import { ProjectPage } from './ProjectPage';
import { ProjectsIndex } from './ProjectsIndex';

/** `#/projects` · `#/projects/<projectId>` */
export function ProjectsRoute({ segments }: RouteProps) {
  const [projectId] = segments;
  return projectId ? <ProjectPage projectId={projectId} /> : <ProjectsIndex />;
}
