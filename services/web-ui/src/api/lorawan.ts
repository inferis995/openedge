import apiClient from './client';

export interface LoRaWANDevice {
  id: number;
  device_id: string;
  dev_eui: string;
  last_seen: string;
  last_rssi: number | null;
  last_snr: number | null;
  last_f_port: number | null;
  available_fields: Record<string, string>;
  raw_payload?: Record<string, unknown>;
  uplink_count: number;
  existing_tags: string[];
}

export interface ImportField {
  name: string;
  alias: string;
  data_type: 'REAL' | 'INT' | 'BOOL' | 'STRING';
  historize: boolean;
}

export interface ImportDevice {
  device_id: string;
  fields: ImportField[];
}

export interface DownlinkRequest {
  device_id: string;
  dev_eui?: string;
  f_port: number;
  payload_hex: string;
  confirmed: boolean;
}

export const lorawanApi = {
  listDevices: (gatewayId: number) =>
    apiClient.get<LoRaWANDevice[]>(`/gateways/${gatewayId}/lorawan/devices`).then(r => r.data),

  importTags: (gatewayId: number, devices: ImportDevice[]) =>
    apiClient.post<{ created: number; skipped: number; message: string }>(
      `/gateways/${gatewayId}/lorawan/devices/import`,
      { devices },
    ).then(r => r.data),

  sendDownlink: (gatewayId: number, req: DownlinkRequest) =>
    apiClient.post(`/gateways/${gatewayId}/lorawan/downlink`, req).then(r => r.data),
};
