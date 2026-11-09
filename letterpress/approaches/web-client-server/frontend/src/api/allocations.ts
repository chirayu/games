import type { PublicPlayer } from "./players";
import { request } from "./request";

export interface GameAllocation {
  id: string;
  requesterId: string;
  opponentId?: string;
  mode: "specified" | "random" | "bot";
  allowedOpponentTypes?: Array<"human" | "bot">;
  state:
    | "pending_acceptance"
    | "queued"
    | "allocated"
    | "declined"
    | "cancelled"
    | "expired"
    | "failed";
  gameId?: string;
  expiresAt?: string;
}

export interface ChallengeSummary {
  allocationId: string;
  direction: "incoming" | "outgoing";
  requester: PublicPlayer;
  opponent: PublicPlayer;
  expiresAt: string;
  createdAt: string;
}

export interface LobbyAllocations {
  items: ChallengeSummary[];
  randomAllocation?: GameAllocation;
  botAllocation?: GameAllocation;
}

export interface BotProfilePreference {
  style: string;
  englishKnowledgeLevel: string;
  skillLevel: string;
}

export interface BotProfilePage {
  items: PublicPlayer[];
}

export function createSpecifiedChallenge(
  opponentId: string,
): Promise<GameAllocation> {
  return request<GameAllocation>("/api/v1/game-allocations", {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      "Idempotency-Key": crypto.randomUUID(),
    },
    body: JSON.stringify({ mode: "specified", opponentId }),
  });
}

export function createRandomHumanAllocation(): Promise<GameAllocation> {
  return request<GameAllocation>("/api/v1/game-allocations", {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      "Idempotency-Key": crypto.randomUUID(),
    },
    body: JSON.stringify({
      mode: "random",
      allowedOpponentTypes: ["human"],
    }),
  });
}

export function getBotProfiles(): Promise<BotProfilePage> {
  return request<BotProfilePage>("/api/v1/bot-profiles", { method: "GET" });
}

export function createBotAllocation(
  botProfile: BotProfilePreference,
  idempotencyKey: string,
): Promise<GameAllocation> {
  return request<GameAllocation>("/api/v1/game-allocations", {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      "Idempotency-Key": idempotencyKey,
    },
    body: JSON.stringify({ mode: "bot", botProfile }),
  });
}

export function getAllocation(id: string): Promise<GameAllocation> {
  return request<GameAllocation>(
    `/api/v1/game-allocations/${encodeURIComponent(id)}`,
    { method: "GET" },
  );
}

export function getLobbyAllocations(): Promise<LobbyAllocations> {
  return request<LobbyAllocations>("/api/v1/game-allocations", {
    method: "GET",
  });
}

export function acceptChallenge(id: string): Promise<GameAllocation> {
  return challengeAction(id, "accept");
}

export function declineChallenge(id: string): Promise<GameAllocation> {
  return challengeAction(id, "decline");
}

export function cancelChallenge(id: string): Promise<GameAllocation> {
  return request<GameAllocation>(
    `/api/v1/game-allocations/${encodeURIComponent(id)}`,
    { method: "DELETE" },
  );
}

function challengeAction(
  id: string,
  action: "accept" | "decline",
): Promise<GameAllocation> {
  return request<GameAllocation>(
    `/api/v1/game-allocations/${encodeURIComponent(id)}/${action}`,
    { method: "POST" },
  );
}
