import { useState, useMemo, useEffect, useDeferredValue } from 'react';
import { useTags } from '@/hooks/useTags';
import { tagsApi } from '@/api/tags';
import { gatewaysApi } from '@/api/gateways';
import { alarmsApi } from '@/api/alarms';
import { useGateways } from '@/hooks/useGateways';
import { useRealtime } from '@/hooks/useRealtime';
import { useNavigationStore } from '@/stores/useNavigationStore';
import { useAuthStore } from '@/stores/useAuthStore';
import { useStaleData, getDataStatus, getStatusClasses } from '@/hooks/useStaleData';
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
import {
    Tabs,
    TabsContent,
    TabsList,
    TabsTrigger,
} from '@/components/ui/tabs';
import { TagAlarmsTab } from '@/components/tags/TagAlarmsTab';

import { Plus, Trash2, Edit2, Database, Upload, Download, ArrowUp, ArrowDown, CheckSquare, Square, X, Bell, BellRing, Pencil } from 'lucide-react';
import { CreateTagDto, OpcUaNode } from '@/types';
import { useSearchParams } from 'react-router-dom';
import { Badge } from '@/components/ui/badge';

interface CurrentValue {
    value: any;
    timestamp: number;
    quality: number;
}

function SortIcon({ field, current, dir }: { field: string; current: string | null; dir: 'asc' | 'desc' }) {
    if (current !== field) return <span className="ml-1 opacity-20">↕</span>;
    return <span className="ml-1">{dir === 'asc' ? '↑' : '↓'}</span>;
}

