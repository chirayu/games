<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { useRoute } from "vue-router";

import {
  getGame,
  submitMove,
  type GameState,
  type GameTile,
  type TileOwner,
} from "../api/games";

type ViewState = "loading" | "ready" | "unauthorized" | "not-found" | "error";

const route = useRoute();
const gameID = String(route.params.gameId ?? "");
const state = ref<ViewState>("loading");
const game = ref<GameState>();
const selectedIndexes = ref<number[]>([]);
const submitting = ref(false);
const refreshing = ref(false);
const moveError = ref("");
const syncMessage = ref("");
const draftKey = `letterpress:draft:${gameID}`;
let pollTimer: number | undefined;

const canPlay = computed(
  () =>
    game.value?.status === "active" &&
    game.value.activePlayer === game.value.playerSeat,
);

const formedWord = computed(() =>
  selectedIndexes.value
    .map((index) => game.value?.tiles[index]?.letter ?? "")
    .join(""),
);

const turnMessage = computed(() => {
  if (!game.value) return "";
  if (game.value.status === "completed") return "Game complete";
  if (game.value.activePlayer === game.value.playerSeat) return "Your turn";
  return `Player ${game.value.activePlayer}’s turn`;
});

const playHint = computed(() =>
  game.value?.status === "completed"
    ? "This game is complete."
    : "Wait for the other player’s move.",
);

const resultHeading = computed(() => {
  if (!game.value?.result) return "";
  if (game.value.result.tie) return "It’s a tie";
  if (game.value.result.winner === game.value.playerSeat) return "You won";
  return `Player ${game.value.result.winner} won`;
});

onMounted(async () => {
  await load();
  window.addEventListener("focus", handleFocus);
  document.addEventListener("visibilitychange", handleVisibility);
  syncPolling();
});

onBeforeUnmount(() => {
  window.removeEventListener("focus", handleFocus);
  document.removeEventListener("visibilitychange", handleVisibility);
  stopPolling();
});

async function load() {
  state.value = "loading";
  selectedIndexes.value = [];
  moveError.value = "";
  syncMessage.value = "";
  await refreshGame(true);
}

async function refreshGame(initial = false, stale = false) {
  if (refreshing.value) return;
  refreshing.value = true;
  try {
    const previousVersion = game.value?.version;
    const latest = await getGame(gameID);
    game.value = latest;
    state.value = "ready";
    if (initial) restoreDraft(latest);
    else if (
      previousVersion !== undefined &&
      latest.version !== previousVersion &&
      selectedIndexes.value.length > 0
    ) {
      discardDraft();
      syncMessage.value = stale
        ? "The board changed before your move. Your saved selection was cleared."
        : "The board was updated, so your saved selection was cleared.";
    } else if (latest.version !== previousVersion) {
      syncMessage.value = "Latest move received.";
    } else if (!stale) {
      syncMessage.value = "";
    }
  } catch (error) {
    const status = (error as { status?: number }).status;
    if (initial) {
      if (status === 401) state.value = "unauthorized";
      else if (status === 404) state.value = "not-found";
      else state.value = "error";
    } else {
      syncMessage.value =
        "You appear to be offline. Your selection is saved on this device.";
    }
  } finally {
    refreshing.value = false;
    syncPolling();
  }
}

function restoreDraft(latest: GameState) {
  try {
    const raw = localStorage.getItem(draftKey);
    if (!raw) return;
    const draft = JSON.parse(raw) as {
      version?: number;
      tileIndexes?: number[];
    };
    if (draft.version !== latest.version) {
      discardDraft();
      syncMessage.value =
        "The board changed while you were away, so an older saved selection was cleared.";
      return;
    }
    if (latest.status !== "active" || latest.activePlayer !== latest.playerSeat)
      return;
    selectedIndexes.value = (draft.tileIndexes ?? []).filter(
      (index, position, indexes) =>
        Number.isInteger(index) &&
        index >= 0 &&
        index < latest.tiles.length &&
        indexes.indexOf(index) === position,
    );
  } catch {
    discardDraft();
  }
}

function saveDraft() {
  if (!game.value || selectedIndexes.value.length === 0) {
    discardDraft();
    return;
  }
  localStorage.setItem(
    draftKey,
    JSON.stringify({
      version: game.value.version,
      tileIndexes: selectedIndexes.value,
    }),
  );
}

function discardDraft() {
  selectedIndexes.value = [];
  localStorage.removeItem(draftKey);
}

function handleFocus() {
  if (document.visibilityState === "visible") void refreshGame();
}

