import axios from 'axios';
import { useNavigationStore } from '@/stores/useNavigationStore';

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
    (config) => {
        const { selectedOrgId } = useNavigationStore.getState();
        if (selectedOrgId) {
            config.headers['X-Organization-ID'] = selectedOrgId.toString();
        }
        return config;
    },
    (error) => Promise.reject(error)
);

// Response interceptor for generic error handling
api.interceptors.response.use(
    (response) => response,
    (error) => {
        // Basic error logging, can be extended with toast notifications
        console.error('API Error:', error.response?.data || error.message);
        return Promise.reject(error);
    }
);

export default api;
