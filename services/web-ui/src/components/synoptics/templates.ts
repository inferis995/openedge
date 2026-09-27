import i18n from '@/i18n';
import type { SynopticWidget } from '@/api/synoptics';

export type SynopticTemplateId = 'blank' | 'line' | 'machine' | 'tanks';

export const SYNOPTIC_TEMPLATES: SynopticTemplateId[] = ['blank', 'line', 'machine', 'tanks'];

/**
 * Starting layouts for a new synoptic page.
 *
 * An empty 1280×720 canvas asks a first-time user to know what a mimic
 * usually contains and where it goes. These put the usual symbols in the
 * usual places, with labels, and no tags: the editor's "Tag" field is where
 * each one is bound, and an unbound widget shows a sample value until then.
 */
export function templateLayout(id: SynopticTemplateId): SynopticWidget[] {
    const t = (k: string) => i18n.t(`synopticTemplates.${k}`);
    let n = 0;
    const w = (type: SynopticWidget['type'], x: number, y: number, width: number, height: number,
        label = '', config: SynopticWidget['config'] = {}): SynopticWidget =>
        ({ id: `tpl-${Date.now().toString(36)}-${n++}`, type, x, y, w: width, h: height, tagId: null, label, config });

    const title = (text: string) => w('label', 40, 24, 600, 44, text, { fontSize: 26, color: '#e2e8f0' });
    const clock = () => w('clock', 1110, 24, 130, 56);

    switch (id) {
        case 'line':
            return [
                title(t('line_title')), clock(),
                w('conveyor', 60, 200, 300, 80, t('infeed')),
                w('conveyor', 420, 200, 300, 80, t('process')),
                w('conveyor', 780, 200, 300, 80, t('outfeed')),
                w('motor', 170, 300, 80, 70, 'M1', { showStatus: true }),
                w('motor', 530, 300, 80, 70, 'M2', { showStatus: true }),
                w('motor', 890, 300, 80, 70, 'M3', { showStatus: true }),
                w('value', 60, 460, 180, 70, t('speed'), { unit: 'm/min', decimals: 1 }),
                w('value', 270, 460, 180, 70, t('pieces'), { decimals: 0 }),
                w('value', 480, 460, 180, 70, t('rejects'), { decimals: 0 }),
                w('indicator', 700, 470, 60, 60, t('line_alarm')),
                w('button', 1100, 200, 120, 50, t('start')),
                w('button', 1100, 270, 120, 50, t('stop')),
            ];
        case 'machine':
            return [
                title(t('machine_title')), clock(),
                w('motor', 60, 140, 200, 170, t('main_motor'), { showStatus: true }),
                w('gauge', 320, 130, 200, 200, t('temperature'), { unit: '°C', min: 0, max: 150 }),
                w('gauge', 560, 130, 200, 200, t('pressure'), { unit: 'bar', min: 0, max: 10 }),
                w('bargraph', 820, 150, 380, 44, t('load'), { unit: '%', min: 0, max: 100 }),
                w('value', 820, 230, 180, 70, t('run_hours'), { unit: 'h', decimals: 0 }),
                w('value', 1020, 230, 180, 70, t('cycles'), { decimals: 0 }),
                w('indicator', 60, 400, 60, 60, t('running')),
                w('indicator', 160, 400, 60, 60, t('fault')),
                w('setpoint', 320, 400, 200, 80, t('speed_sp'), { unit: 'rpm' }),
                w('button', 560, 410, 120, 50, t('start')),
                w('button', 700, 410, 120, 50, t('stop')),
            ];
        case 'tanks':
            return [
                title(t('tanks_title')), clock(),
                w('tank', 80, 140, 120, 220, t('tank_a'), { min: 0, max: 100, showPercentage: true }),
                w('pipe', 200, 328, 120, 18, '', { strokeWidth: 100 }),
                w('valve', 320, 300, 70, 70, 'V1'),
                w('pipe', 390, 328, 110, 18, '', { strokeWidth: 100 }),
                w('pump', 500, 300, 80, 80, 'P1', { showStatus: true }),
                w('pipe', 580, 328, 110, 18, '', { strokeWidth: 100 }),
                w('valve', 690, 300, 70, 70, 'V2'),
                w('pipe', 760, 328, 120, 18, '', { strokeWidth: 100 }),
                w('mixer', 880, 140, 140, 220, t('tank_b'), { min: 0, max: 100 }),
                w('silo', 1080, 120, 120, 240, t('silo'), { min: 0, max: 100, showPercentage: true }),
                w('value', 80, 400, 160, 64, t('level_a'), { unit: '%', decimals: 0 }),
                w('value', 880, 400, 160, 64, t('temperature'), { unit: '°C', decimals: 1 }),
                w('value', 500, 400, 160, 64, t('flow'), { unit: 'm³/h', decimals: 1 }),
            ];
        default:
            return [];
    }
}
