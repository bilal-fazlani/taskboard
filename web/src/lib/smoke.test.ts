import { describe, expect, it } from 'vitest'

describe('web test setup', () => {
  it('runs TypeScript tests', () => {
    const sum = (values: number[]): number => values.reduce((total, n) => total + n, 0)
    expect(sum([1, 2, 3])).toBe(6)
  })

  it('runs in Node without a DOM', () => {
    expect(typeof document).toBe('undefined')
  })
})
