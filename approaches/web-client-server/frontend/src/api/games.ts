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

export interface CreatedGame {
  gameId: string;
  playerOneToken: string;
  playerTwoToken: string;
}

export interface PlayerSession {
  gameId: string;
  seat: number;
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

export async function createGame(): Promise<CreatedGame> {
  return request<CreatedGame>("/api/v1/games", {
    method: "POST",
  });
}

export async function exchangeSession(
  gameId: string,
  token: string,
): Promise<PlayerSession> {
  return request<PlayerSession>(
    `/api/v1/games/${encodeURIComponent(gameId)}/sessions`,
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ token }),
    },
  );
}

async function request<T>(path: string, init: RequestInit): Promise<T> {
  const response = await fetch(path, {
    ...init,
    credentials: "same-origin",
    headers: {
      Accept: "application/json",
      ...init.headers,
    },
  });
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
