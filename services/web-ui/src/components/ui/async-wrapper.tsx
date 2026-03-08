import { ReactNode } from 'react';
import { Loader2 } from 'lucide-react';
import { Skeleton } from './skeleton';
import { Button } from './button';
import { Alert, AlertDescription, AlertTitle } from './alert';
import { AlertCircle } from 'lucide-react';

interface AsyncWrapperProps {
    children: ReactNode;
    loading?: boolean;
    error?: string | null;
    onRetry?: () => void;
    loadingType?: 'spinner' | 'skeleton';
    skeletonCount?: number;
    empty?: boolean;
    emptyMessage?: string;
    emptyIcon?: ReactNode;
}

/**
 * Wrapper component for handling loading, error, and empty states
 */
export function AsyncWrapper({
    children,
    loading = false,
    error = null,
    onRetry,
    loadingType = 'spinner',
    skeletonCount = 3,
    empty = false,
    emptyMessage = 'No data found',
    emptyIcon,
}: AsyncWrapperProps) {
    // Error state
    if (error) {
        return (
            <Alert variant="destructive" className="my-4">
                <AlertCircle className="h-4 w-4" />
                <AlertTitle>Error</AlertTitle>
                <AlertDescription className="mt-2">
                    <p>{error}</p>
                    {onRetry && (
                        <Button
                            variant="outline"
                            size="sm"
                            className="mt-3"
                            onClick={onRetry}
                        >
                            Retry
                        </Button>
                    )}
                </AlertDescription>
            </Alert>
        );
    }

    // Loading state with spinner
    if (loading && loadingType === 'spinner') {
        return (
            <div className="flex items-center justify-center py-12">
                <div className="text-center">
                    <Loader2 className="h-8 w-8 animate-spin text-primary mx-auto mb-3" />
                    <p className="text-sm text-muted-foreground">Loading...</p>
                </div>
            </div>
        );
    }

    // Loading state with skeleton
    if (loading && loadingType === 'skeleton') {
        return (
            <div className="space-y-3 py-4">
                {Array.from({ length: skeletonCount }).map((_, i) => (
                    <Skeleton key={i} className="h-16 w-full" />
                ))}
            </div>
        );
    }

    // Empty state
    if (empty) {
        return (
            <div className="flex flex-col items-center justify-center py-12 text-center">
                {emptyIcon || (
                    <div className="w-16 h-16 rounded-full bg-muted flex items-center justify-center mb-4">
                        <AlertCircle className="h-8 w-8 text-muted-foreground" />
                    </div>
                )}
                <p className="text-muted-foreground">{emptyMessage}</p>
            </div>
        );
    }

    // Normal state
    return <>{children}</>;
}

/**
 * Table skeleton loader
 */
export function TableSkeleton({ rows = 5, columns = 4 }: { rows?: number; columns?: number }) {
    return (
        <div className="space-y-3 py-4">
            <div className="flex gap-4 px-4">
                {Array.from({ length: columns }).map((_, i) => (
                    <Skeleton key={i} className="h-4 flex-1" />
                ))}
            </div>
            {Array.from({ length: rows }).map((_, i) => (
                <div key={i} className="flex gap-4 px-4 py-3 border-b">
                    {Array.from({ length: columns }).map((_, j) => (
                        <Skeleton key={j} className="h-4 flex-1" />
                    ))}
                </div>
            ))}
        </div>
    );
}

/**
 * Card skeleton loader
 */
export function CardSkeleton({ count = 3 }: { count?: number }) {
    return (
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
            {Array.from({ length: count }).map((_, i) => (
                <div key={i} className="border rounded-lg p-4 space-y-3">
                    <Skeleton className="h-5 w-3/4" />
                    <Skeleton className="h-4 w-1/2" />
                    <Skeleton className="h-16 w-full" />
                </div>
            ))}
        </div>
    );
}

/**
 * Inline loading spinner for buttons
 */
export function ButtonLoader({ size = 'sm' }: { size?: 'sm' | 'md' | 'lg' }) {
    const sizeClasses = {
        sm: 'h-4 w-4',
        md: 'h-5 w-5',
        lg: 'h-6 w-6',
    };

    return (
        <Loader2 className={`animate-spin ${sizeClasses[size]}`} />
    );
}
