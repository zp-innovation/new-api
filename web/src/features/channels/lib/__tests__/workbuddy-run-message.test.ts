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
import fs from 'node:fs/promises'
import path from 'node:path'

import { beforeAll, describe, expect, test } from 'vitest'

import {
  workBuddyRunMessage,
  type RunMessageTranslate,
} from '../workbuddy-run-message'

let zh: Record<string, string>
let t: RunMessageTranslate

beforeAll(async () => {
  const localesDir = path.resolve('src/i18n/locales')
  const locale = JSON.parse(
    await fs.readFile(path.join(localesDir, 'zh.json'), 'utf8')
  )
  zh = locale.translation
  t = (key, params) => {
    let value = zh[key] ?? key
    for (const [name, replacement] of Object.entries(params ?? {})) {
      value = value.replaceAll(`{{${name}}}`, String(replacement))
    }
    return value
  }
})

describe('workBuddyRunMessage', () => {
  test('translates a fixed success message', () => {
    expect(workBuddyRunMessage(t, 'streak state is fine')).toBe('连签状态正常')
  })

  test('translates every known prefix in a failure chain', () => {
    const raw =
      'daily chat failed: web daily chat failed: create conversation: workbuddy: upstream error code=10000'

    expect(workBuddyRunMessage(t, raw)).toBe(
      '每日对话失败: 网页端每日对话失败: create conversation: workbuddy: upstream error code=10000'
    )
  })

  test('does not leave the wire prefix inside the translated prefix', () => {
    const translated = workBuddyRunMessage(
      t,
      'daily chat failed: conversation did not finish in 2m0s (status=working)'
    )

    expect(translated.startsWith('每日对话失败: ')).toBe(true)
    expect(translated).not.toContain('daily chat failed')
    expect(translated).not.toContain('::')
  })

  test('keeps unknown upstream detail verbatim', () => {
    expect(workBuddyRunMessage(t, 'a brand new upstream error')).toBe(
      'a brand new upstream error'
    )
  })

  test('interpolates the streak day count', () => {
    expect(workBuddyRunMessage(t, 'activity reported (streak 9 days)')).toBe(
      '已上报活跃（连续 9 天）'
    )
  })

  test('translates growth log entries and their reasons', () => {
    expect(
      workBuddyRunMessage(
        t,
        '体验「资料库」 needs the desktop client (open the library in the desktop client and read the intro)'
      )
    ).toBe('体验「资料库」 需要在桌面端完成（在桌面端打开资料库并阅读简介）')
  })

  test('translates the accepted and real-action entries from the recent-runs example', () => {
    const raw =
      'accepted 1 task(s); 体验「资料库」 needs the desktop client (open the library in the desktop client and read the intro); 关注「腾讯 WorkBuddy」官方公众号 needs a real action (requires following the Tencent WorkBuddy WeChat account for 24 hours)'

    expect(workBuddyRunMessage(t, raw)).toBe(
      '已接受 1 个任务; 体验「资料库」 需要在桌面端完成（在桌面端打开资料库并阅读简介）; 关注「腾讯 WorkBuddy」官方公众号 需要真实操作（需关注腾讯 WorkBuddy 微信公众号满 24 小时）'
    )
  })

  test('translates cat travel outcomes', () => {
    expect(workBuddyRunMessage(t, 'already travelled today')).toBe(
      '今天已经旅行过了'
    )
    expect(workBuddyRunMessage(t, 'cat: travel reward claimed')).toBe(
      '猫猫旅行: 已领取旅行奖励'
    )
    expect(workBuddyRunMessage(t, 'the cat is on its way')).toBe('猫猫已经出发')
  })

  test('translates growth acceptance, progress and failure entries', () => {
    expect(
      workBuddyRunMessage(
        t,
        'accept failed for 2 task(s); claimed 完成任务 (+10); claim 金库 failed: quota; report 资料库 failed: timeout; 夜猫子 is not accepted yet, skipped; 任务 progress 2/5, claim deferred; completed 任务 (+20)'
      )
    ).toBe(
      '有 2 个任务接受失败; 已领取 完成任务（+10）; 领取 金库 失败：quota; 上报 资料库 失败：timeout; 夜猫子 尚未接受，已跳过; 任务 进度 2/5，暂缓领取; 已完成 任务（+20）'
    )
  })

  test('translates streak housekeeping entries', () => {
    expect(
      workBuddyRunMessage(
        t,
        'made up 2026-10-18 with a makeup card; claimed the starter gift: +300; claimed the campaign compensation: +100; redeemed streak tier 7; drew the growth lottery 2 time(s)'
      )
    ).toBe(
      '已使用补签卡补签 2026-10-18; 已领取新手礼包：+300; 已领取活动补偿：+100; 已兑换连续签到档位 7; 成长抽奖次数：2'
    )
  })

  test('translates growth list, acceptance and cat prefixes before raw details', () => {
    expect(workBuddyRunMessage(t, 'list growth tasks: timeout')).toBe(
      '获取成长任务列表: timeout'
    )
    expect(workBuddyRunMessage(t, 'accept: timeout')).toBe('接受任务: timeout')
    expect(workBuddyRunMessage(t, 'state: idle')).toBe('状态：idle')
  })
})
