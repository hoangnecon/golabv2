import type { ObservationSnapshot, Profile } from '@/api/types';
import { Metrics } from '@/shared/layout/Detail';
import type { Metric } from '@/shared/layout/Detail';
import {
  NONE,
  formatBurnRate,
  formatComputeUnits,
  formatGbPair,
  formatMbAsGbPair,
  formatTier,
  formatUptime,
} from '@/shared/lib/format';
import { connectionLabel, connectionTone, driveStatusTone } from '@/shared/lib/status';
import { Meter } from '@/shared/ui/Meter';
import { Status } from '@/shared/ui/StatusDot';

interface TelemetryStripProps {
  observation: ObservationSnapshot | undefined;
  lockedAccount: Profile | undefined;
}

/**
 * 6-Card Read-Only Telemetry Strip for the Project's currently locked Account Session (Spec v2 §7.2):
 * 1. Compute Units · 2. Runtime & Uptime · 3. GPU / VRAM · 4. System RAM · 5. VM Disk · 6. Google Drive FUSE
 */
export function TelemetryStrip({ observation, lockedAccount }: TelemetryStripProps) {
  const cu = observation?.compute_units;
  const hw = observation?.hardware_telemetry;
  const meta = observation?.notebook_metadata;
  const drive = observation?.drive_status;

  const balance = cu?.balance ?? lockedAccount?.ccu_balance;
  const burn = cu?.consumption_rate_hourly ?? lockedAccount?.ccu_burn_rate;
  const tier = cu?.tier ?? lockedAccount?.tier;
  const eligible = cu?.eligible_gpus?.length
    ? cu.eligible_gpus.join(', ')
    : lockedAccount?.eligible_gpus?.join(', ') || NONE;

  const primaryGpu = hw?.gpus?.[0];
  const gpuFraction =
    primaryGpu && primaryGpu.memory_total_mb > 0
      ? primaryGpu.memory_used_mb / primaryGpu.memory_total_mb
      : null;

  const ram = hw?.ram;
  const ramFraction = ram && ram.total_gb > 0 ? ram.used_gb / ram.total_gb : null;

  const rootDisk = hw?.disks?.[0];
  const diskFraction =
    rootDisk && rootDisk.total_gb > 0 ? rootDisk.used_gb / rootDisk.total_gb : null;

  const driveFraction =
    drive?.total_gb && drive.total_gb > 0 && drive.used_gb !== undefined
      ? drive.used_gb / drive.total_gb
      : null;

  const items: readonly Metric[] = [
    {
      label: 'Compute Units',
      value: formatComputeUnits(balance),
      detail: `Burn ${formatBurnRate(burn)} · ${formatTier(tier)}`,
    },
    {
      label: 'Runtime & Uptime',
      value: (
        <Status
          tone={connectionTone(observation?.connection_state)}
          label={connectionLabel(observation?.connection_state)}
        />
      ),
      detail: `Uptime ${formatUptime(meta?.runtime_uptime_seconds)} · Eligible ${eligible}`,
    },
    {
      label: 'GPU / VRAM',
      value: primaryGpu
        ? primaryGpu.name
        : observation?.connection_state === 'idle' || observation?.connection_state === 'busy'
          ? 'CPU Runtime'
          : NONE,
      detail: primaryGpu
        ? formatMbAsGbPair(primaryGpu.memory_used_mb, primaryGpu.memory_total_mb)
        : meta?.accelerator_name || 'No GPU assigned',
      footer: gpuFraction !== null ? <Meter value={gpuFraction} label="GPU VRAM utilization" /> : undefined,
    },
    {
      label: 'System RAM',
      value: ram ? formatGbPair(ram.used_gb, ram.total_gb) : NONE,
      detail: ram ? `${ram.free_gb.toFixed(2)} GB free` : 'Kernel disconnected',
      footer: ramFraction !== null ? <Meter value={ramFraction} label="System RAM utilization" /> : undefined,
    },
    {
      label: 'VM Disk',
      value: rootDisk ? formatGbPair(rootDisk.used_gb, rootDisk.total_gb) : NONE,
      detail: rootDisk?.label || '/ (ephemeral SSD)',
      footer: diskFraction !== null ? <Meter value={diskFraction} label="VM disk utilization" /> : undefined,
    },
    {
      label: 'Google Drive FUSE',
      value: (
        <Status
          tone={driveStatusTone(drive?.status, drive?.mounted)}
          label={
            drive?.status === 'corrupted_local_dir'
              ? 'Shadow dir error'
              : drive?.status === 'drive_auth_pending'
                ? 'OAuth pending'
                : drive?.mounted
                  ? 'FUSE mounted'
                  : 'Unmounted'
          }
        />
      ),
      detail:
        drive?.mounted && drive.total_gb
          ? formatGbPair(drive.used_gb, drive.total_gb)
          : drive?.mount_path || '/content/drive',
      footer: driveFraction !== null ? <Meter value={driveFraction} label="Google Drive quota used" /> : undefined,
    },
  ];

  return <Metrics items={items} />;
}
