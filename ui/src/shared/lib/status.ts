import type { ConnectionState, PermissionLevel } from '@/api/types';
import type { Tone } from '@/shared/ui/StatusDot';

export function connectionTone(state: ConnectionState | undefined): Tone {
  switch (state) {
    case 'idle':
      return 'solid';
    case 'busy':
    case 'connecting':
      return 'active';
    case 'auth_required':
    case 'permission_denied':
      return 'warn';
    case 'browser_stopped':
    case 'tab_closed':
    case 'runtime_disconnected':
    default:
      return 'hollow';
  }
}

export function connectionLabel(state: ConnectionState | undefined): string {
  switch (state) {
    case 'browser_stopped':
      return 'Browser stopped';
    case 'tab_closed':
      return 'Tab closed';
    case 'auth_required':
      return 'Sign-in required';
    case 'permission_denied':
      return 'Access denied';
    case 'runtime_disconnected':
      return 'VM disconnected';
    case 'connecting':
      return 'Connecting VM';
    case 'idle':
      return 'Idle';
    case 'busy':
      return 'Busy';
    default:
      return 'Unknown';
  }
}

export function permissionTone(permission: PermissionLevel | undefined): Tone {
  switch (permission) {
    case 'owner':
    case 'editor':
      return 'solid';
    case 'commenter':
    case 'view_only':
    case 'denied':
    case 'auth_required':
      return 'warn';
    case 'unknown':
    default:
      return 'hollow';
  }
}

export function permissionLabel(permission: PermissionLevel | undefined): string {
  switch (permission) {
    case 'owner':
      return 'Owner';
    case 'editor':
      return 'Editor';
    case 'commenter':
      return 'Commenter';
    case 'view_only':
      return 'View only';
    case 'denied':
      return 'Access denied';
    case 'auth_required':
      return 'Auth required';
    case 'unknown':
    default:
      return 'Unprobed';
  }
}

export function permissionRemediation(permission: PermissionLevel | undefined): string | null {
  switch (permission) {
    case 'view_only':
    case 'commenter':
      return 'Read-only access detected (`view_only`). Observation tools work when locked, but mutating MCP tools (`colab_add_cell`, `colab_update_cell`, `colab_save_notebook`, `colab_mount_drive`) return `GOLAB_PERMISSION_READ_ONLY`. Share this notebook with Editor permission in Google Drive to enable writes.';
    case 'denied':
      return 'Google Drive denied access to this notebook for this account (`GOLAB_PERMISSION_DENIED`). Share the notebook with this Google Account or remove the session.';
    case 'auth_required':
      return 'This Chrome profile redirected to Google Sign-In (`GOLAB_AUTH_REQUIRED`). Click Open Tab to sign into Google inside this isolated Chrome profile.';
    default:
      return null;
  }
}

export function driveStatusTone(status: string | undefined, mounted: boolean | undefined): Tone {
  if (status === 'corrupted_local_dir') return 'error';
  if (status === 'drive_auth_pending') return 'warn';
  if (mounted || status === 'mounted') return 'solid';
  return 'hollow';
}
