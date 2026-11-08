import { request } from "./request";

export interface Player {
  id: string;
  name: string;
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
