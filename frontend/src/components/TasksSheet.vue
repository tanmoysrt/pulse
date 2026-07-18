<template>
  <div class="tasks-sheet">
    <ul class="tasks-list">
      <li v-for="(t, i) in todos" :key="i" :class="t.status">
        <span class="tick"></span>
        <span class="task-content">{{ t.content }}</span>
      </li>
    </ul>
  </div>
</template>

<script setup>
defineProps({ todos: { type: Array, default: () => [] } })
</script>
