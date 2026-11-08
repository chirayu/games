<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useRouter } from "vue-router";

import { getActiveGames, type ActiveGameSummary } from "../api/games";
import { searchPlayers, type PublicPlayer } from "../api/players";
import { APIError } from "../api/request";
import {
  clearPlayerSession,
  endPlayerSession,
  usePlayerSession,
} from "../composables/playerSession";

const router = useRouter();
const { player } = usePlayerSession();
const loggingOut = ref(false);
const logoutError = ref("");
const games = ref<ActiveGameSummary[]>([]);
const gamesCursor = ref("");
const gamesState = ref<"loading" | "ready" | "error" | "stale">("loading");
const searchQuery = ref("");
const searchResults = ref<PublicPlayer[]>([]);
const searchCursor = ref("");
const searchState = ref<"idle" | "loading" | "ready" | "error">("idle");

const yourTurn = computed(() =>
  games.value.filter((game) => game.activePlayer === game.playerSeat),
);
const waiting = computed(() =>
  games.value.filter((game) => game.activePlayer !== game.playerSeat),
);

onMounted(() => loadGames(false));

async function loadGames(append: boolean) {
  if (!append) gamesState.value = "loading";
  try {
    const page = await getActiveGames(append ? gamesCursor.value : "");
    games.value = append ? [...games.value, ...page.items] : page.items;
    gamesCursor.value = page.nextCursor ?? "";
    gamesState.value = "ready";
  } catch (error) {
    gamesState.value =
      error instanceof APIError && error.status === 401 ? "stale" : "error";
  }
}

async function runSearch(append = false) {
  const query = searchQuery.value.trim();
  if (!query) return;
  searchState.value = "loading";
  try {
    const page = await searchPlayers(query, append ? searchCursor.value : "");
    searchResults.value = append
      ? [...searchResults.value, ...page.items]
      : page.items;
    searchCursor.value = page.nextCursor ?? "";
    searchState.value = "ready";
  } catch (error) {
    if (error instanceof APIError && error.status === 401) {
      gamesState.value = "stale";
      return;
    }
    searchState.value = "error";
  }
}

async function returnToSignIn() {
  clearPlayerSession();
  await router.replace({ name: "home" });
}

async function logout() {
  loggingOut.value = true;
  logoutError.value = "";
  try {
    await endPlayerSession();
    await router.replace({ name: "home" });
  } catch {
    logoutError.value =
      "We couldn't log you out. Check your connection and try again.";
  } finally {
    loggingOut.value = false;
  }
}
</script>

<template>
  <main class="lobby-page">
    <header class="lobby-header">
      <div>
        <p class="eyebrow">Letterpress</p>
        <h1>Your games</h1>
        <p v-if="player" class="notice">Signed in as {{ player.name }}</p>
      </div>
      <button
        class="secondary-action"
        type="button"
        :disabled="loggingOut"
        @click="logout"
      >
        {{ loggingOut ? "Logging out…" : "Log out" }}
      </button>
    </header>
    <p v-if="logoutError" class="notice notice--error" role="alert">
      {{ logoutError }}
    </p>

    <section v-if="gamesState === 'loading'" class="lobby-state" role="status">
      Loading your active games…
    </section>
    <section
      v-else-if="gamesState === 'stale'"
      class="lobby-state"
      aria-labelledby="stale-title"
    >
      <h2 id="stale-title">Your session has expired.</h2>
      <p>Enter your player name again to return to your games.</p>
      <button class="primary-action" type="button" @click="returnToSignIn">
        Return to sign in
      </button>
    </section>
    <section
      v-else-if="gamesState === 'error'"
      class="lobby-state"
      aria-labelledby="failure-title"
    >
      <h2 id="failure-title">Your games couldn’t be loaded.</h2>
      <p>Check your connection and try again.</p>
      <button class="primary-action" type="button" @click="loadGames(false)">
        Try again
      </button>
    </section>
    <div v-else class="lobby-content">
      <section
        v-if="games.length === 0"
        class="empty-lobby"
        aria-labelledby="empty-title"
      >
        <h2 id="empty-title">No active games yet.</h2>
        <p>
          Find another player below. Challenge creation arrives in the next
          wave.
        </p>
      </section>

      <section v-if="yourTurn.length" aria-labelledby="your-turn-title">
        <h2 id="your-turn-title" class="section-title">Your turn</h2>
        <div class="game-list">
          <RouterLink
            v-for="game in yourTurn"
            :key="game.id"
            class="game-summary"
            :to="`/games/${game.id}`"
          >
            <strong>{{ game.opponent.name }}</strong>
            <span>{{ game.scores.playerOne }}–{{ game.scores.playerTwo }}</span>
            <small
              >{{ game.opponent.type === "bot" ? "Bot" : "Player" }} · Your
              move</small
            >
          </RouterLink>
        </div>
      </section>

      <section v-if="waiting.length" aria-labelledby="waiting-title">
        <h2 id="waiting-title" class="section-title">Waiting for opponent</h2>
        <div class="game-list">
          <RouterLink
            v-for="game in waiting"
            :key="game.id"
            class="game-summary"
            :to="`/games/${game.id}`"
          >
            <strong>{{ game.opponent.name }}</strong>
            <span>{{ game.scores.playerOne }}–{{ game.scores.playerTwo }}</span>
            <small
              >{{ game.opponent.type === "bot" ? "Bot" : "Player" }} · Their
              move</small
            >
          </RouterLink>
        </div>
      </section>

      <button
        v-if="gamesCursor"
        class="secondary-action load-more"
        type="button"
        @click="loadGames(true)"
      >
        Load more games
      </button>

      <section class="player-search" aria-labelledby="find-player-title">
        <h2 id="find-player-title" class="section-title">Find a player</h2>
        <form class="search-form" @submit.prevent="runSearch(false)">
          <label class="visually-hidden" for="player-search">Player name</label>
          <input
            id="player-search"
            v-model="searchQuery"
            placeholder="Search by name"
          />
          <button
            class="primary-action"
            type="submit"
            :disabled="searchState === 'loading'"
          >
            {{ searchState === "loading" ? "Searching…" : "Search" }}
          </button>
        </form>
        <p
          v-if="searchState === 'error'"
          class="notice notice--error"
          role="alert"
        >
          Player search failed. Check the name or your connection and try again.
        </p>
        <p
          v-else-if="searchState === 'ready' && searchResults.length === 0"
          class="notice"
        >
          No matching players found.
        </p>
        <ul v-else-if="searchResults.length" class="player-results">
          <li
            v-for="result in searchResults"
            :key="result.id"
            :data-player-id="result.id"
          >
            <span>{{ result.name }}</span
            ><small>{{ result.type === "bot" ? "Bot" : "Player" }}</small>
          </li>
        </ul>
        <button
          v-if="searchCursor"
          class="secondary-action load-more"
          type="button"
          @click="runSearch(true)"
        >
          Load more players
        </button>
      </section>
    </div>
  </main>
</template>
