import { describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { ref } from 'vue'
import AccountPage from '../account.vue'

function setupGlobals() {
  const toastAdd = vi.fn()
  const updateSelf = vi.fn().mockResolvedValue({ id: 'user-1', name: 'New Name', email: 'user@example.com' })
  const user = ref({ id: 'user-1', name: 'Old Name', email: 'user@example.com' })

  ;(globalThis as any).useAuth = () => ({ user })
  ;(globalThis as any).useApi = () => ({ updateSelf })
  ;(globalThis as any).useToast = () => ({ add: toastAdd })

  return { toastAdd, updateSelf, user }
}

describe('account.vue', () => {
  it('saves the profile name via updateSelf', async () => {
    const { updateSelf, toastAdd } = setupGlobals()

    const wrapper = mount(AccountPage, {
      global: { stubs: { transition: false } },
      shallow: true,
    })
    await flushPromises()

    ;(wrapper.vm as any).profileForm.name = 'New Name'
    await (wrapper.vm as any).handleSaveProfile()

    expect(updateSelf).toHaveBeenCalledWith({ name: 'New Name' })
    expect(toastAdd).toHaveBeenCalledWith(expect.objectContaining({ title: 'Profile updated' }))
  })

  it('rejects blank password fields before calling the API', async () => {
    const { updateSelf, toastAdd } = setupGlobals()

    const wrapper = mount(AccountPage, {
      global: { stubs: { transition: false } },
      shallow: true,
    })
    await flushPromises()

    ;(wrapper.vm as any).passwordForm.oldPassword = ''
    ;(wrapper.vm as any).passwordForm.password = 'new-password'
    ;(wrapper.vm as any).passwordForm.passwordConfirm = 'new-password'
    await (wrapper.vm as any).handleChangePassword()

    expect(updateSelf).not.toHaveBeenCalled()
    expect(toastAdd).toHaveBeenCalledWith(expect.objectContaining({ title: 'All password fields are required' }))
  })

  it('rejects mismatched password confirmation before calling the API', async () => {
    const { updateSelf, toastAdd } = setupGlobals()

    const wrapper = mount(AccountPage, {
      global: { stubs: { transition: false } },
      shallow: true,
    })
    await flushPromises()

    ;(wrapper.vm as any).passwordForm.oldPassword = 'old-password'
    ;(wrapper.vm as any).passwordForm.password = 'new-password'
    ;(wrapper.vm as any).passwordForm.passwordConfirm = 'something-else'
    await (wrapper.vm as any).handleChangePassword()

    expect(updateSelf).not.toHaveBeenCalled()
    expect(toastAdd).toHaveBeenCalledWith(expect.objectContaining({ title: 'Passwords do not match' }))
  })

  it('changes the password and clears the form on success', async () => {
    const { updateSelf, toastAdd } = setupGlobals()

    const wrapper = mount(AccountPage, {
      global: { stubs: { transition: false } },
      shallow: true,
    })
    await flushPromises()

    ;(wrapper.vm as any).passwordForm.oldPassword = 'old-password'
    ;(wrapper.vm as any).passwordForm.password = 'new-password'
    ;(wrapper.vm as any).passwordForm.passwordConfirm = 'new-password'
    await (wrapper.vm as any).handleChangePassword()

    expect(updateSelf).toHaveBeenCalledWith({
      old_password: 'old-password',
      password: 'new-password',
      password_confirm: 'new-password',
    })
    expect(toastAdd).toHaveBeenCalledWith(expect.objectContaining({ title: 'Password updated' }))
    expect((wrapper.vm as any).passwordForm).toEqual({ oldPassword: '', password: '', passwordConfirm: '' })
  })

  it('surfaces a wrong-current-password error from the API', async () => {
    const toastAdd = vi.fn()
    const updateSelf = vi.fn().mockRejectedValue(new Error('current password is incorrect'))
    const user = ref({ id: 'user-1', name: 'Old Name', email: 'user@example.com' })
    ;(globalThis as any).useAuth = () => ({ user })
    ;(globalThis as any).useApi = () => ({ updateSelf })
    ;(globalThis as any).useToast = () => ({ add: toastAdd })

    const wrapper = mount(AccountPage, {
      global: { stubs: { transition: false } },
      shallow: true,
    })
    await flushPromises()

    ;(wrapper.vm as any).passwordForm.oldPassword = 'wrong-password'
    ;(wrapper.vm as any).passwordForm.password = 'new-password'
    ;(wrapper.vm as any).passwordForm.passwordConfirm = 'new-password'
    await (wrapper.vm as any).handleChangePassword()

    expect(toastAdd).toHaveBeenCalledWith(expect.objectContaining({
      title: 'Failed to update password',
      description: 'current password is incorrect',
    }))
  })
})
