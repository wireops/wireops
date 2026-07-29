import { driver } from 'driver.js'
import 'driver.js/dist/driver.css'

const MOBILE_BREAKPOINT = 1024

type TourStepDef = {
  key: string
  title: string
  description: string
  mobileDescription?: string
}

export function useOnboardingTour() {
  const { $pb } = useNuxtApp()
  const { user } = useAuth()
  const { isViewer } = usePermissions()
  const mobileMenuOpen = useState('mobile_menu_open', () => false)

  const shouldShowTour = computed(() => user.value != null && !user.value.tour_completed)

  const completeTour = async (isMobile = false) => {
    if (isMobile) mobileMenuOpen.value = false

    const userId = user.value?.id
    if (!userId) return
    if (user.value) user.value.tour_completed = true
    try {
      await $pb.collection('users').update(userId, { tour_completed: true })
    } catch (e) {
      // eslint-disable-next-line no-console
      console.warn('[onboarding-tour] failed to persist tour_completed:', e)
    }
  }

  const startTour = async () => {
    const isMobile = window.innerWidth < MOBILE_BREAKPOINT
    const prefix = isMobile ? 'mobile-nav' : 'nav'

    if (isMobile) {
      mobileMenuOpen.value = true
      await nextTick()
    }

    const navSteps: TourStepDef[] = [
      {
        key: 'dashboard',
        title: 'Dashboard',
        description: 'Overview of your stacks, jobs, and workers.',
      },
      {
        key: 'workloads',
        title: 'Workloads',
        description: 'Manage Stacks (Docker Compose) and Jobs (scheduled tasks) from here.',
      },
      {
        key: 'repositories',
        title: 'Repositories',
        description: 'Connect Git repositories to sync deploys automatically.',
      },
      {
        key: 'secrets',
        title: 'Secrets',
        description: 'Store sensitive variables and keys used by your stacks.',
      },
      ...(isViewer.value
        ? []
        : [{
            key: 'workers',
            title: 'Workers',
            description: 'Connected remote hosts that run your deploys.',
          }]),
      {
        key: 'search',
        title: 'Quick search',
        description: 'Press ⌘K anytime to navigate the app quickly.',
        mobileDescription: 'Tap here anytime to navigate the app quickly.',
      },
    ]

    const steps = navSteps
      .map(s => ({
        element: `[data-tour="${prefix}-${s.key}"]`,
        popover: {
          title: s.title,
          description: isMobile && s.mobileDescription ? s.mobileDescription : s.description,
        },
      }))
      .filter(step => document.querySelector(step.element))

    if (steps.length === 0) {
      completeTour(isMobile)
      return
    }

    const tourDriver = driver({
      showProgress: true,
      allowClose: true,
      nextBtnText: 'Next',
      prevBtnText: 'Previous',
      doneBtnText: 'Done',
      popoverClass: 'wireops-tour-popover',
      steps,
      onDestroyStarted: () => {
        tourDriver.destroy()
        completeTour(isMobile)
      },
    })

    tourDriver.drive()
  }

  return { shouldShowTour, startTour, completeTour }
}
