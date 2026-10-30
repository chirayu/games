<script setup lang="ts">
import { onMounted, ref } from "vue";

import { getHealth } from "../api/health";

type Status = "checking" | "connected" | "unavailable";

const status = ref<Status>("checking");

onMounted(async () => {
  try {
    const health = await getHealth();
    status.value = health.status === "ok" ? "connected" : "unavailable";
  } catch {
    status.value = "unavailable";
  }
});
</script>

<template>
  <p class="server-status" :data-status="status" role="status">
    <span class="server-status__dot" aria-hidden="true"></span>
    <template v-if="status === 'checking'">Connecting to game server…</template>
    <template v-else-if="status === 'connected'"
      >Game server connected</template
    >
    <template v-else>Game server unavailable</template>
  </p>
</template>

<style scoped>
.server-status {
  align-items: center;
  color: var(--ink-muted);
  display: inline-flex;
  font-size: 0.875rem;
  gap: 0.55rem;
  margin: 0;
}

.server-status__dot {
  background: #d49b30;
  border-radius: 999px;
  box-shadow: 0 0 0 4px rgb(212 155 48 / 14%);
  height: 0.55rem;
  width: 0.55rem;
}

.server-status[data-status="connected"] .server-status__dot {
  background: #27845d;
  box-shadow: 0 0 0 4px rgb(39 132 93 / 14%);
}

.server-status[data-status="unavailable"] .server-status__dot {
  background: #b64d48;
  box-shadow: 0 0 0 4px rgb(182 77 72 / 14%);
}
</style>
