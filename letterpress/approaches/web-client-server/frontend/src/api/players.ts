import { request } from "./request";

export interface Player {
  id: string;
  name: string;
}

export interface PublicPlayer extends Player {
  type: "human" | "bot";
}

export interface PlayerPage {
  items: PublicPlayer[];
  nextCursor?: string;
}

export function enterPlayer(name: string): Promise<Player> {
  return request<Player>("/api/v1/player-sessions", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ name }),
  });
}

export function getCurrentPlayer(): Promise<Player> {
  return request<Player>("/api/v1/players/me", { method: "GET" });
}

export function logoutPlayer(): Promise<void> {
  return request<void>("/api/v1/player-sessions/current", {
    method: "DELETE",
  });
}

export function searchPlayers(
  query: string,
  cursor = "",
  limit = 20,
): Promise<PlayerPage> {
  const parameters = new URLSearchParams({ q: query, limit: String(limit) });
  if (cursor) parameters.set("cursor", cursor);
  return request<PlayerPage>(`/api/v1/players?${parameters}`, {
    method: "GET",
  });
}
