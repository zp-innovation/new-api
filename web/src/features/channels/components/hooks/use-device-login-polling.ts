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
import { useCallback, useEffect, useRef, useState } from 'react'
import { toast } from 'sonner'

import { handleServerError } from '@/lib/handle-server-error'

export type DeviceLoginPhase = 'idle' | 'pending' | 'ready' | 'expired' | 'denied'

export type DeviceLoginStatus =
  | 'pending'
  | 'slow_down'
  | 'ready'
  | 'expired'
  | 'denied'
  | 'error'

export type DeviceLoginStart = {
  authUrl: string
  sessionId: string
  userCode?: string
  /** Interval chosen by the upstream; the loop never polls faster than one second. */
  intervalMs?: number
}

export type DeviceLoginPoll = {
  status: DeviceLoginStatus
  credential?: string
  intervalMs?: number
}

export type DeviceLoginMessages = {
  startFailed: string
  pollFailed: string
  created: string
  denied: string
  expired: string
}

export type DeviceLoginPollingOptions = {
  startLogin: () => Promise<DeviceLoginStart>
  pollLogin: (sessionId: string) => Promise<DeviceLoginPoll>
  onCredential: (credential: string) => void
  messages: DeviceLoginMessages
  defaultIntervalMs?: number
}

const MIN_POLL_INTERVAL_MS = 1000
const DEFAULT_POLL_INTERVAL_MS = 2000

/**
 * Drives one device-authorization attempt: open the upstream link, poll until the
 * credential is issued, and stop on denial, expiry, failure or unmount. Panels
 * that differ only in their endpoints and copy share this lifecycle.
 */
export function useDeviceLoginPolling(options: DeviceLoginPollingOptions) {
  const [phase, setPhase] = useState<DeviceLoginPhase>('idle')
  const [authUrl, setAuthUrl] = useState('')
  const [userCode, setUserCode] = useState('')
  const [starting, setStarting] = useState(false)
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  // Callers pass fresh closures every render; the loop reads the latest ones
  // without restarting the timer it already scheduled.
  const optionsRef = useRef(options)
  useEffect(() => {
    optionsRef.current = options
  }, [options])

  const stopPolling = useCallback(() => {
    if (timerRef.current !== null) {
      clearTimeout(timerRef.current)
      timerRef.current = null
    }
  }, [])

  // A poll in flight when the drawer closes would keep hitting the upstream
  // console, so the loop dies with the panel.
  useEffect(() => stopPolling, [stopPolling])

  const pollOnce = useCallback(
    async function pollOnce(sessionId: string, intervalMs: number) {
      const { messages, onCredential, pollLogin } = optionsRef.current
      try {
        const polled = await pollLogin(sessionId)
        if (polled.status === 'ready' && polled.credential) {
          stopPolling()
          setPhase('ready')
          onCredential(polled.credential)
          toast.success(messages.created)
          return
        }
        if (polled.status === 'pending' || polled.status === 'slow_down') {
          const nextIntervalMs = Math.max(
            MIN_POLL_INTERVAL_MS,
            polled.intervalMs ?? intervalMs
          )
          timerRef.current = setTimeout(
            () => void pollOnce(sessionId, nextIntervalMs),
            nextIntervalMs
          )
          return
        }
        stopPolling()
        setPhase(polled.status === 'denied' ? 'denied' : 'expired')
        toast.error(
          polled.status === 'denied' ? messages.denied : messages.expired
        )
      } catch (error) {
        stopPolling()
        setPhase('expired')
        handleServerError(error, messages.pollFailed)
      }
    },
    [stopPolling]
  )

  const start = useCallback(async () => {
    const { defaultIntervalMs, messages, startLogin } = optionsRef.current
    stopPolling()
    setStarting(true)
    setPhase('idle')
    setAuthUrl('')
    setUserCode('')
    try {
      const started = await startLogin()
      setAuthUrl(started.authUrl)
      setUserCode(started.userCode ?? '')
      setPhase('pending')
      window.open(started.authUrl, '_blank', 'noopener,noreferrer')
      const intervalMs = Math.max(
        MIN_POLL_INTERVAL_MS,
        started.intervalMs ?? defaultIntervalMs ?? DEFAULT_POLL_INTERVAL_MS
      )
      timerRef.current = setTimeout(
        () => void pollOnce(started.sessionId, intervalMs),
        intervalMs
      )
    } catch (error) {
      stopPolling()
      setPhase('expired')
      handleServerError(error, messages.startFailed)
    } finally {
      setStarting(false)
    }
  }, [pollOnce, stopPolling])

  return { phase, authUrl, userCode, starting, start, stopPolling }
}
