<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{ status: string }>()

const meta = computed(() => {
  switch (props.status) {
    case 'paid':       return { label: 'Paid',       color: 'var(--billable)' }
    case 'sent':       return { label: 'Sent',       color: 'var(--billable)' }
    case 'active':     return { label: 'Active',     color: 'var(--billable)' }
    case 'pending':    return { label: 'Pending',    color: 'var(--unbilled)' }
    case 'draft':      return { label: 'Draft',      color: 'var(--ink-3)'    }
    case 'overdue':    return { label: 'Overdue',    color: 'var(--overdue)'  }
    case 'cancelled':  return { label: 'Cancelled',  color: 'var(--overdue)'  }
    case 'on_hold':
    case 'paused':     return { label: 'Paused',     color: 'var(--ink-3)'    }
    case 'completed':  return { label: 'Completed',  color: 'var(--billable)' }
    case 'invoiced':   return { label: 'Invoiced',   color: 'var(--billable)' }
    case 'accepted':   return { label: 'Accepted',   color: 'var(--billable)' }
    case 'rejected':   return { label: 'Rejected',   color: 'var(--overdue)'  }
    case 'expired':    return { label: 'Expired',    color: 'var(--overdue)'  }
    case 'converted':  return { label: 'Converted',  color: 'var(--billable)' }
    default:           return { label: props.status, color: 'var(--ink-3)'    }
  }
})
</script>

<template>
  <span class="pill">
    <span class="d" :style="{ background: meta.color }" />
    {{ meta.label }}
  </span>
</template>

<style scoped>
.pill {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  font-family: var(--font-sans);
  font-size: 10.5px;
  letter-spacing: 0.03em;
  text-transform: uppercase;
  font-weight: 500;
  color: var(--ink-2);
  white-space: nowrap;
}
.d {
  width: 6px;
  height: 6px;
  border-radius: 6px;
}
</style>
