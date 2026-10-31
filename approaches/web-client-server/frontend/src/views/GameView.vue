<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useRoute } from "vue-router";

import {
  getGame,
  type GameState,
  type GameTile,
  type TileOwner,
} from "../api/games";

type ViewState = "loading" | "ready" | "unauthorized" | "not-found" | "error";

const route = useRoute();
const gameID = String(route.params.gameId ?? "");
const state = ref<ViewState>("loading");
const game = ref<GameState>();

const turnMessage = computed(() => {
  if (!game.value) return "";
  if (game.value.status === "completed") return "Game complete";
  if (game.value.activePlayer === game.value.playerSeat) return "Your turn";
  return `Player ${game.value.activePlayer}’s turn`;
});

onMounted(load);

async function load() {
  state.value = "loading";
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
  return `${tile.letter}, ${ownerName(tile.owner)}${tile.protected ? ", protected" : ""}, row ${row}, column ${column}`;
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
        <div
          v-for="tile in game.tiles"
          :key="tile.index"
          class="game-tile"
          role="gridcell"
          tabindex="0"
          :data-owner="tile.owner"
          :data-protected="tile.protected"
          :aria-rowindex="Math.floor(tile.index / game.width) + 1"
          :aria-colindex="(tile.index % game.width) + 1"
          :aria-label="tileLabel(tile)"
        >
          <span class="game-tile__letter">{{ tile.letter }}</span>
          <span v-if="tile.protected" class="protection-mark" aria-hidden="true"
            >◆</span
          >
        </div>
      </div>

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
