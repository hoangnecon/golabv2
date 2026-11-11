export interface AppErrorPayload {
  code?: string;
  message?: string;
  hint?: string;
}

export class ApiError extends Error {
  readonly code: string;
  readonly hint: string;

  constructor(
    readonly status: number,
    message: string,
    code = '',
    hint = '',
  ) {
    super(message);
    this.name = 'ApiError';
    this.code = code;
    this.hint = hint;
  }
}

export function errorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    return error.hint ? `${error.message} (${error.hint})` : error.message;
  }
  return error instanceof Error ? error.message : String(error);
}

export async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, {
    ...init,
    headers: { 'Content-Type': 'application/json', ...init?.headers },
  });

  if (!response.ok) {
    let message = `HTTP ${response.status}`;
    let code = '';
    let hint = '';
    try {
      const body = (await response.json()) as {
        message?: string;
        error?: string | AppErrorPayload;
      };
      if (typeof body.error === 'object' && body.error !== null) {
        code = body.error.code ?? '';
        hint = body.error.hint ?? '';
        message = body.error.message ?? message;
      } else if (typeof body.error === 'string') {
        message = body.error;
      } else if (body.message) {
        message = body.message;
      }
    } catch {
      // Non-JSON error body: keep status code message.
    }
    throw new ApiError(response.status, message, code, hint);
  }

  return (await response.json()) as T;
}