function handleVisibility() {
  if (document.visibilityState === "visible") void refreshGame();
  syncPolling();
}

function syncPolling() {
  const shouldPoll =
    document.visibilityState === "visible" &&
    game.value?.status === "active" &&
    game.value.activePlayer !== game.value.playerSeat;
  if (!shouldPoll) {
    stopPolling();
    return;
  }
  if (pollTimer === undefined) {
    pollTimer = window.setInterval(() => void refreshGame(), 30_000);
  }
}

function stopPolling() {
  if (pollTimer !== undefined) window.clearInterval(pollTimer);
  pollTimer = undefined;
}

function ownerName(owner: TileOwner) {
  if (owner === 1) return "owned by Player 1";
  if (owner === 2) return "owned by Player 2";
  return "neutral";
}

function tileLabel(tile: GameTile) {
  if (!game.value) return "";
  const row = Math.floor(tile.index / game.value.width) + 1;
  const column = (tile.index % game.value.width) + 1;
  const selectionPosition = selectedIndexes.value.indexOf(tile.index);
  const selection =
    selectionPosition >= 0 ? `, selected ${selectionPosition + 1}` : "";
  return `${tile.letter}, ${ownerName(tile.owner)}${tile.protected ? ", protected" : ""}${selection}, row ${row}, column ${column}`;
}

function toggleTile(index: number) {
  if (!canPlay.value || submitting.value) return;
  moveError.value = "";
  const position = selectedIndexes.value.indexOf(index);
  if (position >= 0) selectedIndexes.value.splice(position, 1);
  else selectedIndexes.value.push(index);
  saveDraft();
}

function clearSelection() {
  if (submitting.value) return;
  discardDraft();
  moveError.value = "";
}

async function submitSelection() {
  if (
    !game.value ||
    !canPlay.value ||
    selectedIndexes.value.length === 0 ||
    submitting.value
  )
    return;
  submitting.value = true;
  moveError.value = "";
  try {
    game.value = await submitMove(
      gameID,
      [...selectedIndexes.value],
      game.value.version,
    );
    discardDraft();
    syncMessage.value = "Move submitted.";
    syncPolling();
  } catch (error) {
    const code = (error as { code?: string }).code;
    const messages: Record<string, string> = {
      duplicate_tile: "A tile can appear only once in a word.",
      invalid_tile: "One of those tiles is no longer available.",
      invalid_word:
        "That word isn’t in the English dictionary. Edit your selection and try again.",
      repeated_word: "That word has already been played.",
      prefix_word: "That word is a prefix of a word already played.",
      out_of_turn: "It is no longer your turn.",
      game_completed: "This game is already complete.",
      version_conflict:
        "The board changed before your move was submitted. It has been refreshed.",
    };
    moveError.value =
      messages[code ?? ""] ?? "Your move couldn’t be submitted. Try again.";
    if (code === "version_conflict") {
      await refreshGame(false, true);
    } else {
      saveDraft();
    }
  } finally {
    submitting.value = false;
  }
}
</script>

