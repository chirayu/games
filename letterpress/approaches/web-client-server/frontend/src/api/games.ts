import { request } from "./request";

export { APIError } from "./request";

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
