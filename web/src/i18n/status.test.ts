import { describe, expect, it } from 'vitest'
import { normalizeSeriesStatus } from './status'

describe('normalizeSeriesStatus', () => {
  it('maps known english aliases to canonical codes', () => {
    expect(normalizeSeriesStatus('ongoing')).toBe('ongoing')
    expect(normalizeSeriesStatus('publishing')).toBe('ongoing')
    expect(normalizeSeriesStatus('serializing')).toBe('ongoing')
    expect(normalizeSeriesStatus('complete')).toBe('completed')
    expect(normalizeSeriesStatus('finished')).toBe('completed')
    expect(normalizeSeriesStatus('paused')).toBe('hiatus')
    expect(normalizeSeriesStatus('dropped')).toBe('cancelled')
    expect(normalizeSeriesStatus('canceled')).toBe('cancelled')
  })

  it('is case-insensitive and trims surrounding whitespace', () => {
    expect(normalizeSeriesStatus('  Ongoing ')).toBe('ongoing')
    expect(normalizeSeriesStatus('COMPLETED')).toBe('completed')
    expect(normalizeSeriesStatus('Hiatus')).toBe('hiatus')
  })

  it('maps chinese aliases (not affected by lowercasing) to codes', () => {
    expect(normalizeSeriesStatus('连载中')).toBe('ongoing')
    expect(normalizeSeriesStatus('已完结')).toBe('completed')
    expect(normalizeSeriesStatus('休刊中')).toBe('hiatus')
    expect(normalizeSeriesStatus('有生之年')).toBe('hiatus')
    expect(normalizeSeriesStatus('已取消')).toBe('cancelled')
    expect(normalizeSeriesStatus('已放弃')).toBe('cancelled')
  })

  it('falls back to unknown for empty, null, undefined, or unrecognized values', () => {
    expect(normalizeSeriesStatus('')).toBe('unknown')
    expect(normalizeSeriesStatus('   ')).toBe('unknown')
    expect(normalizeSeriesStatus(null)).toBe('unknown')
    expect(normalizeSeriesStatus(undefined)).toBe('unknown')
    expect(normalizeSeriesStatus('something-else')).toBe('unknown')
  })
})
