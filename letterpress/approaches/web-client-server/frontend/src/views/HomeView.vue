<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useRouter } from "vue-router";

import { APIError } from "../api/request";
import {
  restorePlayerSession,
  retryPlayerSessionRestore,
  startPlayerSession,
  usePlayerSession,
} from "../composables/playerSession";

const router = useRouter();
const { player, restoring, restoreError } = usePlayerSession();
const name = ref("");
const submitting = ref(false);
const errorMessage = ref("");
const normalizedName = computed(() => name.value.trim().replace(/\s+/g, " "));

onMounted(async () => {
  await restorePlayerSession();
  if (player.value) await router.replace({ name: "lobby" });
});

async function submit() {
  errorMessage.value = "";
  const candidate = normalizedName.value;
  if (
    candidate.length < 1 ||
    candidate.length > 24 ||
    !/^[A-Za-z0-9 _-]+$/.test(candidate)
  ) {
    errorMessage.value =
      "Use 1–24 letters, numbers, spaces, hyphens, or underscores.";
    return;
  }
  submitting.value = true;
  try {
    await startPlayerSession(candidate);
    await router.replace({ name: "lobby" });
  } catch (error) {
    errorMessage.value =
      error instanceof APIError && error.code === "invalid_player_name"
        ? "Use 1–24 letters, numbers, spaces, hyphens, or underscores."
        : "We couldn't sign you in. Check your connection and try again.";
  } finally {
    submitting.value = false;
  }
}

async function retryRestore() {
  await retryPlayerSessionRestore();
  if (player.value) await router.replace({ name: "lobby" });
}
</script>

<template>
  <main class="home">
    <section class="hero identity-card" aria-labelledby="page-title">
      <p class="eyebrow">Letterpress</p>
      <h1 id="page-title">What should we call you?</h1>
      <p class="introduction">
        Enter a name to register or return to your games. No password is needed.
      </p>

      <p v-if="restoring" class="notice" role="status" aria-live="polite">
        Checking for an existing session…
      </p>
      <div v-else-if="restoreError" class="session-recovery">
        <p class="notice notice--error" role="alert">{{ restoreError }}</p>
        <button class="secondary-action" type="button" @click="retryRestore">
          Try again
        </button>
      </div>
      <form v-else class="identity-form" novalidate @submit.prevent="submit">
        <label for="player-name">Player name</label>
        <input
          id="player-name"
          v-model="name"
          name="player-name"
          autocomplete="nickname"
          maxlength="24"
          :aria-describedby="
            errorMessage ? 'name-help name-error' : 'name-help'
          "
          :aria-invalid="Boolean(errorMessage)"
          :disabled="submitting"
          autofocus
        />
        <p id="name-help" class="field-help">
          Letters, numbers, spaces, hyphens, and underscores are welcome.
        </p>
        <p
          v-if="errorMessage"
          id="name-error"
          class="notice notice--error"
          role="alert"
        >
          {{ errorMessage }}
        </p>
        <button class="primary-action" type="submit" :disabled="submitting">
          {{ submitting ? "Entering…" : "Enter Letterpress" }}
        </button>
      </form>

      <aside class="identity-warning" aria-label="Important account limitation">
        <strong>Names are not private.</strong>
        Anyone who enters the same name can access that player and their games.
      </aside>
    </section>
  </main>
</template>
