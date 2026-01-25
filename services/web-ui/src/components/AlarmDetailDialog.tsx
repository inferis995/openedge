import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Alarm } from '@/types';
import { useTags } from '@/hooks/useTags';
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from '@/components/ui/dialog';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { Label } from '@/components/ui/label';
import { Settings, ExternalLink } from 'lucide-react';

interface AlarmDetailDialogProps {
    alarm: Alarm | null;
    open: boolean;
    onOpenChange: (open: boolean) => void;
    onAcknowledge?: (alarmId: number) => Promise<void>;
}

const AlarmDetailDialog = ({ alarm, open, onOpenChange, onAcknowledge }: AlarmDetailDialogProps) => {
    const navigate = useNavigate();
    const { tags } = useTags(null);
    const [isAcknowledging, setIsAcknowledging] = useState(false);

    // Find the associated tag to get priority info
    const associatedTag = alarm ? tags.find(t => t.id === alarm.tag_id) : null;

    const handleAcknowledge = async () => {
        if (!alarm || !onAcknowledge) return;

        setIsAcknowledging(true);
        try {
            await onAcknowledge(alarm.id);
        } finally {
            setIsAcknowledging(false);
        }
    };

    const getStatusBadge = (state: string) => {
        switch (state) {
            case 'active':
                return <Badge variant="destructive" className="animate-pulse">Active</Badge>;
            case 'rtn':
                return <Badge variant="warning" className="bg-yellow-500 text-white">RTN</Badge>;
            case 'acknowledged':
                return <Badge variant="secondary" className="bg-blue-500 text-white">Acknowledged</Badge>;
            case 'cleared':
                return <Badge variant="outline">Cleared</Badge>;
            default:
                return <Badge variant="outline">{state}</Badge>;
        }
    };

    const getPriorityBadge = (priority?: number) => {
        if (!priority) return null;
        const colors = {
            1: 'bg-gray-500 text-white',
            2: 'bg-blue-500 text-white',
            3: 'bg-yellow-500 text-white',
            4: 'bg-orange-500 text-white',
            5: 'bg-red-500 text-white',
        };
        const color = colors[priority as keyof typeof colors] || 'bg-gray-500 text-white';
        return (
            <Badge variant="outline" className={color}>
                Priority {priority}
            </Badge>
        );
    };

    const formatDateTime = (dateStr?: string) => {
        if (!dateStr) return '-';
        return new Date(dateStr).toLocaleString();
    };

    const handleNavigateToTag = () => {
        if (!alarm) return;
        // Navigate to tags page with this tag selected/filter
        navigate('/tags', { state: { highlightTagId: alarm.tag_id } });
        onOpenChange(false);
    };

    if (!alarm) return null;

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent className="max-w-2xl max-h-[90vh] overflow-y-auto">
                <DialogHeader>
                    <DialogTitle className="flex items-center gap-2">
                        <span>Alarm Details</span>
                        {getStatusBadge(alarm.state)}
                    </DialogTitle>
                    <DialogDescription>
                        Full information about alarm #{alarm.id}
                    </DialogDescription>
                </DialogHeader>

                <div className="space-y-6">
                    {/* Tag Information Section */}
                    <div className="space-y-3">
                        <h3 className="text-lg font-semibold flex items-center gap-2">
                            <Settings size={18} className="text-muted-foreground" />
                            Tag Information
                        </h3>
                        <div className="grid grid-cols-2 gap-4 rounded-lg border p-4 bg-muted/30">
                            <div className="space-y-1">
                                <Label className="text-xs text-muted-foreground">Tag Name</Label>
                                <p className="font-medium">{alarm.tag_alias || `Tag #${alarm.tag_id}`}</p>
                            </div>
                            <div className="space-y-1">
                                <Label className="text-xs text-muted-foreground">Tag Code (PLC Address)</Label>
                                <p className="font-medium font-mono text-sm">{alarm.tag_code || '-'}</p>
                            </div>
                            {associatedTag?.alarm_priority && (
                                <div className="space-y-1">
                                    <Label className="text-xs text-muted-foreground">Alarm Priority</Label>
                                    <div>{getPriorityBadge(associatedTag.alarm_priority)}</div>
                                </div>
                            )}
                            {associatedTag?.alarm_threshold !== undefined && associatedTag?.alarm_operator && (
                                <div className="space-y-1">
                                    <Label className="text-xs text-muted-foreground">Alarm Threshold</Label>
                                    <p className="font-medium font-mono text-sm">
                                        {associatedTag.alarm_operator} {associatedTag.alarm_threshold}
                                    </p>
                                </div>
                            )}
                        </div>
                        <Button
                            variant="outline"
                            size="sm"
                            className="w-full"
                            onClick={handleNavigateToTag}
                        >
                            <ExternalLink size={14} className="mr-2" />
                            View Tag Configuration
                        </Button>
                    </div>

                    {/* Alarm Details Section */}
                    <div className="space-y-3">
                        <h3 className="text-lg font-semibold">Alarm Details</h3>
                        <div className="space-y-3 rounded-lg border p-4 bg-muted/30">
                            <div className="space-y-1">
                                <Label className="text-xs text-muted-foreground">Alarm ID</Label>
                                <p className="font-medium">#{alarm.id}</p>
                            </div>
                            <div className="space-y-1">
                                <Label className="text-xs text-muted-foreground">State</Label>
                                <div>{getStatusBadge(alarm.state)}</div>
                            </div>
                            <div className="space-y-1">
                                <Label className="text-xs text-muted-foreground">Message</Label>
                                <p className="font-medium">{alarm.message || 'No message'}</p>
                            </div>
                            {alarm.value_at_trigger !== undefined && (
                                <div className="space-y-1">
                                    <Label className="text-xs text-muted-foreground">Value at Trigger</Label>
                                    <p className="font-mono font-medium">{alarm.value_at_trigger}</p>
                                </div>
                            )}
                        </div>
                    </div>

                    {/* Timestamps Section */}
                    <div className="space-y-3">
                        <h3 className="text-lg font-semibold">Timestamps</h3>
                        <div className="space-y-3 rounded-lg border p-4 bg-muted/30">
                            <div className="space-y-1">
                                <Label className="text-xs text-muted-foreground">Triggered At</Label>
                                <p className="text-sm">{formatDateTime(alarm.triggered_at)}</p>
                            </div>
                            <div className="space-y-1">
                                <Label className="text-xs text-muted-foreground">Acknowledged At</Label>
                                <p className="text-sm">{formatDateTime(alarm.acknowledged_at)}</p>
                            </div>
                            <div className="space-y-1">
                                <Label className="text-xs text-muted-foreground">Cleared At</Label>
                                <p className="text-sm">{formatDateTime(alarm.cleared_at)}</p>
                            </div>
                        </div>
                    </div>

                    {/* State History Note */}
                    <div className="rounded-lg border border-blue-200 bg-blue-50 p-4">
                        <p className="text-sm text-blue-900">
                            <strong>Note:</strong> This dialog shows the current alarm state. To view the full history
                            of state changes (ACTIVE → RTN → ACKNOWLEDGED), navigate to the tag configuration page.
                        </p>
                    </div>
                </div>

                <DialogFooter className="flex-col sm:flex-row gap-2">
                    {alarm.state === 'active' && onAcknowledge && (
                        <Button
                            onClick={handleAcknowledge}
                            disabled={isAcknowledging}
                            className="w-full sm:w-auto"
                        >
                            {isAcknowledging ? 'Acknowledging...' : 'Acknowledge Alarm'}
                        </Button>
                    )}
                    <Button
                        variant="outline"
                        onClick={() => onOpenChange(false)}
                        className="w-full sm:w-auto"
                    >
                        Close
                    </Button>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    );
};

export default AlarmDetailDialog;
