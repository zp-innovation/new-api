/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { act, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import {
  useDeviceLoginPolling,
  type DeviceLoginMessages,
  type DeviceLoginPoll,
  type DeviceLoginPollingOptions,
} from '../use-device-login-polling'

const messages: DeviceLoginMessages = {
  startFailed: 'failed to start',
  pollFailed: 'failed to poll',
  created: 'credential created',
  denied: 'request denied',
  expired: 'request expired',
}

function setup(overrides: Partial<DeviceLoginPollingOptions> = {}) {
  const onCredential = vi.fn()
  const startLogin = vi.fn(async () => ({
    authUrl: 'https://console.example.com/authorize',
    sessionId: 'session-1',
    intervalMs: 1000,
  }))
  const pollLogin = vi.fn<() => Promise<DeviceLoginPoll>>(async () => ({
    status: 'pending',
  }))
  const hook = renderHook(() =>
    useDeviceLoginPolling({
      startLogin,
      pollLogin,
      onCredential,
      messages,
      ...overrides,
    })
  )
  return { hook, onCredential, startLogin, pollLogin }
}

describe('useDeviceLoginPolling', () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    vi.spyOn(window, 'open').mockImplementation(() => null)
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  test('keeps polling until the device login issues the credential', async () => {
    const pollLogin = vi
      .fn<() => Promise<DeviceLoginPoll>>()
      .mockResolvedValueOnce({ status: 'pending' })
      .mockResolvedValueOnce({ status: 'ready', credential: 'issued' })
    const { hook, onCredential, startLogin } = setup({ pollLogin })

    await act(async () => {
      await hook.result.current.start()
    })
    expect(startLogin).toHaveBeenCalledTimes(1)
    expect(window.open).toHaveBeenCalledWith(
      'https://console.example.com/authorize',
      '_blank',
      'noopener,noreferrer'
    )
    expect(hook.result.current.phase).toBe('pending')
    expect(hook.result.current.authUrl).toBe(
      'https://console.example.com/authorize'
    )

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1000)
    })
    expect(pollLogin).toHaveBeenCalledTimes(1)
    expect(onCredential).not.toHaveBeenCalled()

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1000)
    })
    expect(onCredential).toHaveBeenCalledWith('issued')
    expect(hook.result.current.phase).toBe('ready')
  })

  test('honours the interval the upstream asks for on slow_down', async () => {
    const pollLogin = vi
      .fn<() => Promise<DeviceLoginPoll>>()
      .mockResolvedValueOnce({ status: 'slow_down', intervalMs: 4000 })
      .mockResolvedValue({ status: 'pending' })
    const { hook } = setup({ pollLogin })

    await act(async () => {
      await hook.result.current.start()
    })
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1000)
    })
    expect(pollLogin).toHaveBeenCalledTimes(1)

    await act(async () => {
      await vi.advanceTimersByTimeAsync(4000)
    })
    expect(pollLogin).toHaveBeenCalledTimes(2)
  })

  test('stops on a denied authorization and never issues a credential', async () => {
    const pollLogin = vi
      .fn<() => Promise<DeviceLoginPoll>>()
      .mockResolvedValue({ status: 'denied' })
    const { hook, onCredential } = setup({ pollLogin })

    await act(async () => {
      await hook.result.current.start()
    })
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1000)
    })
    expect(hook.result.current.phase).toBe('denied')

    await act(async () => {
      await vi.advanceTimersByTimeAsync(10000)
    })
    expect(pollLogin).toHaveBeenCalledTimes(1)
    expect(onCredential).not.toHaveBeenCalled()
  })

  test('reports a failed start without scheduling a poll', async () => {
    const startLogin = vi.fn(async () => {
      throw new Error('upstream refused')
    })
    const { hook, pollLogin } = setup({ startLogin })

    await act(async () => {
      await hook.result.current.start()
    })
    expect(hook.result.current.phase).toBe('expired')
    expect(hook.result.current.starting).toBe(false)

    await act(async () => {
      await vi.advanceTimersByTimeAsync(10000)
    })
    expect(pollLogin).not.toHaveBeenCalled()
  })

  test('clears the pending poll when the panel unmounts', async () => {
    const { hook, pollLogin } = setup()

    await act(async () => {
      await hook.result.current.start()
    })
    hook.unmount()

    await act(async () => {
      await vi.advanceTimersByTimeAsync(10000)
    })
    expect(pollLogin).not.toHaveBeenCalled()
  })
})
