import * as React from 'react';
import { cn } from '@/lib/utils';

export interface CheckboxProps extends React.InputHTMLAttributes<HTMLInputElement> {
    onCheckedChange?: (checked: boolean) => void;
}

const Checkbox = React.forwardRef<HTMLInputElement, CheckboxProps>(
    ({ className, onCheckedChange, onChange, ...props }, ref) => {
        const handleChange = (e: React.ChangeEvent<HTMLInputElement>) => {
            onChange?.(e);
            onCheckedChange?.(e.target.checked);
        };
        return (
            <input
                type="checkbox"
                ref={ref}
                onChange={handleChange}
                className={cn(
                    'h-4 w-4 rounded border border-input accent-primary cursor-pointer',
                    className
                )}
                {...props}
            />
        );
    }
);
Checkbox.displayName = 'Checkbox';

export { Checkbox };
