import { toast } from 'sonner';
import axios, { AxiosError } from 'axios';
import i18n from '@/i18n';

export interface ApiError {
    message: string;
    status?: number;
    code?: string;
}

/**
 * Format API error for display.
 *
 * The server's own message wins whenever there is one: "the site still has
 * areas" tells the user what to do, "Server error - please try again" does not.
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
            return { message: i18n.t('errors.network'), code: 'NETWORK_ERROR' };
        }
    }

    if (error instanceof Error) {
        return { message: error.message };
    }

    return { message: i18n.t('errors.unexpected') };
}

/**
 * Get default error message by HTTP status code
 */
function getDefaultErrorMessage(status: number): string {
    const known = [400, 401, 403, 404, 409, 422, 429, 500, 502, 503, 504];
    if (known.includes(status)) return i18n.t(`errors.status_${status}`);
    return i18n.t('errors.status_other', { status });
}

/**
 * Show toast notification for API error.
 *
 * It used to replace the server's explanation with a fixed "Server Error /
 * Please try again later" for every 5xx and a fixed "Not Found" for every 404,
 * in English, so the one sentence that said what went wrong never reached the
 * user.
 */
export function showApiError(error: unknown, context?: string): void {
    const { message, code } = formatApiError(error);
    toast.error(context || i18n.t('errors.title'), {
        description: message,
        duration: code === 'NETWORK_ERROR' ? 6000 : 5000,
    });
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
