<script setup lang="ts">
import { ref } from "vue";
import { useRouter } from "vue-router";

import {
  endPlayerSession,
  usePlayerSession,
} from "../composables/playerSession";

const router = useRouter();
const { player } = usePlayerSession();
const loggingOut = ref(false);
const logoutError = ref("");

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
    <section class="empty-lobby" aria-labelledby="empty-title">
      <h2 id="empty-title">Your lobby is ready.</h2>
      <p>Active games and player discovery arrive in the next step.</p>
    </section>
  </main>
</template>
