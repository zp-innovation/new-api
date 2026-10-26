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
import { useCallback, useState } from 'react'

import { extractAuthorizationCode } from '../../lib/oauth-callback'

/**
 * Holds the pasted authorization code for the panels that exchange one.
 *
 * Operators usually copy the whole address bar, so the paste is reduced to the
 * `code` parameter while a bare code keeps working. Both the code and whether
 * it was extracted from a URL stay together, because the panels render the
 * "code extracted" hint from the same state.
 */
export function useAuthorizationCodeInput() {
  const [code, setCode] = useState('')
  const [codeExtracted, setCodeExtracted] = useState(false)

  const handleCodeChange = useCallback((raw: string) => {
    const nextCode = extractAuthorizationCode(raw)
    setCode(nextCode)
    setCodeExtracted(nextCode !== raw.trim())
  }, [])

  const resetCode = useCallback(() => {
    setCode('')
    setCodeExtracted(false)
  }, [])

  return { code, codeExtracted, handleCodeChange, resetCode }
}
