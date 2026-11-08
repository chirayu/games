import { readonly, ref } from "vue";

import {
  enterPlayer,
  getCurrentPlayer,
  logoutPlayer,
  type Player,
} from "../api/players";
import { APIError } from "../api/request";

const player = ref<Player | null>(null);
const restoring = ref(false);
const restoreError = ref<string | null>(null);
let restored = false;

export async function restorePlayerSession(): Promise<Player | null> {
  if (restored) return player.value;
  restoring.value = true;
  restoreError.value = null;
  try {
    player.value = await getCurrentPlayer();
  } catch (error) {
    player.value = null;
    if (!(error instanceof APIError) || error.status !== 401) {
      restoreError.value =
        "We couldn't reach the game server. Check your connection and try again.";
    }
  } finally {
    restoring.value = false;
    restored = true;
  }
  return player.value;
}

export async function startPlayerSession(name: string): Promise<Player> {
  const current = await enterPlayer(name);
  player.value = current;
  restored = true;
  return current;
}

export async function endPlayerSession(): Promise<void> {
  await logoutPlayer();
  player.value = null;
  restored = true;
}

export function usePlayerSession() {
  return {
    player: readonly(player),
    restoring: readonly(restoring),
    restoreError: readonly(restoreError),
  };
}

export async function retryPlayerSessionRestore(): Promise<Player | null> {
  restored = false;
  return restorePlayerSession();
}

export function resetPlayerSessionForTests() {
  player.value = null;
  restoring.value = false;
  restoreError.value = null;
  restored = false;
}
