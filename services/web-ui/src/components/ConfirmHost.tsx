import { useTranslation } from 'react-i18next';
import { ConfirmDialog } from '@/components/ui/confirm-dialog';
import { useConfirmStore } from '@/lib/confirm';

/** Draws the question asked through confirmAction(). Mounted once, in the layout. */
export default function ConfirmHost() {
    const { t } = useTranslation();
    const request = useConfirmStore((s) => s.request);
    const settle = useConfirmStore((s) => s.settle);

    return (
        <ConfirmDialog
            open={request !== null}
            title={request?.title ?? ''}
            description={request?.description}
            confirmLabel={request?.confirmLabel ?? (request?.destructive ? t('common.delete') : t('common.confirm'))}
            cancelLabel={request?.cancelLabel ?? t('common.cancel')}
            destructive={request?.destructive}
            onConfirm={() => settle(true)}
            onCancel={() => settle(false)}
        />
    );
}
