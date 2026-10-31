<script setup lang="ts">
import { useRoute } from "vue-router";

const route = useRoute();
const gameID = String(route.params.gameId ?? "");
</script>

<template>
  <main class="page-shell">
    <section class="flow-card flow-card--compact" aria-labelledby="ready-title">
      <p class="eyebrow">Game {{ gameID }}</p>
      <h1 id="ready-title">Your seat is ready.</h1>
      <p class="introduction">
        You can safely refresh or return later on this device. The board arrives
        in the next wave.
      </p>
    </section>
  </main>
</template>
