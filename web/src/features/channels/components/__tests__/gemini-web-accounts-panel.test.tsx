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
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createInstance } from 'i18next'
import { I18nextProvider } from 'react-i18next'
import { expect, test, vi } from 'vitest'

import en from '@/i18n/locales/en.json'
import zh from '@/i18n/locales/zh.json'

import {
  listGeminiWebAccounts,
  setGeminiWebAccountStatus,
} from '../../api'
import { GeminiWebAccountsPanel } from '../gemini-web-accounts-panel'

vi.mock('../../api', () => ({
  addGeminiWebAccount: vi.fn(),
  deleteGeminiWebAccount: vi.fn(),
  listGeminiWebAccounts: vi.fn(),
  setGeminiWebAccountStatus: vi.fn(),
}))

test('translates the account panel and account row in Chinese', async () => {
  vi.mocked(listGeminiWebAccounts).mockResolvedValue({
    success: true,
    data: [
      {
        id: 1,
        channel_id: 7,
        label: 'Gemini Web account',
        status: 'enabled',
        note: '',
        fail_count: 1,
        last_used_at: 0,
        last_ok_at: 0,
        last_error: 'no SNlM0e in page (cookie expired or not signed in)',
        created_at: 1,
        cookie_size: 6833,
      },
    ],
  })
  const i18n = createInstance()
  await i18n.init({
    lng: 'zh',
    fallbackLng: 'en',
    resources: { en, zh },
    nsSeparator: false,
  })

  render(
    <I18nextProvider i18n={i18n}>
      <GeminiWebAccountsPanel channelId={7} />
    </I18nextProvider>
  )

  const panelTitle = screen.getAllByText('Gemini Web 账号')[0]
  expect(panelTitle).toBeVisible()
  expect(
    screen.getByText(
      '粘贴从已登录 gemini.google.com 的浏览器里取到的完整 Cookie。必须包含 __Secure-1PSID、__Secure-1PSIDTS 和 SAPISID。'
    )
  ).toBeVisible()
  expect(screen.getByPlaceholderText('账号标签（可选）')).toBeVisible()
  expect(screen.getByRole('button', { name: '添加账号' })).toBeVisible()
  expect(
    screen.getByText(
      /Chrome 设备绑定 Cookie 可能无法通过 HTTP 轮换保活/
    )
  ).toBeVisible()

  const accountLabel = (
    await screen.findAllByText('Gemini Web 账号', { selector: 'span' })
  )[0]
  const accountRow = accountLabel.closest('li')
  if (!accountRow) throw new Error('account row not found')
  expect(within(accountRow).getByText(/已启用/)).toBeVisible()
  expect(within(accountRow).getByText(/6833 字节/)).toBeVisible()
  expect(
    within(accountRow).getByText(/Gemini Web Cookie 已失效或未登录/)
  ).toBeVisible()
})

test('parks an account by disabling it instead of deleting the cookie', async () => {
  const user = userEvent.setup()
  vi.mocked(listGeminiWebAccounts).mockResolvedValue({
    success: true,
    data: [
      {
        id: 1,
        channel_id: 7,
        label: 'Gemini Web account',
        status: 'enabled',
        note: '',
        fail_count: 0,
        last_used_at: 0,
        last_ok_at: 0,
        last_error: '',
        created_at: 1,
        cookie_size: 6833,
      },
    ],
  })
  vi.mocked(setGeminiWebAccountStatus).mockResolvedValue({
    success: true,
    message: '',
  })
  const i18n = createInstance()
  await i18n.init({ lng: 'zh', fallbackLng: 'en', resources: { en, zh } })

  render(
    <I18nextProvider i18n={i18n}>
      <GeminiWebAccountsPanel channelId={7} />
    </I18nextProvider>
  )

  const toggle = await screen.findByRole('switch', { name: '停用账号' })
  expect(toggle).toBeChecked()
  await user.click(toggle)

  await waitFor(() =>
    expect(setGeminiWebAccountStatus).toHaveBeenCalledWith(7, 1, 'disabled')
  )
  expect(listGeminiWebAccounts).toHaveBeenCalledTimes(2)
})
