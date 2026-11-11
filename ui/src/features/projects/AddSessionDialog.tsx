import { useState } from 'react';
import type { FormEvent } from 'react';
import { errorMessage } from '@/api/http';
import { useControlPlane } from '@/data/ControlPlaneProvider';
import type { ProjectView } from '@/data/useWorkspace';
import { formatComputeUnits, formatTier } from '@/shared/lib/format';
import { paths } from '@/shared/lib/paths';
import { Button } from '@/shared/ui/Button';
import { Dialog, DialogActions } from '@/shared/ui/Dialog';
import { Checkbox, FormError, Input, Select } from '@/shared/ui/Field';

interface AddSessionDialogProps {
  project: ProjectView;
  onClose: () => void;
}

export function AddSessionDialog({ project, onClose }: AddSessionDialogProps) {
  const { profiles, createSession } = useControlPlane();
  const attachedIds = new Set(project.sessions.map((s) => s.profile_id));
  const available = profiles.filter((p) => !attachedIds.has(p.id));

  const [profileId, setProfileId] = useState(available[0]?.id ?? '');
  const [name, setName] = useState('');
  const [lockNow, setLockNow] = useState(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!profileId) return;
    setPending(true);
    setError(null);
    try {
      await createSession({
        project_id: project.id,
        profile_id: profileId,
        name: name.trim() || undefined,
        lock: lockNow,
      });
      onClose();
    } catch (failure) {
      setError(errorMessage(failure));
      setPending(false);
    }
  }

  return (
    <Dialog title="Attach account session" onClose={onClose}>
      <form onSubmit={handleSubmit} className="flex flex-col gap-4">
        {available.length === 0 ? (
          <p className="text-body text-muted">
            Every registered Google Account is already attached to this project.{' '}
            <a href={paths.accounts} className="font-medium text-fg underline underline-offset-2" onClick={onClose}>
              Add another account
            </a>
            .
          </p>
        ) : (
          <>
            <Select
              label="Account profile"
              required
              value={profileId}
              disabled={pending}
              onChange={(event) => setProfileId(event.target.value)}
            >
              {available.map((account) => (
                <option key={account.id} value={account.id}>
                  {account.name} · {account.account_email || formatTier(account.tier)} ·{' '}
                  {formatComputeUnits(account.ccu_balance)}
                </option>
              ))}
            </Select>
            <Input
              label="Session label"
              hint="Optional"
              value={name}
              disabled={pending}
              onChange={(event) => setName(event.target.value)}
              placeholder="Editor Account #2"
            />
            {!project.lockedSession && (
              <Checkbox
                label="Lock this session immediately"
                checked={lockNow}
                disabled={pending}
                onChange={(event) => setLockNow(event.target.checked)}
              />
            )}
          </>
        )}
        {error && <FormError>{error}</FormError>}
        <DialogActions>
          <Button variant="ghost" onClick={onClose} disabled={pending}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" disabled={pending || !profileId}>
            {pending ? 'Attaching…' : 'Attach account'}
          </Button>
        </DialogActions>
      </form>
    </Dialog>
  );
}
