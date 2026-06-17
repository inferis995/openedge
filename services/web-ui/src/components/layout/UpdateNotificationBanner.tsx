import { useEffect, useState } from 'react';
import { updatesApi, OrgUpdateStatus } from '@/api/updates';
import { useAuthStore } from '@/stores/useAuthStore';
import { useNavigationStore } from '@/stores/useNavigationStore';
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { X, PackageOpen, RefreshCw, AlertCircle, RotateCcw } from 'lucide-react';
import { toast } from 'sonner';

const UpdateNotificationBanner = () => {
    const { isAdmin } = useAuthStore();
    const { selectedOrgId } = useNavigationStore();
    const [status, setStatus] = useState<OrgUpdateStatus | null>(null);
    const [dialogOpen, setDialogOpen] = useState(false);
    const [approving, setApproving] = useState(false);
    const [dismissed, setDismissed] = useState(false);

    useEffect(() => {
        if (!isAdmin() || !selectedOrgId) return;
        const fetchUpdate = async () => {
            try {
                const s = await updatesApi.getOrgUpdate(selectedOrgId);
                setStatus(s);
            } catch {
                // silently ignore — banner is best-effort
            }
        };
        fetchUpdate();
        const interval = setInterval(fetchUpdate, 5 * 60 * 1000); // re-poll every 5 min
        return () => clearInterval(interval);
    }, [isAdmin, selectedOrgId]);

    if (!status?.available || dismissed) return null;
    if (status.status === 'success') return null;

    const handleApprove = async () => {
        if (!selectedOrgId || !status.release_id) return;
        setApproving(true);
        try {
            await updatesApi.approveUpdate(selectedOrgId, status.release_id);
            toast.success(`Update ${status.version} approved — will be applied on next edge poll`);
            setDialogOpen(false);
            setStatus(s => s ? { ...s, approved: true, status: 'approved' } : s);
        } catch {
            toast.error('Failed to approve update');
        } finally {
            setApproving(false);
        }
    };

    let bannerClass = '';
    let icon: React.ReactNode = null;
    let message = '';
    let actionLabel = '';
    let showAction = false;

    switch (status.status) {
        case 'pending':
            bannerClass = 'bg-amber-500/10 border-amber-500/30 text-amber-700 dark:text-amber-300';
            icon = <PackageOpen className="h-4 w-4 shrink-0" />;
            message = `Update ${status.version} available`;
            actionLabel = 'Review & Approve';
            showAction = true;
            break;
        case 'approved':
        case 'updating':
            bannerClass = 'bg-blue-500/10 border-blue-500/30 text-blue-700 dark:text-blue-300';
            icon = <RefreshCw className="h-4 w-4 shrink-0 animate-spin" />;
            message = `Update ${status.version} in progress...`;
            showAction = false;
            break;
        case 'failed':
            bannerClass = 'bg-red-500/10 border-red-500/30 text-red-700 dark:text-red-300';
            icon = <AlertCircle className="h-4 w-4 shrink-0" />;
            message = `Update ${status.version} failed`;
            actionLabel = 'View details';
            showAction = true;
            break;
        case 'rolled_back':
            bannerClass = 'bg-orange-500/10 border-orange-500/30 text-orange-700 dark:text-orange-300';
            icon = <RotateCcw className="h-4 w-4 shrink-0" />;
            message = `Update ${status.version} rolled back`;
            actionLabel = 'View details';
            showAction = true;
            break;
        default:
            return null;
    }

    return (
        <>
            <div className={`flex items-center gap-3 px-4 py-2 border-b text-sm font-medium ${bannerClass}`}>
                {icon}
                <span className="flex-1">{message}</span>
                {showAction && (
                    <button
                        onClick={() => setDialogOpen(true)}
                        className="underline underline-offset-2 hover:opacity-80 text-xs font-semibold"
                    >
                        {actionLabel}
                    </button>
                )}
                <button
                    onClick={() => setDismissed(true)}
                    className="hover:opacity-60 transition-opacity ml-1"
                    aria-label="Dismiss"
                >
                    <X className="h-4 w-4" />
                </button>
            </div>

            <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
                <DialogContent className="max-w-lg">
                    <DialogHeader>
                        <DialogTitle className="flex items-center gap-2">
                            <PackageOpen className="h-5 w-5 text-primary" />
                            Edge Update {status.version}
                        </DialogTitle>
                    </DialogHeader>
                    <div className="space-y-4 py-2">
                        <div className="flex items-center gap-2">
                            <Badge variant="outline">Version</Badge>
                            <span className="font-mono font-semibold">{status.version}</span>
                        </div>
                        {status.status === 'failed' && status.error_msg && (
                            <div className="p-3 rounded bg-red-500/10 border border-red-500/30">
                                <p className="text-sm font-semibold text-red-600 mb-1">Error</p>
                                <p className="text-xs text-red-500 font-mono">{status.error_msg}</p>
                            </div>
                        )}
                        {status.status === 'rolled_back' && (
                            <div className="p-3 rounded bg-orange-500/10 border border-orange-500/30">
                                <p className="text-sm font-semibold text-orange-600">
                                    The update was automatically rolled back after a failed health check.
                                </p>
                                {status.error_msg && <p className="text-xs text-orange-500 font-mono mt-1">{status.error_msg}</p>}
                            </div>
                        )}
                        {status.release_notes && (
                            <div>
                                <p className="text-sm font-semibold mb-1">Release Notes</p>
                                <div className="p-3 rounded bg-muted text-sm whitespace-pre-wrap">{status.release_notes}</div>
                            </div>
                        )}
                        {status.sha256 && (
                            <div>
                                <p className="text-xs text-muted-foreground font-semibold mb-1">SHA256 Checksum</p>
                                <p className="text-xs font-mono bg-muted p-2 rounded break-all">{status.sha256}</p>
                            </div>
                        )}
                        {status.status === 'pending' && (
                            <div className="flex justify-end gap-2 pt-2 border-t">
                                <Button variant="outline" onClick={() => setDialogOpen(false)}>Cancel</Button>
                                <Button onClick={handleApprove} disabled={approving}>
                                    {approving ? <RefreshCw className="h-4 w-4 mr-2 animate-spin" /> : <PackageOpen className="h-4 w-4 mr-2" />}
                                    Approve Update
                                </Button>
                            </div>
                        )}
                    </div>
                </DialogContent>
            </Dialog>
        </>
    );
};

export default UpdateNotificationBanner;
