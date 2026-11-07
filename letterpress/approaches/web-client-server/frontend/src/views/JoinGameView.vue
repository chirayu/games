<script setup lang="ts">
import { onMounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";

import { exchangeSession } from "../api/games";

type JoinState = "joining" | "error";

const route = useRoute();
const router = useRouter();
const state = ref<JoinState>("joining");
const message = ref("Joining your game…");

onMounted(async () => {
  const gameID = String(route.params.gameId ?? "");
  const token = new URLSearchParams(route.hash.slice(1)).get("token");
  if (!gameID || !token) {
    state.value = "error";
    message.value = "This invitation link is missing its private access token.";
    return;
  }

  try {
    await exchangeSession(gameID, token);
    await router.replace({ name: "game", params: { gameId: gameID } });
  } catch {
    state.value = "error";
    message.value =
      "This invitation link is invalid. Ask Player 1 for a new copy.";
  }
});
</script>

<template>
  <main class="page-shell">
    <section class="flow-card flow-card--compact" aria-labelledby="join-title">
      <p class="eyebrow">Private game</p>
      <h1 id="join-title">
        {{ state === "joining" ? "Taking your seat…" : "Unable to join" }}
      </h1>
      <p
        :class="['notice', state === 'error' && 'notice--error']"
        role="status"
      >
        {{ message }}
      </p>
      <RouterLink v-if="state === 'error'" class="text-link" to="/">
        Return home
      </RouterLink>
    </section>
  </main>
</template>
