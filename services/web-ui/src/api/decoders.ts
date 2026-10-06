import apiClient from './client';

export interface DecoderPreset {
    id: string;
    name: string;
    script: string;
}

export interface DecodedSample {
    ts: number;
    time: string;
    values: Record<string, unknown>;
}

export interface DecoderTestResult {
    samples: DecodedSample[];
    logs?: string[];
    error?: string;
}

export type PayloadEncoding = 'text' | 'hex' | 'base64';

export const decodersApi = {
    presets: async (): Promise<DecoderPreset[]> => (await apiClient.get('/decoders/presets')).data,
    /** Runs a decoder on one pasted message. Nothing is stored. */
    test: async (req: {
        preset?: string; script?: string; payload: string; payload_encoding: PayloadEncoding; topic: string;
    }): Promise<DecoderTestResult> => (await apiClient.post('/decoders/test', req)).data,
};
