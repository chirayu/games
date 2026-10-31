<script setup lang="ts">
import { computed, ref } from "vue";

import { createGame, exchangeSession, type CreatedGame } from "../api/games";

const created = ref<CreatedGame>();
const invitationLink = ref("");
const busy = ref(false);
const error = ref("");
const copied = ref(false);

const buttonLabel = computed(() => {
  if (busy.value) return "Creating game…";
  if (created.value) return "Try session again";
  return "Create game";
});

async function startGame() {
  busy.value = true;
  error.value = "";
  copied.value = false;
  try {
    if (!created.value) {
      created.value = await createGame();
    }
    await exchangeSession(created.value.gameId, created.value.playerOneToken);
    const invitation = new URL(
      `/games/${encodeURIComponent(created.value.gameId)}/join`,
      window.location.origin,
    );
    invitation.hash = new URLSearchParams({
      token: created.value.playerTwoToken,
    }).toString();
    invitationLink.value = invitation.toString();
  } catch {
    error.value =
      "We couldn’t create your game. Check your connection and try again.";
  } finally {
    busy.value = false;
  }
}

async function copyInvitation() {
  try {
    await navigator.clipboard.writeText(invitationLink.value);
    copied.value = true;
  } catch {
    copied.value = false;
    error.value =
      "Copy failed. Select the invitation link and copy it manually.";
  }
}
</script>

<template>
  <main class="page-shell">
    <section class="flow-card" aria-labelledby="create-title">
      <p class="eyebrow">New game</p>
      <h1 id="create-title">Invite a worthy opponent.</h1>
      <p class="introduction">
        We’ll create a private game and give you a single invitation link for
        Player 2.
      </p>

      <button
        v-if="!invitationLink"
        class="primary-action"
        type="button"
        :disabled="busy"
        @click="startGame"
      >
        {{ buttonLabel }}
      </button>

      <p v-if="error" class="notice notice--error" role="alert">{{ error }}</p>

      <div v-if="invitationLink" class="invitation" aria-live="polite">
        <h2>Your game is ready</h2>
        <p>
          Send this private link to Player 2. Anyone with the link can take that
          seat.
        </p>
        <label for="invitation-link">Player 2 invitation</label>
        <div class="copy-row">
          <input
            id="invitation-link"
            :value="invitationLink"
            readonly
            @focus="($event.target as HTMLInputElement).select()"
          />
          <button
            class="secondary-action"
            type="button"
            @click="copyInvitation"
          >
            {{ copied ? "Copied" : "Copy link" }}
          </button>
        </div>
        <RouterLink
          class="text-link"
          :to="`/games/${encodeURIComponent(created!.gameId)}`"
        >
          Continue to your game
        </RouterLink>
      </div>
    </section>
  </main>
</template>
