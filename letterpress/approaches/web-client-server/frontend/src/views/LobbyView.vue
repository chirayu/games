<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from "vue";
import { useRouter } from "vue-router";

import { getActiveGames, type ActiveGameSummary } from "../api/games";
import {
  acceptChallenge,
  cancelChallenge,
  createRandomHumanAllocation,
  createSpecifiedChallenge,
  declineChallenge,
  getAllocation,
  getPendingChallenges,
  type ChallengeSummary,
  type GameAllocation,
} from "../api/allocations";
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
const challenges = ref<ChallengeSummary[]>([]);
const challengeError = ref("");
const challengeBusy = ref("");
const randomAllocation = ref<GameAllocation>();
const randomBusy = ref(false);
const randomError = ref("");
let randomPoll: number | undefined;

const incomingChallenges = computed(() =>
  challenges.value.filter((challenge) => challenge.direction === "incoming"),
);
const outgoingChallenges = computed(() =>
  challenges.value.filter((challenge) => challenge.direction === "outgoing"),
);

const yourTurn = computed(() =>
  games.value.filter((game) => game.activePlayer === game.playerSeat),
);
const waiting = computed(() =>
  games.value.filter((game) => game.activePlayer !== game.playerSeat),
);

onMounted(() => {
  void refreshLobby();
  window.addEventListener("focus", refreshLobby);
  document.addEventListener("visibilitychange", refreshWhenVisible);
});
onUnmounted(() => {
  window.removeEventListener("focus", refreshLobby);
  document.removeEventListener("visibilitychange", refreshWhenVisible);
  window.clearInterval(randomPoll);
});

async function joinRandomQueue() {
  randomBusy.value = true;
  randomError.value = "";
  try {
    randomAllocation.value = await createRandomHumanAllocation();
    await handleRandomAllocation(randomAllocation.value);
    if (
      randomAllocation.value?.state === "queued" &&
      randomPoll === undefined
    ) {
      randomPoll = window.setInterval(pollRandomAllocation, 5000);
    }
  } catch {
    randomError.value = "Random matchmaking could not start. Try again.";
  } finally {
    randomBusy.value = false;
  }
}

async function pollRandomAllocation() {
  if (!randomAllocation.value || document.visibilityState !== "visible") return;
  try {
    randomAllocation.value = await getAllocation(randomAllocation.value.id);
    await handleRandomAllocation(randomAllocation.value);
  } catch {
    randomError.value =
      "Queue status could not be refreshed. We’ll keep trying.";
  }
}

async function handleRandomAllocation(allocation: GameAllocation) {
  if (allocation.state === "allocated" && allocation.gameId) {
    window.clearInterval(randomPoll);
    randomPoll = undefined;
    await router.push(`/games/${allocation.gameId}`);
  }
}

async function leaveRandomQueue() {
  if (!randomAllocation.value) return;
  randomBusy.value = true;
  randomError.value = "";
  try {
    await cancelChallenge(randomAllocation.value.id);
    randomAllocation.value = undefined;
    window.clearInterval(randomPoll);
    randomPoll = undefined;
  } catch {
    randomError.value = "The queue changed before cancellation completed.";
    await pollRandomAllocation();
  } finally {
    randomBusy.value = false;
  }
}

function refreshWhenVisible() {
  if (document.visibilityState === "visible") void refreshLobby();
}

async function refreshLobby() {
  await Promise.all([
    loadGames(false),
    loadChallenges(),
    randomAllocation.value ? pollRandomAllocation() : Promise.resolve(),
  ]);
}

async function loadChallenges() {
  try {
    challenges.value = await getPendingChallenges();
  } catch (error) {
    if (error instanceof APIError && error.status === 401) {
      gamesState.value = "stale";
    }
  }
}

async function createChallenge(opponentId: string) {
  challengeBusy.value = `create:${opponentId}`;
  challengeError.value = "";
  try {
    await createSpecifiedChallenge(opponentId);
    await loadChallenges();
  } catch {
    challengeError.value = "The challenge could not be sent. Try again.";
  } finally {
    challengeBusy.value = "";
  }
}

