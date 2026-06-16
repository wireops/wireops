import { describe, it, expect } from 'vitest'
import { translateCron } from './cron-translator'

describe('translateCron', () => {
  it('translates presets correctly', () => {
    expect(translateCron('* * * * *')).toBe('Runs every minute')
    expect(translateCron('*/5 * * * *')).toBe('Runs every 5 minutes')
    expect(translateCron('0 * * * *')).toBe('Runs every hour (at minute 0)')
    expect(translateCron('0 0 * * *')).toBe('Runs daily at midnight (00:00)')
  })

  it('translates specific time formats correctly', () => {
    expect(translateCron('30 2 * * 1')).toBe('Runs at 02:30 on Monday')
    expect(translateCron('0 12 15 * *')).toBe('Runs at 12:00 on day 15 of the month')
  })

  it('translates step expressions correctly', () => {
    expect(translateCron('*/15 0-4 * * *')).toBe('Runs every 15 minutes at hours (0-4)')
  })

  it('returns invalid format messages for incorrect cron expressions', () => {
    expect(translateCron('invalid')).toBe('Invalid cron expression (must be 5 fields)')
    expect(translateCron('* * * *')).toBe('Invalid cron expression (must be 5 fields)')
  })
})
