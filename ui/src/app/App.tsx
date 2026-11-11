import { WifiOff } from 'lucide-react';
import { useEffect } from 'react';
import { ControlPlaneProvider, useControlPlane } from '@/data/ControlPlaneProvider';
import { Shell } from '@/shared/layout/Shell';
import { ShellProvider } from '@/shared/layout/ShellProvider';
import { NoticeProvider } from '@/shared/notice/NoticeProvider';
import { RAIL_ITEMS, useRoute } from './routes';

function Routed() {
  const { route, segments } = useRoute();
  const { online, daemonVersion } = useControlPlane();
  const Current = route.component;
  const CurrentSidebar = route.sidebar;

  useEffect(() => {
    document.title = `${route.label} · GoLab Control Plane`;
  }, [route.label]);

  return (
    <Shell
      items={RAIL_ITEMS}
      activeId={route.id}
      sidebarTitle={route.label}
      sidebar={<CurrentSidebar />}
      online={online}
      daemonVersion={daemonVersion}
    >
      {!online && (
        <p
          role="alert"
          className="m-0 flex h-9 shrink-0 items-center gap-2 border-b border-line bg-raised px-6 text-caption font-medium text-fg"
        >
          <WifiOff aria-hidden className="size-4" strokeWidth={1.75} />
          ▪ Connection to GoLab daemon SSE stream lost. Reconnecting automatically…
        </p>
      )}
      <Current segments={segments} />
    </Shell>
  );
}

export default function App() {
  return (
    <NoticeProvider>
      <ControlPlaneProvider>
        <ShellProvider>
          <Routed />
        </ShellProvider>
      </ControlPlaneProvider>
    </NoticeProvider>
  );
}