async function actOnChallenge(
  challenge: ChallengeSummary,
  action: "accept" | "decline" | "cancel",
) {
  challengeBusy.value = challenge.allocationId;
  challengeError.value = "";
  try {
    const allocation =
      action === "accept"
        ? await acceptChallenge(challenge.allocationId)
        : action === "decline"
          ? await declineChallenge(challenge.allocationId)
          : await cancelChallenge(challenge.allocationId);
    if (allocation.state === "allocated" && allocation.gameId) {
      await router.push(`/games/${allocation.gameId}`);
      return;
    }
    await loadChallenges();
  } catch {
    challengeError.value =
      "That challenge changed before the action completed. Refresh and try again.";
    await loadChallenges();
  } finally {
    challengeBusy.value = "";
  }
}

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
      <section class="random-matchmaking" aria-labelledby="random-title">
        <h2 id="random-title" class="section-title">Play a random player</h2>
        <p v-if="randomAllocation?.state === 'queued'" role="status">
          Looking for another human player…
        </p>
        <button
          v-if="randomAllocation?.state === 'queued'"
          class="secondary-action"
          type="button"
          :disabled="randomBusy"
          @click="leaveRandomQueue"
        >
          {{ randomBusy ? "Cancelling…" : "Cancel search" }}
        </button>
        <button
          v-else
          class="primary-action"
          type="button"
          :disabled="randomBusy"
          @click="joinRandomQueue"
        >
          {{ randomBusy ? "Finding a player…" : "Find a human opponent" }}
        </button>
        <p v-if="randomError" class="notice notice--error" role="alert">
          {{ randomError }}
        </p>
      </section>

      <section
        v-if="games.length === 0"
        class="empty-lobby"
        aria-labelledby="empty-title"
      >
        <h2 id="empty-title">No active games yet.</h2>
        <p>Find another player below and send them a challenge.</p>
      </section>

      <p v-if="challengeError" class="notice notice--error" role="alert">
        {{ challengeError }}
      </p>

      <section
        v-if="incomingChallenges.length"
        aria-labelledby="incoming-title"
      >
        <h2 id="incoming-title" class="section-title">Incoming challenges</h2>
        <ul class="challenge-list">
          <li
            v-for="challenge in incomingChallenges"
            :key="challenge.allocationId"
          >
            <strong>{{ challenge.requester.name }}</strong>
            <div class="challenge-actions">
              <button
                class="primary-action"
                type="button"
                :disabled="challengeBusy === challenge.allocationId"
                @click="actOnChallenge(challenge, 'accept')"
              >
                Accept
              </button>
              <button
                class="secondary-action"
                type="button"
                :disabled="challengeBusy === challenge.allocationId"
                @click="actOnChallenge(challenge, 'decline')"
              >
                Decline
              </button>
            </div>
          </li>
        </ul>
      </section>

      <section
        v-if="outgoingChallenges.length"
        aria-labelledby="outgoing-title"
      >
        <h2 id="outgoing-title" class="section-title">Sent challenges</h2>
        <ul class="challenge-list">
          <li
            v-for="challenge in outgoingChallenges"
            :key="challenge.allocationId"
          >
            <strong>{{ challenge.opponent.name }}</strong>
            <button
              class="secondary-action"
              type="button"
              :disabled="challengeBusy === challenge.allocationId"
              @click="actOnChallenge(challenge, 'cancel')"
            >
              Cancel
            </button>
          </li>
        </ul>
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
            <span
              ><strong>{{ result.name }}</strong
              ><small>{{
                result.type === "bot" ? "Bot" : "Player"
              }}</small></span
            >
            <button
              class="secondary-action"
              type="button"
              :disabled="challengeBusy === `create:${result.id}`"
              @click="createChallenge(result.id)"
            >
              {{
                challengeBusy === `create:${result.id}`
                  ? "Sending…"
                  : "Challenge"
              }}
            </button>
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
