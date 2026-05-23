import { nextTick } from 'vue'

type LivePoliteness = 'polite' | 'assertive'

type AnnouncementState = {
  polite: string
  assertive: string
}

export function useA11yAnnouncer() {
  const announcements = useState<AnnouncementState>('a11y-announcements', () => ({
    polite: '',
    assertive: '',
  }))

  async function announce(message: string, politeness: LivePoliteness = 'polite') {
    const trimmed = message.trim()
    if (!trimmed) return

    announcements.value[politeness] = ''
    await nextTick()
    announcements.value[politeness] = trimmed
  }

  return {
    announcements,
    announce,
  }
}
