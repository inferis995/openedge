// API Client Service with axios
// Design System: See tasks/design-system.md

import axios, { AxiosError, AxiosInstance, InternalAxiosRequestConfig, AxiosResponse } from 'axios';
import type {
  Organization,
  Site,
  Area,
  Gateway,
  GatewayWithHealth,
  Tag,
  TagValue,
  HistoryPoint,
  Alarm,
  CreateOrganizationRequest,
  CreateSiteRequest,
  CreateAreaRequest,
  CreateGatewayRequest,
  UpdateGatewayRequest,
  CreateTagRequest,
  UpdateTagRequest,
  HistoryQueryParams,
  AcknowledgeResponse,
} from '@/types/api';

// Create axios instance with base configuration
const apiClient: AxiosInstance = axios.create({
  baseURL: '/api', // Uses Vite proxy to forward to localhost:8080 in development
  timeout: 10000, // 10 second timeout
  headers: {
    'Content-Type': 'application/json',
  },
});

// Request interceptor: Add X-Organization-ID header from localStorage
apiClient.interceptors.request.use(
  (config: InternalAxiosRequestConfig) => {
    const orgId = localStorage.getItem('ralph_org_id');
    if (orgId && config.headers) {
      config.headers['X-Organization-ID'] = orgId;
    }
    return config;
  },
  (error) => {
    return Promise.reject(error);
  }
);

// Response interceptor: Handle common errors
apiClient.interceptors.response.use(
  (response: AxiosResponse) => response,
  (error: AxiosError) => {
    // Handle 403 Forbidden - Organization access denied
    if (error.response?.status === 403) {
      console.error('Access denied - Check your organization selection');
    }
    // Handle 404 Not Found - Resource not found
    if (error.response?.status === 404) {
      console.error('Resource not found');
    }
    // Handle 500 Internal Server Error
    if (error.response?.status === 500) {
      console.error('Server error, please try again');
    }
    // Handle network errors
    if (!error.response) {
      console.error('Network error - Check if backend is running');
    }
    return Promise.reject(error);
  }
);

// ==================== Organizations ====================

export async function getOrganizations(): Promise<Organization[]> {
  const response = await apiClient.get<Organization[]>('/organizations');
  return response.data;
}

export async function createOrganization(data: CreateOrganizationRequest): Promise<Organization> {
  const response = await apiClient.post<Organization>('/organizations', data);
  return response.data;
}

// ==================== Sites ====================

export async function getSites(orgId?: number): Promise<Site[]> {
  const params = orgId ? { org_id: orgId } : {};
  const response = await apiClient.get<Site[]>('/sites', { params });
  return response.data;
}

export async function createSite(data: CreateSiteRequest): Promise<Site> {
  const response = await apiClient.post<Site>('/sites', data);
  return response.data;
}

// ==================== Areas ====================

export async function getAreas(siteId: number): Promise<Area[]> {
  const response = await apiClient.get<Area[]>('/areas', {
    params: { site_id: siteId },
  });
  return response.data;
}

export async function createArea(data: CreateAreaRequest): Promise<Area> {
  const response = await apiClient.post<Area>('/areas', data);
  return response.data;
}

// ==================== Gateways ====================

export async function getGateways(areaId: number): Promise<GatewayWithHealth[]> {
  const response = await apiClient.get<GatewayWithHealth[]>('/gateways', {
    params: { area_id: areaId },
  });
  return response.data;
}

export async function getGateway(id: number): Promise<GatewayWithHealth> {
  const response = await apiClient.get<GatewayWithHealth>(`/gateways/${id}`);
  return response.data;
}

export async function createGateway(data: CreateGatewayRequest): Promise<GatewayWithHealth> {
  const response = await apiClient.post<GatewayWithHealth>('/gateways', data);
  return response.data;
}

export async function updateGateway(id: number, data: UpdateGatewayRequest): Promise<GatewayWithHealth> {
  const response = await apiClient.put<GatewayWithHealth>(`/gateways/${id}`, data);
  return response.data;
}

// ==================== Tags ====================

export async function getTags(gatewayId: number): Promise<Tag[]> {
  const response = await apiClient.get<Tag[]>('/tags', {
    params: { gateway_id: gatewayId },
  });
  return response.data;
}

export async function createTag(data: CreateTagRequest): Promise<Tag> {
  const response = await apiClient.post<Tag>('/tags', data);
  return response.data;
}

export async function updateTag(id: number, data: UpdateTagRequest): Promise<Tag> {
  const response = await apiClient.put<Tag>(`/tags/${id}`, data);
  return response.data;
}

export async function getCurrentTagValue(id: number): Promise<TagValue> {
  const response = await apiClient.get<TagValue>(`/tags/${id}/current`);
  return response.data;
}

// ==================== History ====================

export async function getHistory(params: HistoryQueryParams): Promise<HistoryPoint[]> {
  const response = await apiClient.get<HistoryPoint[]>('/history', { params });
  return response.data;
}

// ==================== Alarms ====================

export async function getAlarms(filters?: {
  tag_id?: number;
  state?: string;
  limit?: number;
  offset?: number;
}): Promise<Alarm[]> {
  const response = await apiClient.get<Alarm[]>('/alarms', { params: filters });
  return response.data;
}

export async function acknowledgeAlarm(id: number): Promise<AcknowledgeResponse> {
  const response = await apiClient.post<AcknowledgeResponse>(`/alarms/${id}/acknowledge`);
  return response.data;
}

// Export the api client instance for custom requests
export default apiClient;
