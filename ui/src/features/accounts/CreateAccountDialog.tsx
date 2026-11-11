import { useState } from 'react';
import type { FormEvent } from 'react';
import { errorMessage } from '@/api/http';
import type { Tier } from '@/api/types';
import { useControlPlane } from '@/data/ControlPlaneProvider';
import { formatTier } from '@/shared/lib/format';
import { Button } from '@/shared/ui/Button';
import { Dialog, DialogActions } from '@/shared/ui/Dialog';
import { FormError, Input, Select } from '@/shared/ui/Field';

const TIERS: readonly Tier[] = ['free', 'pro', 'pro_plus'];

interface CreateAccountDialogProps {
  onCreated?: (id: string) => void;
  onClose: () => void;
}

export function CreateAccountDialog({ onCreated, onClose }: CreateAccountDialogProps) {
  const { createProfile } = useControlPlane();
  const [name, setName] = useState('');
  const [email, setEmail] = useState('');
  const [tier, setTier] = useState<Tier>('free');
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setPending(true);
    setError(null);
    try {
      const created = await createProfile({
        name: name.trim(),
        account_email: email.trim() || undefined,
        tier,
      });
      onClose();
      onCreated?.(created.id);
    } catch (failure) {
      setError(errorMessage(failure));
      setPending(false);
    }
  }

  return (
    <Dialog title="New account profile" onClose={onClose}>
      <form onSubmit={handleSubmit} className="flex flex-col gap-4">
        <Input
          label="Account label"
          required
          value={name}
          disabled={pending}
          onChange={(event) => setName(event.target.value)}
          placeholder="Research Account #2"
        />
        <Input
          label="Google account email"
          hint="Optional (auto-synced from Colab)"
          type="email"
          value={email}
          disabled={pending}
          onChange={(event) => setEmail(event.target.value)}
          placeholder="researcher@gmail.com"
        />
        <Select
          label="Colab subscription tier"
          value={tier}
          disabled={pending}
          onChange={(event) => setTier(event.target.value as Tier)}
        >
          {TIERS.map((value) => (
            <option key={value} value={value}>
              {formatTier(value)}
            </option>
          ))}
        </Select>
        {error && <FormError>{error}</FormError>}
        <DialogActions>
          <Button variant="ghost" onClick={onClose} disabled={pending}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" disabled={pending || !name.trim()}>
            {pending ? 'Creating…' : 'Create account'}
          </Button>
        </DialogActions>
      </form>
    </Dialog>
  );
}
