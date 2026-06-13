import { describe, it, expect } from 'vitest'
import { cn } from './utils'

describe('cn (className utility)', () => {
    it('merges class names', () => {
        expect(cn('foo', 'bar')).toBe('foo bar')
    })

    it('deduplicates tailwind conflicting classes (last wins)', () => {
        expect(cn('p-2', 'p-4')).toBe('p-4')
        expect(cn('text-red-500', 'text-blue-500')).toBe('text-blue-500')
    })

    it('handles conditional class names', () => {
        expect(cn('base', false && 'hidden', 'visible')).toBe('base visible')
        expect(cn('base', true && 'active')).toBe('base active')
    })

    it('handles undefined and null gracefully', () => {
        expect(cn('base', undefined, null, 'end')).toBe('base end')
    })

    it('handles empty input', () => {
        expect(cn()).toBe('')
        expect(cn('')).toBe('')
    })

    it('handles array inputs', () => {
        expect(cn(['foo', 'bar'])).toBe('foo bar')
    })

    it('handles object inputs', () => {
        expect(cn({ 'bg-red-500': true, 'bg-blue-500': false })).toBe('bg-red-500')
    })
})
