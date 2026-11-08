interface APIErrorBody {
  code: string;
  message: string;
}

interface ErrorEnvelope {
  error: APIErrorBody;
}

interface DataEnvelope<T> {
  data: T;
}

export class APIError extends Error {
  readonly code: string;
  readonly status: number;

  constructor(status: number, body?: APIErrorBody) {
    super(body?.message ?? `request failed with status ${status}`);
    this.name = "APIError";
    this.code = body?.code ?? "request_failed";
    this.status = status;
  }
}

export async function request<T>(path: string, init: RequestInit): Promise<T> {
  const response = await fetch(path, {
    ...init,
    credentials: "same-origin",
    headers: {
      Accept: "application/json",
      ...init.headers,
    },
  });
  if (response.status === 204) {
    return undefined as T;
  }
  const body = (await response.json().catch(() => undefined)) as
    DataEnvelope<T> | ErrorEnvelope | undefined;
  if (!response.ok) {
    const error = body && "error" in body ? body.error : undefined;
    throw new APIError(response.status, error);
  }
  if (!body || !("data" in body)) {
    throw new APIError(response.status, {
      code: "invalid_response",
      message: "The game server returned an invalid response.",
    });
  }
  return body.data;
}