const TagsPage = () => {
    const [searchParams] = useSearchParams();
    const gatewayIdParam = searchParams.get('gateway_id');
    const [selectedGatewayId, setSelectedGatewayId] = useState<string>(gatewayIdParam || 'all');
    const [searchInput, setSearchInput] = useState<string>('');
    const searchQuery = useDeferredValue(searchInput);
    const [filterDriverType, setFilterDriverType] = useState('all');
    const [sortField, setSortField] = useState<'alias' | 'code' | 'data_type' | 'sort_order' | null>(null);
    const [sortDir, setSortDir] = useState<'asc' | 'desc'>('asc');
    const { selectedOrgId } = useNavigationStore();
    const { isAdmin } = useAuthStore();

    useEffect(() => {
        if (gatewayIdParam) {
            setSelectedGatewayId(gatewayIdParam);
        }
    }, [gatewayIdParam]);

    const { gateways } = useGateways(); // Get all gateways for filter

    // Create a map of gateways by ID for quick lookup (for Sparkplug B device status)
    const gatewayMap = useMemo(() => {
        const map = new Map<number, { name: string }>();
        gateways.forEach(g => map.set(g.id, { name: g.name }));
        return map;
    }, [gateways]);

    const { tags, isLoading, create, remove, update } = useTags(
        selectedGatewayId && selectedGatewayId !== 'all' ? parseInt(selectedGatewayId) : undefined
    );

    const [isOpen, setIsOpen] = useState(false);
    const [writeDialogTag, setWriteDialogTag] = useState<{ id: number; alias: string; dataType: string; currentValue: any } | null>(null);
    const [writeValue, setWriteValue] = useState<string>('');
    const [writeLoading, setWriteLoading] = useState(false);

    const handleTagWrite = async () => {
        if (!writeDialogTag) return;
        setWriteLoading(true);
        try {
            let value: any = writeValue;
            if (writeDialogTag.dataType === 'BOOL') {
                value = writeValue === 'true';
            } else if (writeDialogTag.dataType === 'INT' || writeDialogTag.dataType === 'UINT' || writeDialogTag.dataType === 'DINT') {
                value = parseInt(writeValue, 10);
            } else if (writeDialogTag.dataType === 'REAL') {
                value = parseFloat(writeValue);
            }
            await tagsApi.writeTag(writeDialogTag.id, { value });
            setWriteDialogTag(null);
        } catch (err) {
            console.error('Write failed:', err);
        } finally {
            setWriteLoading(false);
        }
    };
    const [updatingTagId, setUpdatingTagId] = useState<number | null>(null);
    const [formData, setFormData] = useState<Partial<CreateTagDto>>({
        code: '',
        alias: '',
        data_type: 'REAL',
        historize: false,
        deadband_value: 0.1,
        json_path: '',

    });

    // Modbus builder state
    const [modbusType, setModbusType] = useState<string>('holding');
    const [modbusAddress, setModbusAddress] = useState<number>(1);
    const [modbusBit, setModbusBit] = useState<number>(0);

    // Active Alarms Map state
    const [activeAlarms, setActiveAlarms] = useState<Record<number, boolean>>({});
    // Alarm definitions count per tag
    const [alarmDefsCount, setAlarmDefsCount] = useState<Record<number, number>>({});
    const [alarmRefreshKey, setAlarmRefreshKey] = useState(0);

    // Import/Export state
    const [isImportOpen, setIsImportOpen] = useState(false);
    const [importContent, setImportContent] = useState('');
    const [isImporting, setIsImporting] = useState(false);
    const [importResult, setImportResult] = useState<{ created: number; updated: number; errors?: string[] } | null>(null);

    const [isHistorizeImport, setIsHistorizeImport] = useState(false);

    // Batch Selection State
    const [selectedTagIds, setSelectedTagIds] = useState<number[]>([]);

    // OPC UA Browse State
    const [isBrowseOpen, setIsBrowseOpen] = useState(false);
    const [browseNodes, setBrowseNodes] = useState<OpcUaNode[]>([]);
    const [browsePath, setBrowsePath] = useState<{ nodeId: string; name: string }[]>([{ nodeId: 'i=84', name: 'Root' }]);
    const [isBrowsing, setIsBrowsing] = useState(false);
    const [browseError, setBrowseError] = useState<string | null>(null);

    // Import/Export handlers
    const handleImport = async () => {
        if (!selectedGatewayId || selectedGatewayId === 'all') return;

        setIsImporting(true);
        setImportResult(null);

        try {
            const result = await tagsApi.importTags(parseInt(selectedGatewayId), importContent, isHistorizeImport);
            setImportResult(result);
            if (result.created > 0 || result.updated > 0) {
                // Refresh tags
                window.location.reload();
            }
        } catch (error) {
            console.error('Import failed:', error);
            setImportResult({ created: 0, updated: 0, errors: ['Import failed: ' + String(error)] });
        } finally {
            setIsImporting(false);
        }
    };

    const handleExport = async () => {
        if (!selectedGatewayId || selectedGatewayId === 'all') return;

        try {
            const content = await tagsApi.exportTags(parseInt(selectedGatewayId));
            // Download as .txt file
            const blob = new Blob([content], { type: 'text/plain' });
            const url = URL.createObjectURL(blob);
            const a = document.createElement('a');
            a.href = url;
            a.download = `tags_gateway_${selectedGatewayId}.txt`;
            document.body.appendChild(a);
            a.click();
            document.body.removeChild(a);
            URL.revokeObjectURL(url);
        } catch (error) {
            console.error('Export failed:', error);
            alert('Export failed: ' + String(error));
        }
    };

    // Fetch active alarms + alarm definition counts per tag
    // Fetch active alarms + alarm definition counts per gateway
    useEffect(() => {
        const fetchAlarmData = async () => {
            try {
                // Fetch all active alarms first
                const activeList = await alarmsApi.getActiveAlarms();
                const activeMap: Record<number, boolean> = {};
                (activeList || []).forEach((a: any) => { activeMap[a.tag_id] = true; });
                setActiveAlarms(activeMap);
            } catch (err) {
                console.error("Failed to fetch active alarms:", err);
            }

            // Fetch alarm definitions count explicitly for the selected gateway
            if (selectedGatewayId && selectedGatewayId !== 'all') {
                try {
                    const counts = await alarmsApi.getGatewayAlarmCounts(Number(selectedGatewayId));
                    setAlarmDefsCount(counts || {});
                } catch (err) {
                    console.error("Failed to fetch alarm counts:", err);
                }
            } else if (selectedGatewayId === 'all') {
                try {
                    const counts = await alarmsApi.getAllAlarmCounts();
                    setAlarmDefsCount(counts || {});
                } catch (err) {
                    console.error("Failed to fetch all alarm counts:", err);
                }
            }
        };

        if (tags.length > 0) {
            fetchAlarmData();
            const interval = setInterval(fetchAlarmData, 15000);
            return () => clearInterval(interval);
        }
    }, [tags, selectedGatewayId, alarmRefreshKey]);

    const refreshAlarmData = () => setAlarmRefreshKey(k => k + 1);

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

    // OPC UA Browse handlers
    const handleBrowseOpen = async () => {
        setIsBrowseOpen(true);
        setBrowsePath([{ nodeId: 'i=84', name: 'Root' }]);
        setBrowseError(null);
        await loadBrowseNodes('i=84');
    };

    const loadBrowseNodes = async (nodeId: string) => {
        if (!selectedGatewayId || selectedGatewayId === 'all') return;
        setIsBrowsing(true);
        setBrowseError(null);
        try {
            const result = await gatewaysApi.browseNodes(parseInt(selectedGatewayId), nodeId);
            setBrowseNodes(result.nodes || []);
            if (result.error) setBrowseError(result.error);
        } catch (err: any) {
            setBrowseError(err.message || 'Browse failed');
            setBrowseNodes([]);
        }
        setIsBrowsing(false);
    };

    const handleBrowseNavigate = async (node: OpcUaNode) => {
        if (node.node_class === 'Object' && node.children_count > 0) {
            setBrowsePath(prev => [...prev, { nodeId: node.node_id, name: node.display_name || node.name }]);
            await loadBrowseNodes(node.node_id);
        }
    };

    const handleBrowseBack = async (index: number) => {
        const newPath = browsePath.slice(0, index + 1);
        setBrowsePath(newPath);
        await loadBrowseNodes(newPath[newPath.length - 1].nodeId);
    };

    const mapOpcUaDataType = (opcuaType: string): 'BOOL' | 'INT' | 'REAL' | 'DINT' | 'STRING' => {
        switch (opcuaType) {
            case 'Boolean': return 'BOOL';
            case 'Int16': case 'UInt16': case 'SByte': case 'Byte': return 'INT';
            case 'Int32': case 'UInt32': return 'DINT';
            case 'Float': return 'REAL';
            case 'Double': return 'REAL';
            case 'String': case 'ByteString': case 'DateTime': return 'STRING';
            default: return 'REAL';
        }
    };

    const handleAddNodeAsTag = (node: OpcUaNode) => {
        setFormData({
            code: node.node_id,
            alias: node.display_name || node.name,
            data_type: mapOpcUaDataType(node.data_type),
            historize: false,
            deadband_value: 0.1,
        json_path: '',
            gateway_id: parseInt(selectedGatewayId!),
        });
        setIsBrowseOpen(false);
        setUpdatingTagId(null);
        setIsOpen(true);
    };

    // Real-time value integration
    const [currentValues, setCurrentValues] = useState<Map<number, CurrentValue>>(new Map());
    const { values: realtimeValues } = useRealtime(selectedOrgId || undefined);

    // Data status detection hook (uses Sparkplug B BIRTH/DEATH)
    const { isDeviceOnline } = useStaleData();

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
        json_path: '',
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
            json_path: tag.json_path || '',
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
        json_path: '',

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

    // Batch Action Handlers
    const toggleSelect = (id: number) => {
        setSelectedTagIds(prev =>
            prev.includes(id) ? prev.filter(p => p !== id) : [...prev, id]
        );
    };

    const handleSelectAll = () => {
        if (selectedTagIds.length === tagsList.length) {
            setSelectedTagIds([]);
        } else {
            setSelectedTagIds(tagsList.map(t => t.id));
        }
    };

    const handleBatchDelete = async () => {
        if (!confirm(`Delete ${selectedTagIds.length} tags?`)) return;

        // Execute all deletions in parallel for instant UI update
        await Promise.all(selectedTagIds.map(id => remove(id)));
        setSelectedTagIds([]);
    };

    const handleSort = (field: 'alias' | 'code' | 'data_type' | 'sort_order') => {
        if (sortField === field) {
            setSortDir(d => d === 'asc' ? 'desc' : 'asc');
        } else {
            setSortField(field);
            setSortDir('asc');
        }
    };

    /*
    const _handleMoveSelected = async (direction: 'up' | 'down') => {
        if (selectedTagIds.length === 0) return;

        // Create a copy of the current tags list
        const currentList = [...tagsList];
        const selectedSet = new Set(selectedTagIds);

        // Sort selected IDs based on current position to handle moving multiple contiguous items
        // For 'up': process from top to bottom
        // For 'down': process from bottom to top
        const selectedIndices = currentList
            .map((tag, index) => ({ id: tag.id, index }))
            .filter(item => selectedSet.has(item.id))
            .sort((a, b) => direction === 'up' ? a.index - b.index : b.index - a.index);

        let newList = [...currentList];

        for (const { index } of selectedIndices) {
            const newIndex = direction === 'up' ? index - 1 : index + 1;

            // Boundary checks
            if (newIndex < 0 || newIndex >= newList.length) continue;

            // Swap if the target adjacent item is NOT also selected (moving block logic)
            // simplified: actually just swap with neighbor
            // But if neighbor is also selected, we don't swap with it? 
            // Standard behavior: 
            // - Move Up: Swap with element above if above element is NOT selected. 
            // - If above element IS selected, it should have already moved up (if processing top-down).


            // If we are moving UP, and the item ABOVE is also selected, we don't swap (they move together)
            // But since we process top-down for UP, the item above would have moved first if it could.

            // Let's us array manipulation: remove and insert.
            const item = newList[index];
            newList.splice(index, 1);
            newList.splice(newIndex, 0, item);
        }

        // Optimistic UI update (optional, but good for responsiveness)
        // Since we rely on backend sort_order, we must call API.

        // Extract new order of IDs
        const newOrderIds = newList.map(t => t.id);

        try {
            await tagsApi.reorder(newOrderIds);
            // Trigger refresh or rely on realtime update if implemented? 
            // tagsApi.reorder should theoretically trigger a reload if we listened, but here we just reload the list
            window.location.reload(); // Simple reload to get new order
        } catch (error) {
            console.error("Failed to reorder tags", error);
            alert("Failed to reorder tags");
        }
        }
    };
    */

    const handleMoveSingle = async (id: number, direction: 'up' | 'down') => {
        const index = tagsList.findIndex(t => t.id === id);
        if (index === -1) return;

        const newIndex = direction === 'up' ? index - 1 : index + 1;
        if (newIndex < 0 || newIndex >= tagsList.length) return;

        const newList = [...tagsList];
        const item = newList[index];
        newList.splice(index, 1);
        newList.splice(newIndex, 0, item);

        const newOrderIds = newList.map(t => t.id);

        try {
            await tagsApi.reorder(newOrderIds);
            window.location.reload();
        } catch (error) {
            console.error("Failed to reorder tag", error);
            alert("Failed to reorder tag");
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

    // (Active alarms are already fetched in the other useEffect hook 'fetchAlarmData' at the top of the file)

    // Clear current values when gateway changes
    useEffect(() => {
        setCurrentValues(new Map());
    }, [selectedGatewayId]);

    const tagsList = useMemo(() => {
        let filtered = [...tags];

        // Driver type filter
        if (filterDriverType !== 'all') {
            const gwDriverMap = new Map(gateways.map(g => [g.id, g.driver_type]));
            filtered = filtered.filter(t => gwDriverMap.get(t.gateway_id) === filterDriverType);
        }

        // Search filter
        if (searchQuery) {
            const lower = searchQuery.toLowerCase();
            filtered = filtered.filter(t =>
                t.code.toLowerCase().includes(lower) ||
                (t.alias && t.alias.toLowerCase().includes(lower))
            );
        }

        // Sort
        if (sortField) {
            filtered.sort((a, b) => {
                const av = (a as any)[sortField] ?? '';
                const bv = (b as any)[sortField] ?? '';
                const cmp = String(av).localeCompare(String(bv), undefined, { numeric: true });
                return sortDir === 'asc' ? cmp : -cmp;
            });
        }

        return filtered;
    }, [tags, searchQuery, filterDriverType, gateways, sortField, sortDir]);

    if (isLoading) {
        return <div className="p-8 text-center text-muted-foreground">Loading tags...</div>;
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


                    <div className="w-[300px]">
                        <Input
                            placeholder="Search tags..."
                            value={searchInput}
                            onChange={(e) => setSearchInput(e.target.value)}
                            className="w-full"
                        />
                    </div>

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

                    <div className="w-[160px]">
                        <Select value={filterDriverType} onValueChange={setFilterDriverType}>
                            <SelectTrigger className="clip-chamfer-sm">
                                <SelectValue placeholder="All drivers" />
                            </SelectTrigger>
                            <SelectContent>
                                <SelectItem value="all">All drivers</SelectItem>
                                <SelectItem value="S7">Siemens S7</SelectItem>
                                <SelectItem value="MODBUS_TCP">Modbus TCP</SelectItem>
                                <SelectItem value="OPC_UA">OPC-UA</SelectItem>
                                <SelectItem value="MQTT">MQTT</SelectItem>
                                <SelectItem value="LORAWAN">LoRaWAN</SelectItem>
                            </SelectContent>
                        </Select>
                    </div>

                    {isAdmin() && (
                        <>
                            {/* Export Button */}
                            <Button
                                variant="outline"
                                className="gap-2"
                                onClick={handleExport}
                                disabled={!selectedGatewayId || selectedGatewayId === 'all'}
                            >
                                <Download size={16} /> Export
                            </Button>

                            {/* Import Dialog */}
                            <Dialog open={isImportOpen} onOpenChange={setIsImportOpen}>
                                <DialogTrigger asChild>
                                    <Button
                                        variant="outline"
                                        className="gap-2"
                                        disabled={!selectedGatewayId || selectedGatewayId === 'all'}
                                    >
                                        <Upload size={16} /> Import
                                    </Button>
                                </DialogTrigger>
                                <DialogContent className="max-w-2xl">
                                    <DialogHeader>
                                        <DialogTitle>Import Tags</DialogTitle>
                                        <DialogDescription>
                                            {selectedGatewayDriverType === 'S7' ? (
                                                <span>Paste S7 Data Block (DB) definitions from TIA Portal (STL syntax). Format: <code className="bg-muted px-1 rounded">Alias : DataType AT Address;</code></span>
                                            ) : (
                                                <span>Paste tag definitions in standard format. Format: <code className="bg-muted px-1 rounded">Alias : DataType AT Address;</code></span>
                                            )}
                                        </DialogDescription>
                                    </DialogHeader>
                                    <div className="space-y-4">
                                        {selectedGatewayDriverType === 'S7' && (
                                            <div className="text-xs bg-blue-50 border border-blue-200 text-blue-800 p-3 rounded-md">
                                                <p className="font-semibold mb-1">Siemens S7 Import Guide:</p>
                                                <ul className="list-disc pl-4 space-y-1">
                                                    <li>Use standard S7 absolute addressing (e.g., <code className="font-semibold">DB1.DBX0.0</code>, <code className="font-semibold">DB2.DBW4</code>, <code className="font-semibold">DB3.DBD8</code>)</li>
                                                    <li>Supported types: BOOL, INT, DINT, REAL, STRING, WORD, etc.</li>
                                                    <li>You can paste data block source exports directly but you <strong>must append the absolute address</strong> after the <code>AT</code> keyword.</li>
                                                </ul>
                                            </div>
                                        )}
                                        <textarea
                                            className="w-full h-64 p-3 font-mono text-sm border rounded-md"
                                            placeholder={selectedGatewayDriverType === 'S7' ?
                                                `Totalizzatore_1 : BOOL AT DB1.DBX0.0;\nPortata_Misuratore : INT AT DB1.DBW2;\nHMI_PortataTotale : REAL AT DB1.DBD8;` :
                                                `HMI_CFG_HBeg_1 : DINT AT 42095;\nHMI_PortataTotEM1 : REAL AT 42131;\nDO_Valvola_1 : BOOL AT 00001;`
                                            }
                                            value={importContent}
                                            onChange={(e) => setImportContent(e.target.value)}
                                        />
                                        <div className="flex items-center space-x-2">
                                            <Switch
                                                id="historize-import"
                                                checked={isHistorizeImport}
                                                onCheckedChange={setIsHistorizeImport}
                                            />
                                            <Label htmlFor="historize-import">Enable History for all imported tags</Label>
                                        </div>
                                        {importResult && (
                                            <div className={`p-3 rounded-md text-sm ${importResult.errors?.length ? 'bg-amber-50 border border-amber-200' : 'bg-green-50 border border-green-200'}`}>
                                                <p>Created: {importResult.created} | Updated: {importResult.updated}</p>
                                                {importResult.errors && importResult.errors.length > 0 && (
                                                    <div className="mt-2 text-red-600">
                                                        <p className="font-medium">Errors:</p>
                                                        <ul className="list-disc list-inside max-h-32 overflow-auto">
                                                            {importResult.errors.map((err, i) => (
                                                                <li key={i}>{err}</li>
                                                            ))}
                                                        </ul>
                                                    </div>
                                                )}
                                            </div>
                                        )}
                                    </div>
                                    <DialogFooter>
                                        <Button variant="outline" onClick={() => setIsImportOpen(false)}>
                                            Cancel
                                        </Button>
                                        <Button onClick={handleImport} disabled={isImporting || !importContent.trim()}>
                                            {isImporting ? 'Importing...' : 'Import'}
                                        </Button>
                                    </DialogFooter>
                                </DialogContent>
                            </Dialog>

                            {/* OPC UA Browse Button */}
                            {selectedGatewayDriverType === 'OPC_UA' && (
                                <Button
                                    variant="outline"
                                    className="gap-2 border-[#c8e600]/50 text-[#4a5500] dark:text-[#c8e600] hover:bg-[#c8e600]/10"
                                    onClick={handleBrowseOpen}
                                    disabled={!selectedGatewayId || selectedGatewayId === 'all'}
                                >
                                    <Database size={16} /> Browse Server
                                </Button>
                            )}

                            {/* Add Tag Dialog */}
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
                                    <Tabs defaultValue="general" className="w-full mt-4">
                                        <TabsList className="mb-4">
                                            <TabsTrigger value="general">Generale</TabsTrigger>
                                            {updatingTagId && <TabsTrigger value="alarms">Allarmi</TabsTrigger>}
                                        </TabsList>
                                        <TabsContent value="general">
                                            <div className="grid gap-6 py-4">
                                                <div className="grid grid-cols-2 gap-4">
                                                    {/* Modbus Address Builder */}
                                                    {selectedGatewayDriverType === 'MODBUS_TCP' && (
                                                        <div className="col-span-2 space-y-4 p-4 bg-muted/50 rounded-md border">
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
                                                                        min="0"
                                                                        value={modbusAddress}
                                                                        onChange={(e) => handleModbusAddressChange(parseInt(e.target.value) || 0)}
                                                                        placeholder="0"
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
                                                        <Label htmlFor="code">
                                                            {selectedGatewayDriverType === 'MQTT' ? 'MQTT Topic' : selectedGatewayDriverType === 'LORAWAN' ? 'Device / Field' : 'Tag Code / Address'}
                                                        </Label>
                                                        <Input
                                                            id="code"
                                                            value={formData.code}
                                                            onChange={(e) => handleInputChange('code', e.target.value)}
                                                            placeholder={
                                                                selectedGatewayDriverType === 'MQTT' ? 'e.g. machine/line1/temp' :
                                                                selectedGatewayDriverType === 'LORAWAN' ? 'e.g. sensor-01/temperature' :
                                                                'e.g. %MW100 or 40001'
                                                            }
                                                        />
                                                        {selectedGatewayDriverType === 'MODBUS_TCP' && (
                                                            <p className="text-[10px] text-muted-foreground">Auto-generated from builder above, or type manually</p>
                                                        )}
                                                        {selectedGatewayDriverType === 'MQTT' && (
                                                            <p className="text-[10px] text-emerald-600">Enter the exact MQTT topic the PLC publishes to.</p>
                                                        )}
                                                        {selectedGatewayDriverType === 'LORAWAN' && (
                                                            <p className="text-[10px] text-violet-600">Format: <code>device_id/field</code> — e.g. <code>sensor-01/temperature</code>. Special fields: rssi, snr, f_port. Wildcard device: <code>*/temperature</code></p>
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

                                                    {/* JSON path — MQTT only */}
                                                    {selectedGatewayDriverType === 'MQTT' && (
                                                    <div className="grid gap-2 border-t pt-3 mt-2">
                                                        <Label htmlFor="json_path" className="text-sm flex items-center gap-2">
                                                            JSON path <span className="text-[10px] font-normal text-muted-foreground">(opzionale)</span>
                                                        </Label>
                                                        <Input
                                                            id="json_path"
                                                            value={formData.json_path || ''}
                                                            onChange={(e) => handleInputChange('json_path', e.target.value)}
                                                            placeholder="es. temp   oppure   data.values.0.temperature"
                                                            className="font-mono text-sm"
                                                        />
                                                        <p className="text-[10px] text-muted-foreground">
                                                            Per payload JSON: estrae il campo a questo percorso (notazione dotted). Lascia vuoto se il payload È già il valore.
                                                        </p>
                                                    </div>
                                                    )}
                                                </div>
                                            </div>
                                            <DialogFooter>
                                                <Button onClick={handleSaveWithValidation}>{updatingTagId ? 'Update Tag' : 'Create Tag'}</Button>
                                            </DialogFooter>
                                        </TabsContent>

                                        {updatingTagId && (
                                            <TabsContent value="alarms">
                                                <TagAlarmsTab tagId={updatingTagId} dataType={formData.data_type || 'REAL'} onSave={refreshAlarmData} />
                                            </TabsContent>
                                        )}
                                    </Tabs>
                                </DialogContent>
                            </Dialog>
                        </>
                    )}
                </div>
            </div>


            {/* Bulk Actions Toolbar */}
            {
                selectedTagIds.length > 0 && (
                    <div className="flex items-center gap-2 p-2 bg-[#c8e600]/10 border border-[#c8e600]/30 rounded-md text-foreground animate-in fade-in slide-in-from-top-2">
                        <span className="text-sm font-medium px-2">{selectedTagIds.length} Selected</span>
                        <div className="h-4 w-px bg-border mx-2" />
                        <Button variant="ghost" size="sm" onClick={handleBatchDelete} className="gap-1 hover:bg-red-100 text-red-600 hover:text-red-700">
                            <Trash2 size={16} /> Delete Selected
                        </Button>
                        <div className="flex-1" />
                        <Button variant="ghost" size="sm" onClick={() => setSelectedTagIds([])} className="gap-1 hover:bg-[#c8e600]/20 text-[#4a5500] dark:text-[#c8e600]">
                            <X size={16} /> Cancel
                        </Button>
                    </div>
                )
            }

            {/* Stats bar */}
            <div className="flex items-center gap-6 text-sm text-muted-foreground px-1">
                <span>
                    <strong className="text-foreground">{tagsList.length}</strong>
                    {tagsList.length !== tags.length && ` / ${tags.length}`} tags
                </span>
                <span>
                    <strong className="text-green-600">{tagsList.filter(t => {
                        const v = currentValues.get(t.id);
                        return v && v.quality === 0;
                    }).length}</strong> Good quality
                </span>
                <span>
                    <strong className="text-amber-500">{tagsList.filter(t => alarmDefsCount[t.id] > 0).length}</strong> with alarms
                </span>
                {(tagsList.length !== tags.length || filterDriverType !== 'all') && (
                    <Button variant="ghost" size="sm" className="h-6 text-xs" onClick={() => { setSearchInput(''); setSelectedGatewayId('all'); setFilterDriverType('all'); }}>
                        Clear filters
                    </Button>
                )}
            </div>

            <div className="clip-chamfer border bg-card">
                <Table>
                    <TableHeader>
                        <TableRow>
                            {isAdmin() && (
                                <TableHead className="w-[40px]">
                                    <Button variant="ghost" size="sm" className="p-0 h-6 w-6" onClick={handleSelectAll}>
                                        {selectedTagIds.length === tagsList.length && tagsList.length > 0 ? (
                                            <CheckSquare size={16} className="text-[#4a5500] dark:text-[#c8e600]" />
                                        ) : (
                                            <Square size={16} className="text-slate-300" />
                                        )}
                                    </Button>
                                </TableHead>
                            )}
                            <TableHead className="cursor-pointer select-none hover:bg-muted/50" onClick={() => handleSort('code')}>
                                Code <SortIcon field="code" current={sortField} dir={sortDir} />
                            </TableHead>
                            <TableHead className="cursor-pointer select-none hover:bg-muted/50" onClick={() => handleSort('alias')}>
                                Alias <SortIcon field="alias" current={sortField} dir={sortDir} />
                            </TableHead>
                            <TableHead className="cursor-pointer select-none hover:bg-muted/50" onClick={() => handleSort('data_type')}>
                                Type <SortIcon field="data_type" current={sortField} dir={sortDir} />
                            </TableHead>
                            <TableHead>Current Value</TableHead>
                            <TableHead>History</TableHead>
                            <TableHead>Allarmi</TableHead>

                            {isAdmin() && <TableHead className="text-right">Actions</TableHead>}
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
                                    <TableRow key={tag.id} className={`${selectedTagIds.includes(tag.id) ? 'bg-muted/50' : 'hover:bg-muted/30'}`}>
                                        {isAdmin() && (
                                            <TableCell>
                                                <div className="flex items-center gap-1">
                                                    <Button
                                                        variant="ghost"
                                                        size="sm"
                                                        className="p-0 h-6 w-6"
                                                        onClick={(e) => { e.stopPropagation(); toggleSelect(tag.id); }}
                                                    >
                                                        {selectedTagIds.includes(tag.id) ? (
                                                            <CheckSquare size={16} className="text-[#4a5500] dark:text-[#c8e600]" />
                                                        ) : (
                                                            <Square size={16} className="text-slate-300" />
                                                        )}
                                                    </Button>
                                                    {selectedTagIds.includes(tag.id) && (
                                                        <div className="flex flex-col gap-0 animate-in fade-in zoom-in duration-200">
                                                            <Button
                                                                variant="ghost"
                                                                size="icon"
                                                                className="h-5 w-5 p-0 text-[#4a5500] dark:text-[#c8e600] hover:bg-[#c8e600]/20"
                                                                onClick={(e) => { e.stopPropagation(); handleMoveSingle(tag.id, 'up'); }}
                                                            >
                                                                <ArrowUp size={14} />
                                                            </Button>
                                                            <Button
                                                                variant="ghost"
                                                                size="icon"
                                                                className="h-5 w-5 p-0 text-blue-600 hover:bg-blue-100"
                                                                onClick={(e) => { e.stopPropagation(); handleMoveSingle(tag.id, 'down'); }}
                                                            >
                                                                <ArrowDown size={14} />
                                                            </Button>
                                                        </div>
                                                    )}
                                                </div>
                                            </TableCell>
                                        )}

                                        <TableCell className="font-medium font-mono text-xs">
                                            <div className="flex items-center gap-2">
                                                {tag.code}
                                            </div>
                                        </TableCell>
                                        <TableCell className="font-medium">
                                            <div className="flex items-center gap-2">
                                                {tag.alias || '-'}
                                                {activeAlarms[tag.id] && (
                                                    <Badge variant="destructive" className="h-5 px-1.5 flex gap-1 items-center animate-pulse">
                                                        <Bell size={10} /> Allarme
                                                    </Badge>
                                                )}
                                            </div>
                                        </TableCell>
                                        <TableCell>
                                            <Badge variant="secondary" className="text-xs">{tag.data_type}</Badge>
                                        </TableCell>
                                        <TableCell>
                                            <div className="flex items-center gap-3">
                                                {/* Value display with distinct styling */}
                                                {(() => {
                                                    const deviceName = gatewayMap.get(tag.gateway_id)?.name;
                                                    const deviceOnline = isDeviceOnline(deviceName);
                                                    const status = getDataStatus(currentValue?.quality, deviceOnline);
                                                    const statusClasses = getStatusClasses(status);

                                                    return (
                                                        <>
                                                            {/* Quality dot */}
                                                            {currentValue && (
                                                                <span className={[
                                                                    'inline-block w-2 h-2 rounded-full shrink-0',
                                                                    currentValue.quality === 0 ? 'bg-green-500' :
                                                                    currentValue.quality === 1 ? 'bg-yellow-500' : 'bg-red-500'
                                                                ].join(' ')} />
                                                            )}
                                                            <div className={`
                                                                font-mono font-medium px-2 py-1 rounded border min-w-[80px] text-center
                                                                ${statusClasses.bg} ${statusClasses.border} ${statusClasses.text}
                                                            `}>
                                                                {currentValue ? (
                                                                    typeof currentValue.value === 'boolean'
                                                                        ? (currentValue.value ? 'TRUE' : 'FALSE')
                                                                        : (currentValue.value !== null && currentValue.value !== undefined
                                                                            ? (typeof currentValue.value === 'number'
                                                                                ? (tag.data_type === 'DINT' || tag.data_type === 'INT'
                                                                                    ? Math.round(currentValue.value).toLocaleString()
                                                                                    : currentValue.value.toLocaleString(undefined, { maximumFractionDigits: 2 }))
                                                                                : currentValue.value.toString())
                                                                            : '-')
                                                                ) : '-'}
                                                            </div>

                                                            {/* Quality indicator */}
                                                            {currentValue && (
                                                                <Badge variant="outline" className={`text-[10px] h-5 px-1 ${status === 'good'
                                                                    ? 'text-green-500 border-green-500/30 bg-green-500/10'
                                                                    : status === 'unknown'
                                                                        ? 'text-muted-foreground border-border bg-muted'
                                                                        : 'text-red-500 border-red-500/30 bg-red-500/10'
                                                                    }`}>
                                                                    {status === 'good' ? 'GOOD' : status === 'unknown' ? 'UNKNOWN' : 'BAD'}
                                                                </Badge>
                                                            )}

                                                            {/* Timestamp tooltip */}
                                                            {currentValue?.timestamp && (
                                                                <span
                                                                    className={`text-xs ${status === 'bad' ? 'text-red-500 font-medium' : 'text-muted-foreground'}`}
                                                                    title={`Last update: ${new Date(currentValue.timestamp).toLocaleString()}`}
                                                                >
                                                                    {new Date(currentValue.timestamp).toLocaleTimeString([], { hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit' })}
                                                                </span>
                                                            )}
                                                        </>
                                                    );
                                                })()}
                                                {/* Write button */}
                                                {isAdmin() && (
                                                    <Button
                                                        variant="ghost"
                                                        size="icon"
                                                        className="h-6 w-6 shrink-0"
                                                        title="Write value"
                                                        onClick={() => {
                                                            const cv = currentValues.get(tag.id);
                                                            setWriteValue(tag.data_type === 'BOOL'
                                                                ? (cv?.value === true ? 'false' : 'true')
                                                                : (cv?.value != null ? String(cv.value) : '0'));
                                                            setWriteDialogTag({ id: tag.id, alias: tag.alias || '', dataType: tag.data_type, currentValue: cv?.value });
                                                        }}
                                                    >
                                                        <Pencil size={13} />
                                                    </Button>
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
                                            {alarmDefsCount[tag.id] ? (
                                                <div className="flex items-center gap-1">
                                                    {activeAlarms[tag.id] ? (
                                                        <Badge variant="destructive" className="h-5 px-1.5 gap-1 items-center animate-pulse text-xs">
                                                            <BellRing size={10} />
                                                            ATTIVO ({alarmDefsCount[tag.id]})
                                                        </Badge>
                                                    ) : (
                                                        <Badge variant="secondary" className="h-5 px-1.5 gap-1 items-center text-xs bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-400">
                                                            <Bell size={10} />
                                                            {alarmDefsCount[tag.id]} allarme{alarmDefsCount[tag.id] > 1 ? 'i' : ''}
                                                        </Badge>
                                                    )}
                                                </div>
                                            ) : (
                                                <span className="text-xs text-muted-foreground">-</span>
                                            )}
                                        </TableCell>

                                        {isAdmin() && (
                                            <TableCell className="text-right">
                                                <div className="flex items-center justify-end gap-2">
                                                    <Button
                                                        variant="ghost"
                                                        size="icon"
                                                        className="h-8 w-8 text-muted-foreground hover:text-[#4a5500] dark:hover:text-[#c8e600] hover:bg-[#c8e600]/10"
                                                        onClick={() => handleEdit(tag)}
                                                    >
                                                        <Edit2 size={16} />
                                                    </Button>
                                                    <Button
                                                        variant="ghost"
                                                        size="icon"
                                                        className="h-8 w-8 text-red-500 hover:text-red-600 hover:bg-red-500/10"
                                                        onClick={(e) => handleDelete(e, tag.id)}
                                                    >
                                                        <Trash2 size={16} />
                                                    </Button>
                                                </div>
                                            </TableCell>
                                        )}
                                    </TableRow>
                                );
                            })
                        )}
                    </TableBody>
                </Table>
            </div>

            {/* OPC UA Browse Dialog */}
            <Dialog open={isBrowseOpen} onOpenChange={setIsBrowseOpen}>
                <DialogContent className="max-w-3xl max-h-[80vh] flex flex-col">
                    <DialogHeader>
                        <DialogTitle>Browse OPC UA Server</DialogTitle>
                        <DialogDescription>
                            Navigate the OPC UA address space. Click on Object nodes to expand, or click "Add as Tag" on Variable nodes to create a tag.
                        </DialogDescription>
                    </DialogHeader>

                    {/* Breadcrumb Navigation */}
                    <div className="flex items-center gap-1 text-sm flex-wrap">
                        {browsePath.map((item, index) => (
                            <div key={index} className="flex items-center">
                                {index > 0 && <span className="mx-1 text-muted-foreground">/</span>}
                                <button
                                    className={`hover:underline ${index === browsePath.length - 1 ? 'font-semibold text-[#4a5500] dark:text-[#c8e600]' : 'text-muted-foreground'}`}
                                    onClick={() => handleBrowseBack(index)}
                                    disabled={isBrowsing}
                                >
                                    {item.name}
                                </button>
                            </div>
                        ))}
                    </div>

                    {/* Error message */}
                    {browseError && (
                        <div className="p-3 bg-red-50 border border-red-200 rounded-md text-sm text-red-700">
                            {browseError}
                        </div>
                    )}

                    {/* Node List */}
                    <div className="flex-1 overflow-auto border rounded-md min-h-[300px]">
                        {isBrowsing ? (
                            <div className="flex items-center justify-center h-full text-muted-foreground">
                                <div className="animate-spin rounded-full h-6 w-6 border-b-2 border-indigo-600 mr-2"></div>
                                Browsing nodes...
                            </div>
                        ) : browseNodes.length === 0 ? (
                            <div className="flex items-center justify-center h-full text-muted-foreground">
                                No nodes found at this level.
                            </div>
                        ) : (
                            <div className="divide-y">
                                {browseNodes.map((node, index) => (
                                    <div
                                        key={index}
                                        className={`flex items-center justify-between p-3 hover:bg-muted/50 ${node.node_class === 'Object' && node.children_count > 0 ? 'cursor-pointer' : ''}`}
                                        onClick={() => handleBrowseNavigate(node)}
                                    >
                                        <div className="flex items-center gap-3">
                                            <div className={`w-8 h-8 rounded flex items-center justify-center text-xs font-mono ${node.node_class === 'Object'
                                                ? 'bg-[#c8e600]/20 text-[#4a5500] dark:text-[#c8e600]'
                                                : node.node_class === 'Variable'
                                                    ? 'bg-emerald-100 text-emerald-700'
                                                    : 'bg-muted text-muted-foreground'
                                                }`}>
                                                {node.node_class === 'Object' ? '📁' : node.node_class === 'Variable' ? '📊' : '⚡'}
                                            </div>
                                            <div>
                                                <p className="font-medium text-sm">{node.display_name || node.name}</p>
                                                <p className="text-xs text-muted-foreground font-mono">{node.node_id}</p>
                                            </div>
                                        </div>
                                        <div className="flex items-center gap-3">
                                            {node.node_class === 'Variable' && (
                                                <>
                                                    <Badge variant="outline" className="text-xs">{node.data_type}</Badge>
                                                    <Button
                                                        size="sm"
                                                        variant="outline"
                                                        className="text-xs border-[#c8e600]/50 text-[#4a5500] dark:text-[#c8e600] hover:bg-[#c8e600]/10"
                                                        onClick={(e) => {
                                                            e.stopPropagation();
                                                            handleAddNodeAsTag(node);
                                                        }}
                                                    >
                                                        <Plus size={14} className="mr-1" /> Add as Tag
                                                    </Button>
                                                </>
                                            )}
                                            {node.node_class === 'Object' && node.children_count > 0 && (
                                                <span className="text-xs text-muted-foreground">{node.children_count} children →</span>
                                            )}
                                        </div>
                                    </div>
                                ))}
                            </div>
                        )}
                    </div>
                </DialogContent>
            </Dialog>

            {/* Write Tag Dialog */}
            <Dialog open={!!writeDialogTag} onOpenChange={(open) => !open && setWriteDialogTag(null)}>
                <DialogContent className="sm:max-w-md">
                    <DialogHeader>
                        <DialogTitle>Write: {writeDialogTag?.alias}</DialogTitle>
                        <DialogDescription>
                            Send a value to the PLC. This action is logged.
                        </DialogDescription>
                    </DialogHeader>
                    <div className="space-y-4 py-2">
                        <div className="flex items-center gap-2">
                            <span className="text-sm text-muted-foreground">Type:</span>
                            <Badge variant="secondary">{writeDialogTag?.dataType}</Badge>
                        </div>
                        {writeDialogTag?.dataType === 'BOOL' ? (
                            <Select value={writeValue} onValueChange={setWriteValue}>
                                <SelectTrigger>
                                    <SelectValue />
                                </SelectTrigger>
                                <SelectContent>
                                    <SelectItem value="true">TRUE</SelectItem>
                                    <SelectItem value="false">FALSE</SelectItem>
                                </SelectContent>
                            </Select>
                        ) : (
                            <Input
                                type={writeDialogTag?.dataType === 'REAL' ? 'number' : 'text'}
                                step={writeDialogTag?.dataType === 'REAL' ? '0.01' : undefined}
                                value={writeValue}
                                onChange={(e) => setWriteValue(e.target.value)}
                                placeholder="Value"
                                className="font-mono"
                            />
                        )}
                    </div>
                    <DialogFooter>
                        <Button variant="outline" onClick={() => setWriteDialogTag(null)}>Cancel</Button>
                        <Button onClick={handleTagWrite} disabled={writeLoading}>
                            {writeLoading ? 'Writing...' : 'Write'}
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>
        </div >
    );
};

export default TagsPage;
