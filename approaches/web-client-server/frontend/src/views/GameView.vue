<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
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
const moveError = ref("");

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

onMounted(load);

async function load() {
  state.value = "loading";
  selectedIndexes.value = [];
  moveError.value = "";
  try {
    game.value = await getGame(gameID);
    state.value = "ready";
  } catch (error) {
    const status = (error as { status?: number }).status;
    if (status === 401) state.value = "unauthorized";
    else if (status === 404) state.value = "not-found";
    else state.value = "error";
  }
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
}

function clearSelection() {
  if (submitting.value) return;
  selectedIndexes.value = [];
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
    selectedIndexes.value = [];
  } catch (error) {
    const code = (error as { code?: string }).code;
    const messages: Record<string, string> = {
      duplicate_tile: "A tile can appear only once in a word.",
      invalid_tile: "One of those tiles is no longer available.",
      invalid_word: "Select at least one tile before submitting.",
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
      try {
        game.value = await getGame(gameID);
        selectedIndexes.value = [];
      } catch {
        // Keep the actionable move error; the next explicit retry can reload.
      }
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
      <p class="eyebrow">Private game</p>
      <h1 id="unauthorized-title">This seat isn’t available here.</h1>
      <p>Open your private invitation link on this device to restore access.</p>
      <RouterLink class="text-link" to="/">Return home</RouterLink>
    </section>

    <section
      v-else-if="state === 'not-found'"
      class="state-card"
      aria-labelledby="missing-title"
    >
      <p class="eyebrow">Game not found</p>
      <h1 id="missing-title">That board doesn’t exist.</h1>
      <p>Check the address or ask the other player to resend the invitation.</p>
      <RouterLink class="text-link" to="/">Return home</RouterLink>
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
