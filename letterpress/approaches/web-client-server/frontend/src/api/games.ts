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

export type TileOwner = 0 | 1 | 2;

export interface GameTile {
  index: number;
  letter: string;
  owner: TileOwner;
  protected: boolean;
}

export interface GameState {
  id: string;
  width: number;
  height: number;
  tiles: GameTile[];
  scores: {
    playerOne: number;
    playerTwo: number;
  };
  activePlayer: 1 | 2;
  playerSeat: 1 | 2;
  version: number;
  status: "active" | "completed";
  moves: GameMove[];
  result?: GameResult;
}

export interface GameMove {
  player: 1 | 2;
  turnNumber: number;
  tileIndexes: number[];
  word: string;
}

export interface GameResult {
  playerOneScore: number;
  playerTwoScore: number;
  winner: 0 | 1 | 2;
  tie: boolean;
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

export async function getGame(gameId: string): Promise<GameState> {
  return request<GameState>(`/api/v1/games/${encodeURIComponent(gameId)}`, {
    method: "GET",
  });
}

export async function submitMove(
  gameId: string,
  tileIndexes: number[],
  expectedVersion: number,
): Promise<GameState> {
  return request<GameState>(
    `/api/v1/games/${encodeURIComponent(gameId)}/moves`,
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ tileIndexes, expectedVersion }),
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
