<script setup lang="ts">
const props = defineProps<{
  code: string
  class?: string
}>()

const colorMode = useColorMode()

function escapeHtml(text: string): string {
  return text
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;')
}

function highlightYaml(code: string): string {
  if (!code) return ''
  
  const lines = code.split('\n')
  const highlighted: string[] = []
  
  for (const line of lines) {
    let highlightedLine = escapeHtml(line)
    
    // Comments (after escaping)
    if (line.trim().startsWith('#')) {
      highlightedLine = `<span class="yaml-comment">${highlightedLine}</span>`
    }
    // Keys (word followed by colon)
    else if (/^\s*[\w-]+\s*:/.test(line)) {
      highlightedLine = highlightedLine.replace(
        /^(\s*)([\w-]+)(\s*:)/,
        '$1<span class="yaml-key">$2</span>$3'
      )
      
      // Check for values after colon
      const colonIndex = line.indexOf(':')
      const afterColon = colonIndex !== -1 ? line.substring(colonIndex + 1) : undefined
      if (afterColon) {
        const trimmed = afterColon.trim()
        
        // Strings in quotes
        if (/^(['"]).*\1$/.test(trimmed)) {
          highlightedLine = highlightedLine.replace(
            /(&quot;|&#39;)(.*?)(&quot;|&#39;)/,
            '<span class="yaml-string">$1$2$3</span>'
          )
        }
        // Numbers
        else if (/^-?\d+(?:\.\d*)?$/.test(trimmed)) {
          highlightedLine = highlightedLine.replace(
            /:(\s*)([-0-9.]+)(\s*)$/,
            ':$1<span class="yaml-number">$2</span>$3'
          )
        }
        // Booleans
        else if (/^(true|false|yes|no|on|off)$/i.test(trimmed)) {
          highlightedLine = highlightedLine.replace(
            /:(\s*)([a-zA-Z]+)(\s*)$/,
            ':$1<span class="yaml-boolean">$2</span>$3'
          )
        }
        // Null
        else if (/^(null|~)$/i.test(trimmed)) {
          highlightedLine = highlightedLine.replace(
            /:(\s*)(null|~)(\s*)$/i,
            ':$1<span class="yaml-null">$2</span>$3'
          )
        }
      }
    }
    // List items
    else if (/^\s*-\s/.test(line)) {
      highlightedLine = highlightedLine.replace(
        /^(\s*-\s)/,
        '<span class="yaml-operator">$1</span>'
      )
      
      // Check for quoted strings in list items
      if (/(['"]).*\1/.test(line)) {
        highlightedLine = highlightedLine.replace(
          /(&quot;|&#39;)(.*?)(&quot;|&#39;)/g,
          '<span class="yaml-string">$1$2$3</span>'
        )
      }
    }
    
    highlighted.push(highlightedLine)
  }
  
  return highlighted.join('\n')
}

const highlightedCode = computed(() => highlightYaml(props.code))
</script>

<template>
  <pre :class="['yaml-highlighter', colorMode.value === 'dark' ? 'dark-mode' : 'light-mode', props.class]"><code v-html="highlightedCode"/></pre>
</template>

<style scoped>
.yaml-highlighter {
  margin: 0;
  padding: 1rem;
  background-color: rgb(243 244 246); /* gray-100 */
  border-radius: 0.5rem;
  overflow-x: auto;
  font-family: 'Monaco', 'Menlo', 'Ubuntu Mono', 'Consolas', monospace;
  font-size: 0.875rem;
  line-height: 1.5;
  color: rgb(17 24 39); /* gray-900 */
}

.yaml-highlighter code {
  display: block;
  white-space: pre;
  color: inherit;
}

/* Keys - yellow to match project primary */
.yaml-highlighter :deep(.yaml-key) {
  color: rgb(202 138 4); /* yellow-600 */
  font-weight: 600;
}

/* Strings - verde mais claro */
.yaml-highlighter :deep(.yaml-string) {
  color: rgb(22 163 74); /* green-600 */
}

/* Numbers - laranja */
.yaml-highlighter :deep(.yaml-number) {
  color: rgb(234 88 12); /* orange-600 */
}

/* Booleans - roxo */
.yaml-highlighter :deep(.yaml-boolean) {
  color: rgb(147 51 234); /* purple-600 */
}

/* Null - cinza */
.yaml-highlighter :deep(.yaml-null) {
  color: rgb(107 114 128); /* gray-500 */
  font-style: italic;
}

/* Comments - cinza mais claro */
.yaml-highlighter :deep(.yaml-comment) {
  color: rgb(156 163 175); /* gray-400 */
  font-style: italic;
}

/* Operators - azul escuro */
.yaml-highlighter :deep(.yaml-operator) {
  color: rgb(37 99 235); /* blue-600 */
  font-weight: bold;
}

/* Dark mode */
.yaml-highlighter.dark-mode {
  background-color: rgb(3 7 18); /* gray-950 */
  color: rgb(243 244 246); /* gray-100 */
}

.yaml-highlighter.dark-mode :deep(.yaml-key) {
  color: rgb(250 204 21); /* yellow-400 */
}

.yaml-highlighter.dark-mode :deep(.yaml-string) {
  color: rgb(74 222 128); /* green-400 */
}

.yaml-highlighter.dark-mode :deep(.yaml-number) {
  color: rgb(251 146 60); /* orange-400 */
}

.yaml-highlighter.dark-mode :deep(.yaml-boolean) {
  color: rgb(192 132 252); /* purple-400 */
}

.yaml-highlighter.dark-mode :deep(.yaml-null) {
  color: rgb(156 163 175); /* gray-400 */
}

.yaml-highlighter.dark-mode :deep(.yaml-comment) {
  color: rgb(107 114 128); /* gray-500 */
}

.yaml-highlighter.dark-mode :deep(.yaml-operator) {
  color: rgb(96 165 250); /* blue-400 */
}
</style>
