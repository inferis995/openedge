import axios, { AxiosError, InternalAxiosRequestConfig } from 'axios';
import { useNavigationStore } from '@/stores/useNavigationStore';
import { useAuthStore } from '@/stores/useAuthStore';

// Extend InternalAxiosRequestConfig to include metadata
interface CustomAxiosRequestConfig extends InternalAxiosRequestConfig {
    metadata?: {
        startTime: number;
    };
}

// Create API client with base URL
const api = axios.create({
    baseURL: '/api', // Proxy in nginx will redirect to backend
    headers: {
        'Content-Type': 'application/json',
    },
    timeout: 10000,
});

// Request interceptor to add X-Organization-ID header
api.interceptors.request.use(
    (config: CustomAxiosRequestConfig) => {
        const { selectedOrgId } = useNavigationStore.getState();
        const { token } = useAuthStore.getState();

        if (token) {
            config.headers['Authorization'] = `Bearer ${token}`;
        }

        if (selectedOrgId && !config.headers['X-Organization-ID']) {
            config.headers['X-Organization-ID'] = selectedOrgId.toString();
        }
        // Add request metadata for loading states
        config.metadata = { startTime: Date.now() };
        return config;
    },
    (error) => {
        console.error('Request error:', error);
        return Promise.reject(error);
    }
);

// Response interceptor for generic error handling
api.interceptors.response.use(
    (response) => {
        // Log slow requests
        const config = response.config as CustomAxiosRequestConfig;
        const duration = Date.now() - (config.metadata?.startTime || 0);
        if (duration > 3000) {
            console.warn(`Slow API request: ${response.config.url} took ${duration}ms`);
        }
        return response;
    },
    (error: AxiosError) => {
        // Enhanced error logging with context
        const requestInfo = {
            url: error.config?.url,
            method: error.config?.method?.toUpperCase(),
            status: error.response?.status,
            statusText: error.response?.statusText,
        };

        // Log detailed error information
        console.error('API Error:', {
            ...requestInfo,
            message: error.response?.data || error.message,
            code: error.code,
        });

        // Handle specific error cases
        if (error.response?.status === 403) {
            console.error('403 Forbidden - Organization access denied. Check X-Organization-ID header.');
        } else if (error.response?.status === 401) {
            console.error('401 Unauthorized - Authentication required');
            // Auto logout
            useAuthStore.getState().logout();
            // Redirect to login is handled by React Router (RequireAuth)
        } else if (error.code === 'ECONNABORTED') {
            console.error('Request timeout - Server took too long to respond');
        } else if (error.code === 'ERR_NETWORK') {
            console.error('Network error - Unable to reach server');
        }

        return Promise.reject(error);
    }
);

export default api;
