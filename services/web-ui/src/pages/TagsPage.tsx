import { useState, useMemo } from 'react';
import { useTags } from '@/hooks/useTags';
import { useGateways } from '@/hooks/useGateways';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Switch } from '@/components/ui/switch';
import {
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
} from '@/components/ui/table';
import {
    Dialog,
    DialogContent,
    DialogHeader,
    DialogTitle,
    DialogFooter,
    DialogTrigger,
} from '@/components/ui/dialog';
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from '@/components/ui/select';
import { Plus, Trash2, Edit2, Bell, Database } from 'lucide-react';
import { CreateTagDto } from '@/types';
import { useSearchParams } from 'react-router-dom';
import { Badge } from '@/components/ui/badge';

const TagsPage = () => {
    const [searchParams] = useSearchParams();
    const gatewayIdParam = searchParams.get('gateway_id');
    const [selectedGatewayId, setSelectedGatewayId] = useState<string>(gatewayIdParam || '');

    const { gateways } = useGateways(); // Get all gateways for filter
    const { tags, isLoading, create, remove } = useTags(selectedGatewayId ? parseInt(selectedGatewayId) : undefined);

    const [isOpen, setIsOpen] = useState(false);
    const [formData, setFormData] = useState<Partial<CreateTagDto>>({
        code: '',
        alias: '',
        data_type: 'REAL',
        historize: false,
        deadband_value: 0.1,
        alarm_enabled: false,
        alarm_threshold: 0,
        alarm_operator: '>',
        alarm_priority: 3,
    });

    const handleInputChange = (field: keyof CreateTagDto, value: any) => {
        setFormData(prev => ({ ...prev, [field]: value }));
    };

    const handleCreate = async () => {
        if (!formData.code || !selectedGatewayId) return;
        try {
            await create({
                ...formData,
                gateway_id: parseInt(selectedGatewayId),
            } as CreateTagDto);
            setIsOpen(false);
            // Reset form (keep gateway selection)
            setFormData({
                code: '',
                alias: '',
                data_type: 'REAL',
                historize: false,
                deadband_value: 0.1,
                alarm_enabled: false,
                alarm_threshold: 0,
                alarm_operator: '>',
                alarm_priority: 3,
            });
        } catch (error) {
            console.error('Failed to create tag', error);
        }
    };

    const handleDelete = async (e: React.MouseEvent, id: number) => {
        e.stopPropagation();
        if (confirm('Are you sure you want to delete this tag?')) {
            await remove(id);
        }
    };

    const tagsList = useMemo(() => tags, [tags]);

    if (isLoading) {
        return <div className="p-8 text-center text-slate-500">Loading tags...</div>;
    }

    return (
        <div className="space-y-6">
            <div className="flex items-center justify-between">
                <div>
                    <h2 className="text-2xl font-bold tracking-tight">Tags</h2>
                    <p className="text-muted-foreground">
                        Manage data points, history, and alarm configurations.
                    </p>
                </div>

                <div className="flex items-center gap-4">
                    <div className="w-[200px]">
                        <Select
                            value={selectedGatewayId}
                            onValueChange={setSelectedGatewayId}
                        >
                            <SelectTrigger>
                                <SelectValue placeholder="Filter by Gateway" />
                            </SelectTrigger>
                            <SelectContent>
                                <SelectItem value="all">All Gateways</SelectItem>
                                {gateways.map((gw) => (
                                    <SelectItem key={gw.id} value={gw.id.toString()}>
                                        {gw.name}
                                    </SelectItem>
                                ))}
                            </SelectContent>
                        </Select>
                    </div>

                    <Dialog open={isOpen} onOpenChange={setIsOpen}>
                        <DialogTrigger asChild disabled={!selectedGatewayId || selectedGatewayId === 'all'}>
                            <Button className="gap-2">
                                <Plus size={16} /> Add Tag
                            </Button>
                        </DialogTrigger>
                        <DialogContent className="max-w-2xl">
                            <DialogHeader>
                                <DialogTitle>Create Tag</DialogTitle>
                            </DialogHeader>
                            <div className="grid gap-6 py-4">
                                <div className="grid grid-cols-2 gap-4">
                                    <div className="grid gap-2">
                                        <Label htmlFor="code">PLC Address (Code)</Label>
                                        <Input
                                            id="code"
                                            value={formData.code}
                                            onChange={(e) => handleInputChange('code', e.target.value)}
                                            placeholder="e.g. %MW100 or DB1.DBW0"
                                        />
                                    </div>
                                    <div className="grid gap-2">
                                        <Label htmlFor="alias">Alias (Name)</Label>
                                        <Input
                                            id="alias"
                                            value={formData.alias}
                                            onChange={(e) => handleInputChange('alias', e.target.value)}
                                            placeholder="e.g. Oven_Temp"
                                        />
                                    </div>
                                </div>

                                <div className="grid gap-2">
                                    <Label htmlFor="type">Data Type</Label>
                                    <Select
                                        value={formData.data_type}
                                        onValueChange={(val) => handleInputChange('data_type', val)}
                                    >
                                        <SelectTrigger>
                                            <SelectValue placeholder="Select Data Type" />
                                        </SelectTrigger>
                                        <SelectContent>
                                            <SelectItem value="BOOL">BOOL</SelectItem>
                                            <SelectItem value="INT">INT</SelectItem>
                                            <SelectItem value="REAL">REAL</SelectItem>
                                            <SelectItem value="DINT">DINT</SelectItem>
                                            <SelectItem value="STRING">STRING</SelectItem>
                                        </SelectContent>
                                    </Select>
                                </div>

                                <div className="grid grid-cols-2 gap-6 border-t pt-4">
                                    {/* History Config */}
                                    <div className="space-y-4">
                                        <div className="flex items-center space-x-2">
                                            <Switch
                                                id="historize"
                                                checked={formData.historize}
                                                onCheckedChange={(checked) => handleInputChange('historize', checked)}
                                            />
                                            <Label htmlFor="historize" className="flex items-center gap-2">
                                                <Database size={14} /> Historize
                                            </Label>
                                        </div>

                                        {formData.historize && (
                                            <div className="grid gap-2 pl-6 border-l-2">
                                                <Label htmlFor="deadband">Deadband Value</Label>
                                                <Input
                                                    id="deadband"
                                                    type="number"
                                                    step="0.01"
                                                    value={formData.deadband_value}
                                                    onChange={(e) => handleInputChange('deadband_value', parseFloat(e.target.value))}
                                                />
                                            </div>
                                        )}
                                    </div>

                                    {/* Alarm Config */}
                                    <div className="space-y-4">
                                        <div className="flex items-center space-x-2">
                                            <Switch
                                                id="alarm"
                                                checked={formData.alarm_enabled}
                                                onCheckedChange={(checked) => handleInputChange('alarm_enabled', checked)}
                                            />
                                            <Label htmlFor="alarm" className="flex items-center gap-2">
                                                <Bell size={14} /> Alarm Enabled
                                            </Label>
                                        </div>

                                        {formData.alarm_enabled && (
                                            <div className="space-y-3 pl-6 border-l-2">
                                                <div className="grid grid-cols-2 gap-2">
                                                    <div>
                                                        <Label className="text-xs">Operator</Label>
                                                        <Select
                                                            value={formData.alarm_operator}
                                                            onValueChange={(val) => handleInputChange('alarm_operator', val)}
                                                        >
                                                            <SelectTrigger className="h-8">
                                                                <SelectValue />
                                                            </SelectTrigger>
                                                            <SelectContent>
                                                                <SelectItem value=">">{'>'}</SelectItem>
                                                                <SelectItem value="<">{'<'}</SelectItem>
                                                                <SelectItem value="=">{'='}</SelectItem>
                                                                <SelectItem value="!=">{'!='}</SelectItem>
                                                            </SelectContent>
                                                        </Select>
                                                    </div>
                                                    <div>
                                                        <Label className="text-xs">Threshold</Label>
                                                        <Input
                                                            type="number"
                                                            className="h-8"
                                                            value={formData.alarm_threshold}
                                                            onChange={(e) => handleInputChange('alarm_threshold', parseFloat(e.target.value))}
                                                        />
                                                    </div>
                                                </div>
                                                <div>
                                                    <Label className="text-xs">Priority (1-5)</Label>
                                                    <Input
                                                        type="number"
                                                        min="1"
                                                        max="5"
                                                        className="h-8"
                                                        value={formData.alarm_priority}
                                                        onChange={(e) => handleInputChange('alarm_priority', parseInt(e.target.value))}
                                                    />
                                                </div>
                                            </div>
                                        )}
                                    </div>
                                </div>
                            </div>
                            <DialogFooter>
                                <Button onClick={handleCreate}>Create Tag</Button>
                            </DialogFooter>
                        </DialogContent>
                    </Dialog>
                </div>
            </div>

            <div className="rounded-md border bg-white">
                <Table>
                    <TableHeader>
                        <TableRow>
                            <TableHead>Code</TableHead>
                            <TableHead>Alias</TableHead>
                            <TableHead>Type</TableHead>
                            <TableHead>History</TableHead>
                            <TableHead>Alarm</TableHead>
                            <TableHead className="text-right">Actions</TableHead>
                        </TableRow>
                    </TableHeader>
                    <TableBody>
                        {tagsList.length === 0 ? (
                            <TableRow>
                                <TableCell colSpan={6} className="h-24 text-center">
                                    No tags found. {selectedGatewayId && selectedGatewayId !== 'all' ? 'Create one for the selected gateway.' : 'Select a gateway to view tags.'}
                                </TableCell>
                            </TableRow>
                        ) : (
                            tagsList.map((tag) => (
                                <TableRow key={tag.id}>
                                    <TableCell className="font-medium font-mono text-xs">{tag.code}</TableCell>
                                    <TableCell className="font-medium">{tag.alias || '-'}</TableCell>
                                    <TableCell>
                                        <Badge variant="secondary" className="text-xs">{tag.data_type}</Badge>
                                    </TableCell>
                                    <TableCell>
                                        {tag.historize ? (
                                            <div className="flex items-center gap-1 text-xs text-green-600">
                                                <Database size={12} />
                                                <span>Yes (DB: {tag.deadband_value})</span>
                                            </div>
                                        ) : (
                                            <span className="text-xs text-muted-foreground">Disabled</span>
                                        )}
                                    </TableCell>
                                    <TableCell>
                                        {tag.alarm_enabled ? (
                                            <div className="flex items-center gap-1 text-xs text-amber-600">
                                                <Bell size={12} />
                                                <span>{tag.alarm_operator} {tag.alarm_threshold} (P{tag.alarm_priority})</span>
                                            </div>
                                        ) : (
                                            <span className="text-xs text-muted-foreground">Disabled</span>
                                        )}
                                    </TableCell>
                                    <TableCell className="text-right">
                                        <div className="flex items-center justify-end gap-2">
                                            <Button
                                                variant="ghost"
                                                size="icon"
                                                className="h-8 w-8 text-slate-500 hover:text-blue-600 hover:bg-blue-50"
                                            // Edit logic to be implemented
                                            >
                                                <Edit2 size={16} />
                                            </Button>
                                            <Button
                                                variant="ghost"
                                                size="icon"
                                                className="h-8 w-8 text-red-500 hover:text-red-600 hover:bg-red-50"
                                                onClick={(e) => handleDelete(e, tag.id)}
                                            >
                                                <Trash2 size={16} />
                                            </Button>
                                        </div>
                                    </TableCell>
                                </TableRow>
                            ))
                        )}
                    </TableBody>
                </Table>
            </div>
        </div>
    );
};

export default TagsPage;
