import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { computed, nextTick, ref } from 'vue'

const driveMock = vi.fn()
const destroyMock = vi.fn()
let lastDriverConfig: any = null

vi.mock('driver.js', () => ({
  driver: vi.fn((config: any) => {
    lastDriverConfig = config
    return { drive: driveMock, destroy: destroyMock }
  }),
}))
vi.mock('driver.js/dist/driver.css', () => ({}))

function setViewportWidth(width: number) {
  Object.defineProperty(window, 'innerWidth', { configurable: true, value: width })
}

function mountTourTargets(prefix: 'nav' | 'mobile-nav') {
  document.body.innerHTML = ['dashboard', 'workloads', 'repositories', 'secrets', 'workers', 'search']
    .map(key => `<div data-tour="${prefix}-${key}"></div>`)
    .join('')
}

describe('useOnboardingTour', () => {
  let updateMock: ReturnType<typeof vi.fn>
  let userRef: ReturnType<typeof ref>

  beforeEach(() => {
    vi.resetModules()
    driveMock.mockClear()
    destroyMock.mockClear()
    lastDriverConfig = null
    document.body.innerHTML = ''
    setViewportWidth(1280)

    userRef = ref({ id: 'user-1', tour_completed: false })
    updateMock = vi.fn().mockResolvedValue({})

    const stateRegistry = new Map<string, ReturnType<typeof ref>>()
    ;(globalThis as any).computed = computed
    ;(globalThis as any).nextTick = nextTick
    ;(globalThis as any).useState = (key: string, init: () => any) => {
      if (!stateRegistry.has(key)) stateRegistry.set(key, ref(init()))
      return stateRegistry.get(key)
    }
    ;(globalThis as any).useNuxtApp = () => ({
      $pb: { collection: () => ({ update: updateMock }) },
    })
    ;(globalThis as any).useAuth = () => ({ user: userRef })
    ;(globalThis as any).usePermissions = () => ({ isViewer: ref(false) })
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('starts the desktop tour with nav-prefixed steps and persists completion when the tour is dismissed', async () => {
    mountTourTargets('nav')
    const { useOnboardingTour } = await import('./useOnboardingTour')
    const { startTour } = useOnboardingTour()

    await startTour()

    expect(driveMock).toHaveBeenCalledTimes(1)
    expect(lastDriverConfig.steps.map((s: any) => s.element)).toEqual([
      '[data-tour="nav-dashboard"]',
      '[data-tour="nav-workloads"]',
      '[data-tour="nav-repositories"]',
      '[data-tour="nav-secrets"]',
      '[data-tour="nav-workers"]',
      '[data-tour="nav-search"]',
    ])
    expect(lastDriverConfig.steps.at(-1).popover.description).toBe('Press ⌘K anytime to navigate the app quickly.')

    lastDriverConfig.onDestroyStarted()
    expect(destroyMock).toHaveBeenCalledTimes(1)
    await vi.waitFor(() => expect(updateMock).toHaveBeenCalledWith('user-1', { tour_completed: true }))
    expect(userRef.value.tour_completed).toBe(true)
  })

  it('uses the mobile-nav prefix, opens the mobile menu, and swaps in the mobile search description below the breakpoint', async () => {
    setViewportWidth(500)
    mountTourTargets('mobile-nav')
    const { useOnboardingTour } = await import('./useOnboardingTour')
    const { startTour } = useOnboardingTour()

    const mobileMenuOpen = (globalThis as any).useState('mobile_menu_open', () => false)

    await startTour()

    expect(mobileMenuOpen.value).toBe(true)
    expect(lastDriverConfig.steps[0].element).toBe('[data-tour="mobile-nav-dashboard"]')
    expect(lastDriverConfig.steps.at(-1).popover.description).toBe('Tap here anytime to navigate the app quickly.')
  })

  it('excludes the Workers step for viewers even when its target is present', async () => {
    ;(globalThis as any).usePermissions = () => ({ isViewer: ref(true) })
    mountTourTargets('nav')
    const { useOnboardingTour } = await import('./useOnboardingTour')
    const { startTour } = useOnboardingTour()

    await startTour()

    const elements = lastDriverConfig.steps.map((s: any) => s.element)
    expect(elements).not.toContain('[data-tour="nav-workers"]')
    expect(elements).toEqual([
      '[data-tour="nav-dashboard"]',
      '[data-tour="nav-workloads"]',
      '[data-tour="nav-repositories"]',
      '[data-tour="nav-secrets"]',
      '[data-tour="nav-search"]',
    ])
  })

  it('persists completion immediately without starting driver.js when no step targets exist in the DOM', async () => {
    const { useOnboardingTour } = await import('./useOnboardingTour')
    const { startTour } = useOnboardingTour()

    await startTour()

    expect(driveMock).not.toHaveBeenCalled()
    await vi.waitFor(() => expect(updateMock).toHaveBeenCalledWith('user-1', { tour_completed: true }))
    expect(userRef.value.tour_completed).toBe(true)
  })
})
