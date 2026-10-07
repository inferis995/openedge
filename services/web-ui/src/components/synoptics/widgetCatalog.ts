import type { SynopticWidget } from '@/api/synoptics';

export interface WidgetCatalogEntry {
    type: SynopticWidget['type'];
    label: string;
    needsTag: boolean;
    defaultW: number;
    defaultH: number;
}

// label is an i18n key: render it with t(label).
// Lives here (not in SynopticWidget.tsx) so that file only exports
// components — mixing values and components breaks fast refresh lint.
export const WIDGET_CATALOG: WidgetCatalogEntry[] = [
    { type: 'value',    label: 'synopticWidget.type_value',    needsTag: true,  defaultW: 110, defaultH: 56 },
    { type: 'gauge',    label: 'synopticWidget.type_gauge',    needsTag: true,  defaultW: 90,  defaultH: 90 },
    { type: 'tank',     label: 'synopticWidget.type_tank',     needsTag: true,  defaultW: 70,  defaultH: 110 },
    { type: 'bargraph', label: 'synopticWidget.type_bargraph',      needsTag: true,  defaultW: 120, defaultH: 30 },
    { type: 'indicator',label: 'synopticWidget.type_indicator',      needsTag: true,  defaultW: 50,  defaultH: 56 },
    { type: 'pump',     label: 'synopticWidget.type_pump',     needsTag: true,  defaultW: 70,  defaultH: 70 },
    { type: 'valve',    label: 'synopticWidget.type_valve',    needsTag: true,  defaultW: 70,  defaultH: 70 },
    { type: 'motor',    label: 'synopticWidget.type_motor',    needsTag: true,  defaultW: 80,  defaultH: 70 },
    { type: 'fan',        label: 'synopticWidget.type_fan',        needsTag: true,  defaultW: 70,  defaultH: 80 },
    { type: 'conveyor',   label: 'synopticWidget.type_conveyor',   needsTag: true,  defaultW: 160, defaultH: 60 },
    { type: 'mixer',      label: 'synopticWidget.type_mixer',      needsTag: true,  defaultW: 80,  defaultH: 100 },
    { type: 'compressor', label: 'synopticWidget.type_compressor', needsTag: true,  defaultW: 70,  defaultH: 80 },
    { type: 'heater',     label: 'synopticWidget.type_heater',     needsTag: true,  defaultW: 80,  defaultH: 70 },
    { type: 'silo',       label: 'synopticWidget.type_silo',       needsTag: true,  defaultW: 80,  defaultH: 120 },
    { type: 'button',   label: 'synopticWidget.type_button',   needsTag: true,  defaultW: 90,  defaultH: 44 },
    { type: 'setpoint', label: 'synopticWidget.type_setpoint', needsTag: true,  defaultW: 110, defaultH: 70 },
    { type: 'pipe',     label: 'synopticWidget.type_pipe',     needsTag: false, defaultW: 120, defaultH: 14 },
    { type: 'label',    label: 'synopticWidget.type_label',    needsTag: false, defaultW: 100, defaultH: 30 },
    { type: 'image',    label: 'synopticWidget.type_image',    needsTag: false, defaultW: 200, defaultH: 150 },
    { type: 'clock',    label: 'synopticWidget.type_clock',    needsTag: false, defaultW: 130, defaultH: 56 },
];
