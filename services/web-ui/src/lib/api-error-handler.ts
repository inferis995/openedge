import { toast } from 'sonner';
import axios, { AxiosError } from 'axios';

export interface ApiError {
    message: string;
    status?: number;
    code?: string;
}

/**
 * Format API error for display
 */
export function formatApiError(error: unknown): ApiError {
    if (axios.isAxiosError(error)) {
        const axiosError = error as AxiosError<{ message?: string; error?: string }>;

        if (axiosError.response) {
            const status = axiosError.response.status;
            const data = axiosError.response.data;
            const message = data?.message || data?.error || getDefaultErrorMessage(status);

            return { message, status };
        }

        if (axiosError.request) {
            return { message: 'Network error - please check your connection', code: 'NETWORK_ERROR' };
        }
    }

    if (error instanceof Error) {
        return { message: error.message };
    }

    return { message: 'An unexpected error occurred' };
}

/**
 * Get default error message by HTTP status code
 */
function getDefaultErrorMessage(status: number): string {
    switch (status) {
        case 400:
            return 'Invalid request - please check your input';
        case 401:
            return 'Authentication required';
        case 403:
            return 'Access denied - check your organization selection';
        case 404:
            return 'Resource not found';
        case 409:
            return 'Conflict - this resource may already exist';
        case 422:
            return 'Validation error - please check your input';
        case 429:
            return 'Too many requests - please try again later';
        case 500:
            return 'Server error - please try again';
        case 502:
        case 503:
            return 'Service temporarily unavailable';
        case 504:
            return 'Request timeout - please try again';
        default:
            return `Request failed with status ${status}`;
    }
}

/**
 * Show toast notification for API error
 */
export function showApiError(error: unknown, context?: string): void {
    const { message, status } = formatApiError(error);

    const contextPrefix = context ? `${context}: ` : '';

    // Check for specific error types to show appropriate toast style
    if (status === 403) {
        toast.error(`${contextPrefix}Access Denied`, {
            description: 'Check your organization selection',
            duration: 5000,
        });
    } else if (status === 404) {
        toast.error(`${contextPrefix}Not Found`, {
            description: 'The requested resource does not exist',
            duration: 4000,
        });
    } else if (status && status >= 500) {
        toast.error(`${contextPrefix}Server Error`, {
            description: 'Please try again later',
            duration: 5000,
        });
    } else if ((error as ApiError)?.code === 'NETWORK_ERROR') {
        toast.error(`${contextPrefix}Network Error`, {
            description: 'Check your internet connection',
            duration: 6000,
        });
    } else {
        toast.error(`${contextPrefix}Error`, {
            description: message,
            duration: 4000,
        });
    }
}

/**
 * Show toast notification for API success
 */
export function showApiSuccess(message: string, description?: string): void {
    toast.success(message, {
        description,
        duration: 3000,
    });
}

/**
 * Show toast notification for API info
 */
export function showApiInfo(message: string, description?: string): void {
    toast.info(message, {
        description,
        duration: 4000,
    });
}

/**
 * Handle API error in try/catch with optional callback
 */
export async function handleApiCall<T>(
    apiCall: () => Promise<T>,
    options: {
        context?: string;
        onSuccess?: (data: T) => void;
        onError?: (error: ApiError) => void;
        showErrorToast?: boolean;
    } = {}
): Promise<T | null> {
    const {
        context,
        onSuccess,
        onError,
        showErrorToast = true,
    } = options;

    try {
        const result = await apiCall();
        onSuccess?.(result);
        return result;
    } catch (error) {
        const apiError = formatApiError(error);

        if (showErrorToast) {
            showApiError(error, context);
        }

        onError?.(apiError);
        return null;
    }
}
