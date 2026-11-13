import { render, screen } from '@testing-library/react';
import { TrendDataTable } from './TrendDataTable';
import type { TagWithHierarchy, TrendDataPoint } from '@/types/trend';

const tag = (id: number, code: string, data_type: TagWithHierarchy['data_type']): TagWithHierarchy =>
    ({ id, gateway_id: 1, code, data_type, historize: true });

describe('TrendDataTable', () => {
    it('renders one header per selected tag and one row per timestamp, newest first', () => {
        const t0 = Date.UTC(2026, 0, 1, 8, 0, 0);
        const data = new Map<number, TrendDataPoint[]>([
            [1, [{ timestamp: t0, value: 12.5, quality: 0 }, { timestamp: t0 + 60_000, value: 13.25, quality: 0 }]],
            [2, [{ timestamp: t0 + 60_000, value: 1, quality: 0 }]],
        ]);
        const { container } = render(
            <TrendDataTable data={data} tags={[tag(1, 'Pressure', 'REAL'), tag(2, 'Pump', 'BOOL')]} selectedTagIds={[1, 2]} />,
        );

        // Date, time and the two tags.
        expect(container.querySelectorAll('thead th')).toHaveLength(4);
        expect(screen.getByText('Pressure')).toBeTruthy();
        expect(screen.getByText('Pump')).toBeTruthy();

        const rows = container.querySelectorAll('tbody tr');
        expect(rows).toHaveLength(2);
        // Every row has a cell for each column.
        rows.forEach(r => expect(r.querySelectorAll('td')).toHaveLength(4));
        // Newest first; the second tag has no reading yet at t0.
        expect(rows[0].textContent).toContain('13.25');
        expect(rows[1].textContent).toContain('12.5');
    });
});
