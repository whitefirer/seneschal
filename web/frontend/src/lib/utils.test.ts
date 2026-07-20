import { describe, it, expect } from 'vitest'
import { formatDuration } from './utils'

describe('formatDuration', () => {
  it('formats sub-second durations as milliseconds', () => {
    expect(formatDuration(500)).toBe('500ms')
  })

  it('formats whole seconds below one minute', () => {
    expect(formatDuration(1000)).toBe('1s')
    expect(formatDuration(59000)).toBe('59s')
  })

  it('formats minutes with remaining seconds', () => {
    expect(formatDuration(83000)).toBe('1m 23s')
  })

  it('formats exact minutes with zero remaining seconds', () => {
    expect(formatDuration(60000)).toBe('1m 0s')
  })
})
