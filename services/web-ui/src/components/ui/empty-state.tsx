import type { LucideIcon } from 'lucide-react';
import type { ReactNode } from 'react';
import { cn } from '@/lib/utils';

interface EmptyStateProps {
    icon: LucideIcon;
    title: string;
    /** What this list is for and how it gets filled — the one thing a first-time user needs. */
    description?: string;
    /** The next step, usually a button. */
    action?: ReactNode;
    className?: string;
}

/**
 * What a list shows when it has nothing in it.
 *
 * "No sites found." told a new user that something was missing but not what a
 * site is or how to make one. This says both, and puts the button next to it.
 */
export function EmptyState({ icon: Icon, title, description, action, className }: EmptyStateProps) {
    return (
        <div className={cn('flex flex-col items-center justify-center text-center gap-3 py-10 px-4', className)}>
            <div className="h-12 w-12 rounded-full bg-muted flex items-center justify-center text-muted-foreground">
                <Icon size={24} />
            </div>
            <div className="space-y-1 max-w-md">
                <p className="font-semibold text-foreground">{title}</p>
                {description && <p className="text-sm text-muted-foreground">{description}</p>}
            </div>
            {action}
        </div>
    );
}
