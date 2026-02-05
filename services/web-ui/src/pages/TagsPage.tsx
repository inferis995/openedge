import { useState, useMemo, useEffect } from 'react';
import { useTags } from '@/hooks/useTags';
import { tagsApi } from '@/api/tags';
import { useGateways } from '@/hooks/useGateways';
import { useRealtime } from '@/hooks/useRealtime';
import { useNavigationStore } from '@/stores/useNavigationStore';
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
    DialogDescription,
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
import { Plus, Trash2, Edit2, Bell, Database, RefreshCw } from 'lucide-react';
import { CreateTagDto } from '@/types';
import { useSearchParams } from 'react-router-dom';
import { Badge } from '@/components/ui/badge';

interface CurrentValue {
    value: any;
    timestamp: number;
    quality: number;
}

const TagsPage = () => {
    const [searchParams] = useSearchParams();
    const gatewayIdParam = searchParams.get('gateway_id');
    const [selectedGatewayId, setSelectedGatewayId] = useState<string>(gatewayIdParam || 'all');
    const { selectedOrgId } = useNavigationStore();

    useEffect(() => {
        if (gatewayIdParam) {
            setSelectedGatewayId(gatewayIdParam);
        }
    }, [gatewayIdParam]);

    const { gateways } = useGateways(); // Get all gateways for filter
    const { tags, isLoading, create, remove, update } = useTags(
        selectedGatewayId && selectedGatewayId !== 'all' ? parseInt(selectedGatewayId) : undefined
    );

    const [isOpen, setIsOpen] = useState(false);
    const [updatingTagId, setUpdatingTagId] = useState<number | null>(null);
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

    // Modbus builder state
    const [modbusType, setModbusType] = useState<string>('holding');
    const [modbusAddress, setModbusAddress] = useState<number>(1);
    const [modbusBit, setModbusBit] = useState<number>(0);

    // Derive selected gateway driver type
    const selectedGatewayDriverType = useMemo(() => {
        if (!selectedGatewayId || selectedGatewayId === 'all') return null;
        const gw = gateways.find(g => g.id.toString() === selectedGatewayId);
        return gw?.driver_type;
    }, [selectedGatewayId, gateways]);

    // Auto-generate Modbus code
    useEffect(() => {
        if (selectedGatewayDriverType !== 'MODBUS_TCP') return;

        let offset = 0;

        switch (modbusType) {
            case 'coil': offset = 0; break;
            case 'discrete': offset = 10000; break;
            case 'input': offset = 30000; break;
            case 'holding': offset = 40000; break;
        }

        // Standard 5-digit format (e.g. 40001)
        // Adjust for address '1' being offset+1
        const codeNum = offset + modbusAddress;
        let code = codeNum.toString().padStart(5, '0');

        // Add bit offset for BOOL in registers
        if (formData.data_type === 'BOOL' && (modbusType === 'holding' || modbusType === 'input')) {
            code += `.${modbusBit}`;
        }

        handleInputChange('code', code);
    }, [modbusType, modbusAddress, modbusBit, formData.data_type, selectedGatewayDriverType]);

    const handleModbusTypeChange = (val: string) => setModbusType(val);
    const handleModbusAddressChange = (val: number) => setModbusAddress(val);
    const handleModbusBitChange = (val: number) => setModbusBit(val);

    // Real-time value integration
    const [currentValues, setCurrentValues] = useState<Map<number, CurrentValue>>(new Map());
    const realtimeValues = useRealtime(selectedOrgId || undefined);

    // Merge real-time updates into currentValues
    useEffect(() => {
        if (realtimeValues.size > 0) {
            setCurrentValues(prev => {
                const next = new Map(prev);
                realtimeValues.forEach((val, id) => {
                    next.set(id, val);
                });
                return next;
            });
        }
    }, [realtimeValues]);


    const handleInputChange = (field: keyof CreateTagDto, value: any) => {
        setFormData(prev => ({ ...prev, [field]: value }));
    };

    const handleSave = async () => {
        try {
            if (updatingTagId) {
                // Update mode
                await update({
                    id: updatingTagId,
                    data: {
                        ...formData,
                        // If selectedGatewayId is valid (not 'all'), use it. 
                        // Otherwise, we rely on the backend not requiring gateway_id for updates unless we want to move it.
                        // Actually, for updates, we might only want to update specific fields.
                        // However, let's keep it simple: if we are filtering by specific gateway, use that.
                        // If 'all', we should probably NOT change the gateway_id unless the user explicitly selected one in a dropdown in the modal (which doesn't exist yet).
                        // Inspecting the UpdateTagRequest, gateway_id is NOT in the struct! It's immutable in the handler anyway for now (or at least not in the struct).
                        // So we don't need to pass gateway_id for update.
                    } as Partial<CreateTagDto>
                });
            } else {
                // Create mode
                if (!selectedGatewayId || selectedGatewayId === 'all') {
                    alert('Please select a specific gateway to create a tag.'); // Simple validation
                    return;
                }
                if (!formData.code) return;
                await create({
                    ...formData,
                    gateway_id: parseInt(selectedGatewayId),
                } as CreateTagDto);
            }

            setIsOpen(false);
            setUpdatingTagId(null);
            // Reset form
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
            console.error('Failed to save tag', error);
        }
    };

    const handleEdit = (tag: any) => {
        setUpdatingTagId(tag.id);
        setFormData({
            code: tag.code,
            alias: tag.alias,
            data_type: tag.data_type,
            historize: tag.historize,
            deadband_value: tag.historize_deadband || 0.1,
            alarm_enabled: tag.alarm_enabled,
            alarm_threshold: tag.alarm_threshold || 0,
            alarm_operator: tag.alarm_operator || '>',
            alarm_priority: tag.alarm_priority || 3,
        });
        setIsOpen(true);
    };

    // Smart Address Calculation Helper
    const calculateNextAddress = (type: string): number => {
        if (!tags || tags.length === 0) return 1;

        let maxAddr = 0;
        let rangeStart = 0;
        let rangeEnd = 0;

        switch (type) {
            case 'coil': rangeStart = 1; rangeEnd = 9999; break;
            case 'discrete': rangeStart = 10001; rangeEnd = 19999; break;
            case 'input': rangeStart = 30001; rangeEnd = 39999; break;
            case 'holding': rangeStart = 40001; rangeEnd = 49999; break;
        }

        tags.forEach(tag => {
            const codeVal = parseFloat(tag.code);
            const addr = Math.floor(codeVal);

            if (addr >= rangeStart && addr <= rangeEnd) {
                let size = 1;
                if (['REAL', 'DINT', 'UDINT', 'DWORD'].includes(tag.data_type)) {
                    size = 2;
                }
                const tagEnd = addr + size - 1;
                if (tagEnd > maxAddr) {
                    maxAddr = tagEnd;
                }
            }
        });

        if (maxAddr > 0) {
            return (maxAddr + 1) - (rangeStart - 1);
        }
        return 1;
    };

    // Update address when type changes
    useEffect(() => {
        if (isOpen && !updatingTagId && selectedGatewayDriverType === 'MODBUS_TCP') {
            const next = calculateNextAddress(modbusType);
            setModbusAddress(next);
        }
    }, [modbusType, isOpen, updatingTagId, selectedGatewayDriverType, tags]);

    const handleCreateOpen = () => {
        setUpdatingTagId(null);
        setModbusType('holding'); // Default

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
        setIsOpen(true);
    };

    const checkAddressOverlap = (newCode: string, newType: string): string | null => {
        // Only valid for Modbus
        if (selectedGatewayDriverType !== 'MODBUS_TCP') return null;

        const newAddr = Math.floor(parseFloat(newCode));

        let newSize = 1;
        if (['REAL', 'DINT', 'UDINT', 'DWORD'].includes(newType)) {
            newSize = 2;
        }
        // If BOOL logic: 
        // - In registers (40001.0): Occupies 1 register (40001), but shares with others?
        //   Technically 1 register is 16 bits.
        //   We should warn if a NON-BOOL tag tries to use 40001 while a BOOL uses 40001.0.
        //   Or if another BOOL uses 40001.0 (exact match).

        // Simpler logic: Check strictly register range usage.
        // Range: [newAddr, newAddr + newSize - 1]

        for (const tag of tags) {
            if (tag.id === updatingTagId) continue; // Skip self update

            const existingAddr = Math.floor(parseFloat(tag.code));
            let existingSize = 1;
            if (['REAL', 'DINT', 'UDINT', 'DWORD'].includes(tag.data_type)) {
                existingSize = 2;
            }

            // Check overlap
            const start1 = newAddr;
            const end1 = newAddr + newSize - 1;
            const start2 = existingAddr;
            const end2 = existingAddr + existingSize - 1;

            if (Math.max(start1, start2) <= Math.min(end1, end2)) {
                // Overlap detected!

                // Exception: Bit-level packing in same register
                // If BOTH are BOOLs in the SAME register, it's allowed (if bits differ)


                if (newType === 'BOOL' && tag.data_type === 'BOOL' && start1 === start2) {
                    // Check exact bit conflict
                    // If both are bit addresses, check bit index
                    // This is complex string parsing, simplified:
                    if (newCode === tag.code) {
                        return `Full collision with tag "${tag.alias}" at ${tag.code}`;
                    }
                    // Else, same register different bits is OK
                    continue;
                }

                return `Memory overlap with tag "${tag.alias}" (${tag.code})`;
            }
        }
        return null;
    };

    // Modified save handler with validation
    const handleSaveWithValidation = async () => {
        // Overlap Check
        if (formData.code && formData.data_type) {
            const overlapError = checkAddressOverlap(formData.code, formData.data_type);
            if (overlapError) {
                if (!confirm(`Warning: ${overlapError}\n\nDo you want to proceed anyway?`)) {
                    return;
                }
            }
        }
        await handleSave();
    };

    const handleDelete = async (e: React.MouseEvent, id: number) => {
        e.stopPropagation();
        if (confirm('Are you sure you want to delete this tag?')) {
            await remove(id);
        }
    };

    // Initial values will come through the WebSocket (useRealtime)
    // Also fetch initial values on page load so we don't show "-" until first change
    useEffect(() => {
        const fetchInitialValues = async () => {
            if (!tags || tags.length === 0) return;

            const newValues = new Map<number, CurrentValue>();

            // Fetch current value for each tag in parallel using tagsApi (includes X-Organization-ID)
            await Promise.allSettled(
                tags.map(async (tag) => {
                    try {
                        const currentValue = await tagsApi.getCurrentValue(tag.id);
                        newValues.set(tag.id, currentValue);
                    } catch (err) {
                        // Silently ignore errors for individual tags (no value in Redis yet)
                    }
                })
            );

            if (newValues.size > 0) {
                setCurrentValues(prev => {
                    const merged = new Map(prev);
                    newValues.forEach((v, k) => merged.set(k, v));
                    return merged;
                });
            }
        };

        fetchInitialValues();
    }, [tags]);

    // Clear current values when gateway changes
    useEffect(() => {
        setCurrentValues(new Map());
    }, [selectedGatewayId]);

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
                    {/* Live status indicator - always on */}
                    <span className="flex items-center gap-2 text-sm text-green-600">
                        <RefreshCw size={14} className="animate-spin" />
                        Live
                    </span>

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
                            <Button className="gap-2" onClick={handleCreateOpen}>
                                <Plus size={16} /> Add Tag
                            </Button>
                        </DialogTrigger>
                        <DialogContent className="max-w-2xl">
                            <DialogHeader>
                                <DialogTitle>{updatingTagId ? 'Edit Tag' : 'Create Tag'}</DialogTitle>
                                <DialogDescription>
                                    {updatingTagId ? 'Modify existing tag configuration.' : 'Add a new tag to the gateway.'}
                                </DialogDescription>
                            </DialogHeader>
                            <div className="grid gap-6 py-4">
                                <div className="grid grid-cols-2 gap-4">
                                    {/* Modbus Address Builder */}
                                    {selectedGatewayDriverType === 'MODBUS_TCP' && (
                                        <div className="col-span-2 space-y-4 p-4 bg-slate-50 rounded-md border">
                                            <h3 className="text-sm font-medium">Modbus Address Builder</h3>
                                            <div className="grid grid-cols-2 gap-4">
                                                <div className="grid gap-2">
                                                    <Label htmlFor="mb-type">Register Type</Label>
                                                    <Select
                                                        value={modbusType}
                                                        onValueChange={handleModbusTypeChange}
                                                    >
                                                        <SelectTrigger>
                                                            <SelectValue />
                                                        </SelectTrigger>
                                                        <SelectContent>
                                                            <SelectItem value="coil">Coil Status (0xxxxx)</SelectItem>
                                                            <SelectItem value="discrete">Discrete Input (1xxxxx)</SelectItem>
                                                            <SelectItem value="input">Input Register (3xxxxx)</SelectItem>
                                                            <SelectItem value="holding">Holding Register (4xxxxx)</SelectItem>
                                                        </SelectContent>
                                                    </Select>
                                                </div>
                                                <div className="grid gap-2">
                                                    <Label htmlFor="mb-addr">Address</Label>
                                                    <Input
                                                        id="mb-addr"
                                                        type="number"
                                                        min="1"
                                                        value={modbusAddress}
                                                        onChange={(e) => handleModbusAddressChange(parseInt(e.target.value) || 0)}
                                                        placeholder="1"
                                                    />
                                                </div>
                                                {/* Bit offset for registers to BOOL */}
                                                {(modbusType === 'holding' || modbusType === 'input') && formData.data_type === 'BOOL' && (
                                                    <div className="grid gap-2">
                                                        <Label htmlFor="mb-bit">Bit Offset (0-15)</Label>
                                                        <Input
                                                            id="mb-bit"
                                                            type="number"
                                                            min="0"
                                                            max="15"
                                                            value={modbusBit}
                                                            onChange={(e) => handleModbusBitChange(parseInt(e.target.value) || 0)}
                                                        />
                                                    </div>
                                                )}
                                            </div>
                                        </div>
                                    )}

                                    <div className="grid gap-2">
                                        <Label htmlFor="code">Tag Code / Address</Label>
                                        <Input
                                            id="code"
                                            value={formData.code}
                                            onChange={(e) => handleInputChange('code', e.target.value)}
                                            placeholder="e.g. %MW100 or 40001"
                                        // Removed readOnly to allow manual override
                                        />
                                        {selectedGatewayDriverType === 'MODBUS_TCP' && (
                                            <p className="text-[10px] text-muted-foreground">Auto-generated from builder above, or type manually</p>
                                        )}
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
                                        onValueChange={(val) => {
                                            handleInputChange('data_type', val);
                                            // Reset bit offset if not BOOL
                                            if (val !== 'BOOL') setModbusBit(0);
                                        }}
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
                                <Button onClick={handleSaveWithValidation}>{updatingTagId ? 'Update Tag' : 'Create Tag'}</Button>
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
                            <TableHead>Current Value</TableHead>
                            <TableHead>History</TableHead>
                            <TableHead>Alarm</TableHead>
                            <TableHead className="text-right">Actions</TableHead>
                        </TableRow>
                    </TableHeader>
                    <TableBody>
                        {tagsList.length === 0 ? (
                            <TableRow>
                                <TableCell colSpan={7} className="h-24 text-center">
                                    No tags found. {selectedGatewayId && selectedGatewayId !== 'all' ? 'Create one for the selected gateway.' : 'Select a gateway to view tags.'}
                                </TableCell>
                            </TableRow>
                        ) : (
                            tagsList.map((tag) => {
                                const currentValue = currentValues.get(tag.id);
                                return (
                                    <TableRow key={tag.id}>
                                        <TableCell className="font-medium font-mono text-xs">{tag.code}</TableCell>
                                        <TableCell className="font-medium">{tag.alias || '-'}</TableCell>
                                        <TableCell>
                                            <Badge variant="secondary" className="text-xs">{tag.data_type}</Badge>
                                        </TableCell>
                                        <TableCell>
                                            <div className="flex items-center gap-3">
                                                {/* Value display with distinct styling */}
                                                <div className={`
                                                    font-mono font-medium px-2 py-1 rounded border min-w-[80px] text-center
                                                    ${currentValue?.quality === 0
                                                        ? 'bg-slate-50 border-slate-200 text-slate-900'
                                                        : 'bg-red-50 border-red-200 text-red-700'
                                                    }
                                                `}>
                                                    {currentValue ? (
                                                        typeof currentValue.value === 'boolean'
                                                            ? (currentValue.value ? 'TRUE' : 'FALSE')
                                                            : (currentValue.value !== null && currentValue.value !== undefined
                                                                ? (typeof currentValue.value === 'number'
                                                                    ? currentValue.value.toLocaleString(undefined, { maximumFractionDigits: 2 })
                                                                    : currentValue.value.toString())
                                                                : '-')
                                                    ) : '-'}
                                                </div>

                                                {/* Quality indicator */}
                                                {currentValue && (
                                                    <Badge variant="outline" className={`text-[10px] h-5 px-1 ${currentValue.quality === 0 ? 'text-green-600 border-green-200 bg-green-50' : 'text-red-600 border-red-200 bg-red-50'}`}>
                                                        {currentValue.quality === 0 ? 'GOOD' : 'BAD'}
                                                    </Badge>
                                                )}

                                                {/* Timestamp tooltip */}
                                                {currentValue?.timestamp && (
                                                    <span className="text-xs text-muted-foreground" title={`Last update: ${new Date(currentValue.timestamp).toLocaleString()}`}>
                                                        {new Date(currentValue.timestamp).toLocaleTimeString([], { hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit' })}
                                                    </span>
                                                )}
                                            </div>
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
                                                    onClick={() => handleEdit(tag)}
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
                                );
                            })
                        )}
                    </TableBody>
                </Table>
            </div>
        </div >
    );
};

export default TagsPage;
