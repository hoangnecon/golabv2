const enc = encodeURIComponent;

/** Canonical v2 hash routes (UUID-keyed projects and profiles, URL-driven log filters). */
export const paths = {
  projects: '#/projects',
  project: (id: string) => `#/projects/${enc(id)}`,
  accounts: '#/accounts',
  account: (id: string) => `#/accounts/${enc(id)}`,
  logs: (query: Record<string, string> = {}) => {
    const params = new URLSearchParams();
    for (const [k, v] of Object.entries(query)) {
      if (v) params.set(k, v);
    }
    const qs = params.toString();
    return qs ? `#/logs?${qs}` : '#/logs';
  },
} as const;

export function navigate(hash: string) {
  window.location.hash = hash;
}
