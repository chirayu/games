<script setup lang="ts">
import ServerStatus from "../components/ServerStatus.vue";

const tiles = ["L", "E", "T", "T", "E", "R", "P", "R", "E", "S", "S"];
</script>

<template>
  <main class="home">
    <section class="hero" aria-labelledby="page-title">
      <div class="wordmark" aria-hidden="true">
        <span
          v-for="(letter, index) in tiles"
          :key="`${letter}-${index}`"
          class="tile"
        >
          {{ letter }}
        </span>
      </div>

      <p class="eyebrow">A game of words and territory</p>
      <h1 id="page-title">Claim the board, one word at a time.</h1>
      <p class="introduction">
        Letterpress is a thoughtful two-player word game designed for turns that
        can unfold across an afternoon—or across a week.
      </p>

      <ServerStatus />
    </section>
  </main>
</template>