<template>
  <main class="game-page">
    <section v-if="state === 'loading'" class="state-card" role="status">
      <span class="loading-mark" aria-hidden="true"></span>
      <h1>Loading the board…</h1>
      <p>Fetching the latest game state.</p>
    </section>

    <section
      v-else-if="state === 'unauthorized'"
      class="state-card"
      aria-labelledby="unauthorized-title"
    >
      <p class="eyebrow">Game access</p>
      <h1 id="unauthorized-title">This game isn’t in your lobby.</h1>
      <p>Sign in as a player assigned to this game, then try again.</p>
      <RouterLink class="text-link" to="/lobby"
        >Return to your games</RouterLink
      >
    </section>

    <section
      v-else-if="state === 'not-found'"
      class="state-card"
      aria-labelledby="missing-title"
    >
      <p class="eyebrow">Game not found</p>
      <h1 id="missing-title">That board doesn’t exist.</h1>
      <p>Check the address or return to your active games.</p>
      <RouterLink class="text-link" to="/lobby"
        >Return to your games</RouterLink
      >
    </section>

    <section
      v-else-if="state === 'error'"
      class="state-card"
      aria-labelledby="error-title"
    >
      <p class="eyebrow">Connection problem</p>
      <h1 id="error-title">The board couldn’t be loaded.</h1>
      <p>Your game is safe. Try fetching the latest state again.</p>
      <button class="primary-action" type="button" @click="load">
        Try again
      </button>
    </section>

    <section v-else-if="game" class="game-layout" aria-labelledby="game-title">
      <header class="game-header">
        <div>
          <p class="eyebrow game-id">Game {{ game.id }}</p>
          <h1 id="game-title">The board</h1>
        </div>
        <div class="turn-banner" role="status">
          <span class="turn-banner__dot" aria-hidden="true"></span>
          {{ turnMessage }}
        </div>
      </header>

      <div class="scoreboard" aria-label="Current scores">
        <article class="score-card score-card--one">
          <div>
            <span>Player 1</span>
            <small v-if="game.playerSeat === 1">You</small>
          </div>
          <strong>{{ game.scores.playerOne }}</strong>
        </article>
        <article class="score-card score-card--two">
          <div>
            <span>Player 2</span>
            <small v-if="game.playerSeat === 2">You</small>
          </div>
          <strong>{{ game.scores.playerTwo }}</strong>
        </article>
      </div>

      <section
        v-if="game.status === 'completed' && game.result"
        class="result-card"
        aria-labelledby="result-title"
      >
        <p class="eyebrow">Final result</p>
        <h2 id="result-title">{{ resultHeading }}</h2>
        <p>
          Player 1 {{ game.result.playerOneScore }} · Player 2
          {{ game.result.playerTwoScore }}
        </p>
      </section>

      <div v-if="syncMessage || refreshing" class="sync-status" role="status">
        <span v-if="refreshing" class="loading-mark" aria-hidden="true"></span>
        {{ refreshing ? "Checking for the latest move…" : syncMessage }}
        <button
          v-if="!refreshing && syncMessage.includes('offline')"
          class="text-button"
          type="button"
          @click="refreshGame()"
        >
          Retry now
        </button>
      </div>

      <div
        class="letter-board"
        role="grid"
        aria-label="Letterpress board"
        :aria-rowcount="game.height"
        :aria-colcount="game.width"
        :style="{
          gridTemplateColumns: `repeat(${game.width}, minmax(0, 1fr))`,
        }"
      >
        <button
          v-for="tile in game.tiles"
          :key="tile.index"
          class="game-tile"
          role="gridcell"
          type="button"
          :class="{
            'game-tile--selected': selectedIndexes.includes(tile.index),
          }"
          :data-owner="tile.owner"
          :data-protected="tile.protected"
          :aria-pressed="selectedIndexes.includes(tile.index)"
          :aria-disabled="!canPlay || submitting"
          :aria-rowindex="Math.floor(tile.index / game.width) + 1"
          :aria-colindex="(tile.index % game.width) + 1"
          :aria-label="tileLabel(tile)"
          @click="toggleTile(tile.index)"
        >
          <span class="game-tile__letter">{{ tile.letter }}</span>
          <span
            v-if="selectedIndexes.includes(tile.index)"
            class="selection-order"
            aria-hidden="true"
          >
            {{ selectedIndexes.indexOf(tile.index) + 1 }}
          </span>
          <span v-if="tile.protected" class="protection-mark" aria-hidden="true"
            >◆</span
          >
        </button>
      </div>

      <section class="move-composer" aria-labelledby="word-title">
        <div class="move-composer__word">
          <span id="word-title">Your word</span>
          <strong aria-live="polite">
            {{ formedWord || "Select tiles" }}
          </strong>
        </div>
        <div class="move-actions">
          <button
            class="secondary-action move-clear"
            type="button"
            :disabled="selectedIndexes.length === 0 || submitting"
            @click="clearSelection"
          >
            Clear
          </button>
          <button
            class="primary-action move-submit"
            type="button"
            :disabled="!canPlay || selectedIndexes.length === 0 || submitting"
            @click="submitSelection"
          >
            {{ submitting ? "Submitting…" : "Submit word" }}
          </button>
        </div>
        <p v-if="!canPlay" class="move-hint">
          {{ playHint }}
        </p>
        <p v-if="moveError" class="move-error" role="alert">{{ moveError }}</p>
      </section>

      <details v-if="game.moves.length" class="move-history">
        <summary>Move history ({{ game.moves.length }})</summary>
        <ol>
          <li v-for="move in game.moves" :key="move.turnNumber">
            <span>Player {{ move.player }}</span>
            <strong>{{ move.word }}</strong>
          </li>
        </ol>
      </details>

      <footer class="board-footer">
        <div class="legend" aria-label="Board legend">
          <span><i data-owner="0"></i>Neutral</span>
          <span><i data-owner="1"></i>Player 1</span>
          <span><i data-owner="2"></i>Player 2</span>
          <span><b aria-hidden="true">◆</b>Protected</span>
        </div>
        <p>State v{{ game.version }} · {{ game.status }}</p>
      </footer>
    </section>
  </main>
</template>
