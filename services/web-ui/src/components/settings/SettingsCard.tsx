import type { ReactNode } from 'react';
import { cn } from '@/lib/utils';

/**
 * One block of a settings page: what it is, what it does, the controls, and
 * (when the block saves together) a footer with the save button. Blocks that
 * save as soon as they are changed say so in the footer note instead.
 */
export function SettingsCard({ title, description, children, footer, className }: {
    title: ReactNode;
    description?: ReactNode;
    children: ReactNode;
    footer?: ReactNode;
    className?: string;
}) {
    return (
        <section className={cn('rounded-lg border bg-card', className)}>
            <header className="space-y-1 px-4 pt-4 sm:px-5 sm:pt-5">
                <h3 className="text-sm font-semibold">{title}</h3>
                {description && <p className="text-sm text-muted-foreground">{description}</p>}
            </header>
            <div className="px-4 py-4 sm:px-5">{children}</div>
            {footer && (
                <footer className="flex flex-col-reverse gap-2 border-t bg-muted/30 px-4 py-3 sm:flex-row sm:items-center sm:justify-end sm:px-5">
                    {footer}
                </footer>
            )}
        </section>
    );
}
