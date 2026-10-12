import { describe, expect, it } from 'vitest';
import { timeAgo } from './relativeTime';

describe('timeAgo', () => {
    const now = Date.UTC(2026, 9, 12, 12, 0, 0);
    it('reads short and in the language', () => {
        expect(timeAgo(now - 10e3, 'it', now)).toBe('ora');
        expect(timeAgo(now - 2 * 60e3, 'it', now)).toBe('2 min fa');
        expect(timeAgo(now - 3 * 3600e3, 'en', now)).toBe('3 hr. ago');
    });
    it('is a dash for no time', () => {
        expect(timeAgo(undefined, 'it', now)).toBe('—');
        expect(timeAgo(Number.NaN, 'it', now)).toBe('—');
    });
});
