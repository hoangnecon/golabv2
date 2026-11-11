import { useState } from 'react';
import type { FormEvent } from 'react';
import { errorMessage } from '@/api/http';
import { useControlPlane } from '@/data/ControlPlaneProvider';
import { formatTier } from '@/shared/lib/format';
import { Button } from '@/shared/ui/Button';
import { Dialog, DialogActions } from '@/shared/ui/Dialog';
import { Checkbox, FormError, Input, Select } from '@/shared/ui/Field';

interface CreateProjectDialogProps {
  onCreated: (projectId: string) => void;
  onClose: () => void;
}

export function CreateProjectDialog({ onCreated, onClose }: CreateProjectDialogProps) {
  const { profiles, createProject } = useControlPlane();
  const [name, setName] = useState('');
  const [notebookUrl, setNotebookUrl] = useState('');
  const [ownerProfileId, setOwnerProfileId] = useState(profiles[0]?.id ?? 'default');
  const [autoLock, setAutoLock] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setSubmitting(true);
    setError(null);
    try {
      const created = await createProject({
        name: name.trim() || undefined,
        notebook_url: notebookUrl.trim(),
        owner_profile_id: ownerProfileId,
        lock: autoLock,
      });
      onClose();
      onCreated(created.id);
    } catch (failure) {
      setError(errorMessage(failure));
      setSubmitting(false);
    }
  }

  return (
    <Dialog title="New project" onClose={onClose}>
      <form onSubmit={handleSubmit} className="flex flex-col gap-4">
        <Input
          label="Project name"
          hint="Optional"
          value={name}
          disabled={submitting}
          onChange={(event) => setName(event.target.value)}
          placeholder="LLM Fine-Tuning"
        />
        <Input
          label="Colab notebook URL or Drive file ID"
          required
          value={notebookUrl}
          disabled={submitting}
          onChange={(event) => {
            setNotebookUrl(event.target.value);
            setError(null);
          }}
          placeholder="https://colab.research.google.com/drive/1YsugLz..."
          className="font-mono"
        />
        {profiles.length > 0 && (
          <Select
            label="Owner account profile"
            value={ownerProfileId}
            disabled={submitting}
            onChange={(event) => setOwnerProfileId(event.target.value)}
          >
            {profiles.map((profile) => (
              <option key={profile.id} value={profile.id}>
                {profile.name} ({profile.account_email || formatTier(profile.tier)})
              </option>
            ))}
          </Select>
        )}
        <Checkbox
          label="Acquire exclusive Project Lock immediately"
          checked={autoLock}
          disabled={submitting}
          onChange={(event) => setAutoLock(event.target.checked)}
        />
        {error && <FormError>{error}</FormError>}
        <DialogActions>
          <Button variant="ghost" onClick={onClose} disabled={submitting}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" disabled={!notebookUrl.trim() || submitting}>
            {submitting ? 'Creating…' : 'Create project'}
          </Button>
        </DialogActions>
      </form>
    </Dialog>
  );
}
