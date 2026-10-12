import { describe, expect, it } from 'vitest';
import { formatLiveValue, shortTagNames } from './format';

describe('formatLiveValue', () => {
    it('writes numbers in the language, with the tag\'s decimals', () => {
        expect(formatLiveValue(3.6, 2, 'it')).toBe('3,60');
        expect(formatLiveValue(2.354, undefined, 'it')).toBe('2,35');
        expect(formatLiveValue(2.5, undefined, 'en')).toBe('2.5');
        expect(formatLiveValue(null, 2, 'it')).toBe('—');
    });
});

describe('shortTagNames', () => {
    const names = (list: string[]) => list.map((name, i) => ({ id: i, name }));
    it('drops what every tag of the device shares, at a separator', () => {
        expect([...shortTagNames(names(['CARIATI_CA_P_1_AN1', 'CARIATI_CA_P_1_AN2', 'CARIATI_CA_P_1_BATTERIA'])).values()])
            .toEqual(['AN1', 'AN2', 'BATTERIA']);
        expect([...shortTagNames(names(['plant/line1/T1', 'plant/line1/T2'])).values()]).toEqual(['T1', 'T2']);
    });
    it('keeps whole names when they share nothing, and a lone one by its last segment', () => {
        expect([...shortTagNames(names(['Pressure', 'Flow'])).values()]).toEqual(['Pressure', 'Flow']);
        expect([...shortTagNames(names(['plant/line1/T1'])).values()]).toEqual(['T1']);
    });
});
