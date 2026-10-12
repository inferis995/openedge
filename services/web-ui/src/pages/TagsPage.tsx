import { useState, useMemo, useEffect, useDeferredValue } from 'react';
import { useTags } from '@/hooks/useTags';
import { tagsApi } from '@/api/tags';
import { gatewaysApi } from '@/api/gateways';
import { alarmsApi } from '@/api/alarms';
import { useGateways } from '@/hooks/useGateways';
import { useRealtime } from '@/hooks/useRealtime';
import { useNavigationStore } from '@/stores/useNavigationStore';
import { useAuthStore } from '@/stores/useAuthStore';
import { useStaleData, getDataStatus } from '@/hooks/useStaleData';
import { timeAgo } from '@/lib/relativeTime';
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

import { Plus, Trash2, Database, Upload, Download, ArrowUp, ArrowDown, CheckSquare, Square, X, Bell, BellRing, Pencil, History, Search, WifiOff, Clock, Settings2 } from 'lucide-react';
import TagHistoryChart from '@/components/TagHistoryChart';
import { CreateTagDto, OpcUaNode } from '@/types';
import { useSearchParams } from 'react-router-dom';
import { Badge } from '@/components/ui/badge';
import { confirmAction } from '@/lib/confirm';
import i18n from '@/i18n';
import { toast } from 'sonner';
import { showApiError } from '@/lib/api-error-handler';
import { EmptyState } from '@/components/ui/empty-state';
import { Tags as TagsIcon } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import TagImportDialog from '@/components/tags/TagImportDialog';
import { saveBlob } from '@/lib/download';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import { useQueryClient } from '@tanstack/react-query';
import type { Tag } from '@/types';

interface CurrentValue {
    value: unknown;
    timestamp: number;
    quality: number;
}

function SortIcon({ field, current, dir }: { field: string; current: string | null; dir: 'asc' | 'desc' }) {
    if (current !== field) return <span className="ml-1 opacity-20">↕</span>;
    return <span className="ml-1">{dir === 'asc' ? '↑' : '↓'}</span>;
}

type TagStatusFilter = 'all' | 'fresh' | 'stale' | 'never' | 'alarm' | 'configured';

const TagsPage = () => {
    const { t: tr } = useTranslation();
    const queryClient = useQueryClient();
    const [searchParams] = useSearchParams();
    const gatewayIdParam = searchParams.get('gateway_id');
    const [selectedGatewayId, setSelectedGatewayId] = useState<string>(gatewayIdParam || 'all');
    const [searchInput, setSearchInput] = useState<string>('');
    const searchQuery = useDeferredValue(searchInput);
    const [filterDriverType, setFilterDriverType] = useState('all');
    // A status chip in the stats bar narrows the table to those tags.
    const [statusFilter, setStatusFilter] = useState<TagStatusFilter>('all');
    const [sortField, setSortField] = useState<'alias' | 'code' | 'data_type' | 'sort_order' | null>(null);
    const [sortDir, setSortDir] = useState<'asc' | 'desc'>('asc');
    const { selectedOrgId } = useNavigationStore();
    const { isAdmin, user } = useAuthStore();
    // Tag export: admins always, regular users with the export flag
    // (the backend enforces it too — this only shows the button).
    const canExport = isAdmin() || user?.permissions?.can_export_config === true;

    // Follow ?gateway_id= when it changes (a link from another page).
    // Adjusted during render rather than in an effect.
    const [seenGatewayParam, setSeenGatewayParam] = useState(gatewayIdParam);
    if (gatewayIdParam !== seenGatewayParam) {
        setSeenGatewayParam(gatewayIdParam);
        if (gatewayIdParam) setSelectedGatewayId(gatewayIdParam);
    }

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
    const [writeDialogTag, setWriteDialogTag] = useState<{ id: number; alias: string; dataType: string; currentValue: unknown } | null>(null);
    // Tag whose history the operator opened with the clock button (shared chart).
    const [historyTag, setHistoryTag] = useState<{ id: number; alias: string } | null>(null);
    const [writeValue, setWriteValue] = useState<string>('');
    const [writeLoading, setWriteLoading] = useState(false);

    const handleTagWrite = async () => {
        if (!writeDialogTag) return;
        setWriteLoading(true);
        try {
            let value: number | string | boolean = writeValue;
            if (writeDialogTag.dataType === 'BOOL') {
                value = writeValue === 'true';
            } else if (writeDialogTag.dataType === 'INT' || writeDialogTag.dataType === 'UINT' || writeDialogTag.dataType === 'DINT') {
                value = parseInt(writeValue, 10);
            } else if (writeDialogTag.dataType === 'REAL') {
                value = parseFloat(writeValue);
            }
            // parseInt("abc") is NaN, and NaN went to the PLC.
            if (typeof value === 'number' && Number.isNaN(value)) {
                toast.warning(i18n.t('tags.write_not_a_number', { type: writeDialogTag.dataType }));
                return;
            }
            await tagsApi.writeTag(writeDialogTag.id, { value });
            toast.success(i18n.t('tags.write_sent'));
            setWriteDialogTag(null);
        } catch (err) {
            // The write used to fail with nothing on screen: the operator saw
            // the dialog stay open and could not tell why.
            showApiError(err, i18n.t('tags.write_failed'));
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
        scaling_enabled: false,
        scaling_raw_min: 4,
        scaling_raw_max: 20,
        scaling_eu_min: 0,
        scaling_eu_max: 100,
        scaling_clamp: true,
        eu_unit: '',
        eu_decimals: 2,
        invert: false,
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

    // Batch Selection State
    const [selectedTagIds, setSelectedTagIds] = useState<number[]>([]);

    // OPC UA Browse State
    const [isBrowseOpen, setIsBrowseOpen] = useState(false);
    const [browseNodes, setBrowseNodes] = useState<OpcUaNode[]>([]);
    const [browsePath, setBrowsePath] = useState<{ nodeId: string; name: string }[]>([{ nodeId: 'i=84', name: 'Root' }]);
    const [isBrowsing, setIsBrowsing] = useState(false);
    const [browseError, setBrowseError] = useState<string | null>(null);

    // Export: Excel and CSV carry every column; the declaration text is what
    // TIA Portal and Codesys read back.
    const handleExport = async (format: 'xlsx' | 'csv' | 'txt') => {
        if (!selectedGatewayId || selectedGatewayId === 'all') return;
        const gatewayId = parseInt(selectedGatewayId);
        try {
            if (format === 'txt') {
                const content = await tagsApi.exportTags(gatewayId);
                saveBlob(new Blob([content], { type: 'text/plain' }), `tags_gateway_${selectedGatewayId}.txt`);
            } else {
                const lang = i18n.language?.startsWith('it') ? 'it' : 'en';
                const { blob, filename } = await tagsApi.downloadSheet(gatewayId, format, lang);
                saveBlob(blob, filename);
            }
        } catch (error) {
            showApiError(error, i18n.t('tags.export_failed'));
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
                (activeList || []).forEach((a) => { activeMap[a.tag_id] = true; });
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

    const handleInputChange = (field: keyof CreateTagDto, value: CreateTagDto[keyof CreateTagDto]) => {
        setFormData(prev => ({ ...prev, [field]: value }));
    };

    // Auto-generate the Modbus code from register type, address and bit.
    // Recomputed during render when one of them changes (null at first, so the
    // first render applies it, as the effect it replaces did on mount).
    const codeInputs = [modbusType, modbusAddress, modbusBit, formData.data_type, selectedGatewayDriverType] as const;
    const [prevCodeInputs, setPrevCodeInputs] = useState<typeof codeInputs | null>(null);
    if (prevCodeInputs === null || codeInputs.some((v, i) => v !== prevCodeInputs[i])) {
        setPrevCodeInputs(codeInputs);
        if (selectedGatewayDriverType === 'MODBUS_TCP') {
            const offset = { coil: 0, discrete: 10000, input: 30000, holding: 40000 }[modbusType] ?? 0;
            // Standard 5-digit format (e.g. 40001); address '1' is offset+1.
            let code = (offset + modbusAddress).toString().padStart(5, '0');
            // Bit offset for BOOL in registers
            if (formData.data_type === 'BOOL' && (modbusType === 'holding' || modbusType === 'input')) {
                code += `.${modbusBit}`;
            }
            if (formData.code !== code) handleInputChange('code', code);
        }
    }

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
        } catch (err: unknown) {
            setBrowseError(err instanceof Error ? err.message : tr('tagsPage.browse_failed'));
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
            gateway_id: parseInt(selectedGatewayId ?? ''),
        });
        setIsBrowseOpen(false);
        setUpdatingTagId(null);
        setIsOpen(true);
    };

    // Real-time value integration
    // Values read once per tag list, then overlaid with the live updates.
    // Derived rather than merged into state by an effect.
    const [fetchedValues, setFetchedValues] = useState<Map<number, CurrentValue>>(new Map());
    const { values: realtimeValues, connected: realtimeConnected } = useRealtime(selectedOrgId || undefined);

    // Data status detection hook (uses Sparkplug B BIRTH/DEATH)
    const { isDeviceOnline } = useStaleData();

    const currentValues = useMemo(() => {
        if (realtimeValues.size === 0) return fetchedValues;
        const next = new Map(fetchedValues);
        realtimeValues.forEach((val, id) => next.set(id, val));
        return next;
    }, [fetchedValues, realtimeValues]);
    // First values batch arrived: until then every value cell renders the
    // same skeleton, so rows never visibly jump when data lands.
    const valuesReady = fetchedValues.size > 0 || realtimeValues.size > 0;

    // One readout, two layouts: the desktop table cell and the mobile card
    // share it, so a value can never look different between phone and PC.
    // The value as the plant shows it: the tag's decimals when it is scaled,
    // whole numbers for integers, booleans as words.
    const formatTagValue = (t: Tag, cv?: CurrentValue): string => {
        if (!cv || cv.value === null || cv.value === undefined) return '—';
        if (typeof cv.value === 'boolean') return cv.value ? 'TRUE' : 'FALSE';
        if (typeof cv.value !== 'number') return String(cv.value);
        if (t.data_type === 'DINT' || t.data_type === 'INT') return Math.round(cv.value).toLocaleString(i18n.language);
        // The tag's decimals, always all of them: 2,20 under 2,15, not 2,2.
        const d = t.eu_decimals != null && t.eu_decimals >= 0 && t.eu_decimals <= 10 ? t.eu_decimals : null;
        return d !== null
            ? cv.value.toLocaleString(i18n.language, { minimumFractionDigits: d, maximumFractionDigits: d })
            : cv.value.toLocaleString(i18n.language, { maximumFractionDigits: 2 });
    };

    // One state per tag, the most urgent first: in alarm, its device offline,
    // a value that is not current, good, or nothing heard yet. ISA-101: colour
    // only where something needs attention.
    type TagState = 'alarm' | 'offline' | 'stale' | 'ok' | 'never';
    const tagState = (t: Tag, cv?: CurrentValue): TagState => {
        if (activeAlarms[t.id]) return 'alarm';
        if (!cv) return 'never';
        const online = isDeviceOnline(gatewayMap.get(t.gateway_id)?.name);
        if (online === false) return 'offline';
        return getDataStatus(cv.quality, online) === 'good' ? 'ok' : 'stale';
    };

    const StateBadge = ({ state }: { state: TagState }) => {
        if (state === 'alarm') return <Badge variant="alarm"><BellRing />{tr('tagsPage.state_alarm')}</Badge>;
        if (state === 'offline') return <Badge variant="alarm"><WifiOff />{tr('tagsPage.state_offline')}</Badge>;
        if (state === 'stale') return <Badge variant="warning"><Clock />{tr('tagsPage.state_stale')}</Badge>;
        // Normal is quiet: a dot and a word, no box.
        if (state === 'ok') return <span className="inline-flex items-center gap-1.5 text-xs text-muted-foreground"><span className="size-2 rounded-full bg-emerald-500" />{tr('tagsPage.state_ok')}</span>;
        return <Badge variant="muted">{tr('tagsPage.state_never')}</Badge>;
    };

    // Value and unit, figures aligned; a skeleton until the first values land.
    const ValueText = ({ tag: t, currentValue: cv, className = '' }: { tag: Tag; currentValue?: CurrentValue; className?: string }) => {
        if (!valuesReady) return <span className="inline-block h-4 w-16 rounded bg-muted animate-pulse align-middle" />;
        const dim = tagState(t, cv) !== 'ok' && tagState(t, cv) !== 'alarm';
        return (
            <span className={`tabular-nums whitespace-nowrap ${dim ? 'text-muted-foreground' : ''} ${className}`}>
                <span className="font-semibold">{formatTagValue(t, cv)}</span>
                {t.eu_unit && cv && <span className="ml-1 text-xs font-normal text-muted-foreground">{t.eu_unit}</span>}
            </span>
        );
    };

    const openWrite = (t: Tag) => {
        const cv = currentValues.get(t.id);
        setWriteValue(t.data_type === 'BOOL'
            ? (cv?.value === true ? 'false' : 'true')
            : (cv?.value != null ? String(cv.value) : '0'));
        setWriteDialogTag({ id: t.id, alias: t.alias || '', dataType: t.data_type, currentValue: cv?.value });
    };

    const absoluteTime = (ms: number) =>
        new Date(ms).toLocaleString(i18n.language, { hour12: false, day: '2-digit', month: '2-digit', year: 'numeric', hour: '2-digit', minute: '2-digit', second: '2-digit' });

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
                    toast.warning(i18n.t('tags.pick_gateway'));
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
                scaling_enabled: false,
                scaling_raw_min: 4,
                scaling_raw_max: 20,
                scaling_eu_min: 0,
                scaling_eu_max: 100,
                scaling_clamp: true,
                eu_unit: '',
                eu_decimals: 2,
                invert: false,
            });
        } catch (error) {
            console.error('Failed to save tag', error);
        }
    };

    const handleEdit = (tag: Tag) => {
        setUpdatingTagId(tag.id);
        setFormData({
            code: tag.code,
            alias: tag.alias,
            data_type: tag.data_type,
            historize: tag.historize,
            deadband_value: tag.historize_deadband || 0.1,
            json_path: tag.json_path || '',
            scaling_enabled: tag.scaling_enabled ?? false,
            scaling_raw_min: tag.scaling_raw_min ?? 4,
            scaling_raw_max: tag.scaling_raw_max ?? 20,
            scaling_eu_min: tag.scaling_eu_min ?? 0,
            scaling_eu_max: tag.scaling_eu_max ?? 100,
            scaling_clamp: tag.scaling_clamp ?? true,
            eu_unit: tag.eu_unit ?? '',
            eu_decimals: tag.eu_decimals ?? 2,
            invert: tag.invert ?? false,
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
    // Suggest the next free address when the register type (or the dialog,
    // or the tag list) changes. Adjusted during render rather than in an effect.
    const addrInputs = [modbusType, isOpen, updatingTagId, selectedGatewayDriverType, tags] as const;
    const [prevAddrInputs, setPrevAddrInputs] = useState(addrInputs);
    if (addrInputs.some((v, i) => v !== prevAddrInputs[i])) {
        setPrevAddrInputs(addrInputs);
        if (isOpen && !updatingTagId && selectedGatewayDriverType === 'MODBUS_TCP') {
            setModbusAddress(calculateNextAddress(modbusType));
        }
    }

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
            scaling_enabled: false,
            scaling_raw_min: 4,
            scaling_raw_max: 20,
            scaling_eu_min: 0,
            scaling_eu_max: 100,
            scaling_clamp: true,
            eu_unit: '',
            eu_decimals: 2,
            invert: false,
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
                        return tr('tagsPage.overlap_full', { alias: tag.alias, code: tag.code });
                    }
                    // Else, same register different bits is OK
                    continue;
                }

                return tr('tagsPage.overlap_partial', { alias: tag.alias, code: tag.code });
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
                if (!(await confirmAction({ title: i18n.t('tags.overlap_title'), description: overlapError, confirmLabel: i18n.t('tags.overlap_proceed') }))) {
                    return;
                }
            }
        }
        await handleSave();
    };

    const handleDelete = async (e: React.MouseEvent, id: number) => {
        e.stopPropagation();
        if (await confirmAction({ title: i18n.t('ask.delete_tag'), description: i18n.t('ask.delete_tag_desc'), destructive: true })) {
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
        if (!(await confirmAction({ title: i18n.t('ask.delete_tags', { count: selectedTagIds.length }), description: i18n.t('ask.delete_tag_desc'), destructive: true }))) return;

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
            // Refetch the list rather than reload the page, which threw away
            // the scroll position, the filters and any open dialog.
            await queryClient.invalidateQueries({ queryKey: ['tags'] });
        } catch (error) {
            console.error("Failed to reorder tags", error);
            toast.error(i18n.t('tags.reorder_failed'));
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
            await queryClient.invalidateQueries({ queryKey: ['tags'] });
        } catch (error) {
            console.error("Failed to reorder tag", error);
            toast.error(i18n.t('tags.reorder_failed'));
        }
    };

    // Initial values will come through the WebSocket (useRealtime)
    // Also fetch initial values on page load so we don't show "-" until first change
    useEffect(() => {
        const fetchInitialValues = async () => {
            if (!tags || tags.length === 0) return;

            const newValues = new Map<number, CurrentValue>();

            // One batch call for the whole organization instead of one
            // request per tag: hundreds of parallel /current calls got
            // rate-limited and most rows stayed empty.
            try {
                const shadows = await tagsApi.getShadowBatch();
                shadows.forEach((s) => {
                    newValues.set(s.tag_id, { value: s.value, timestamp: s.ts, quality: s.quality });
                });
            } catch (err) {
                // Fall back to per-tag fetch (slow) so old servers still work.
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
            }

            if (newValues.size > 0) {
                setFetchedValues(prev => {
                    const merged = new Map(prev);
                    newValues.forEach((v, k) => merged.set(k, v));
                    return merged;
                });
            }
        };

        fetchInitialValues();
    }, [tags]);

    // (Active alarms are already fetched in the other useEffect hook 'fetchAlarmData' at the top of the file)

    // Clear the values read for the previous gateway when it changes.
    const [valuesForGateway, setValuesForGateway] = useState(selectedGatewayId);
    if (valuesForGateway !== selectedGatewayId) {
        setValuesForGateway(selectedGatewayId);
        setFetchedValues(new Map());
    }

    // The React Compiler cannot preserve this memo inside a component this
    // size (it reports so, not a wrong dependency), and it is not enabled in
    // the build anyway. Without the memo every live value update would
    // re-filter and re-sort the whole tag list.
    // eslint-disable-next-line react-hooks/preserve-manual-memoization
    const filteredTags = useMemo(
        () => filterAndSortTags(tags, gateways, filterDriverType, searchQuery, sortField, sortDir),
        [tags, searchQuery, filterDriverType, gateways, sortField, sortDir],
    );
    // What each tag's current value says: valid now, not valid (bad quality,
    // or marked stale because nothing arrived within the gateway's timeout),
    // or nothing received yet. "Good quality" alone lumped the last two
    // together and read as a plant mostly broken.
    const statusById = useMemo(() => {
        const m = new Map<number, 'fresh' | 'stale' | 'never'>();
        for (const t of filteredTags) {
            const v = currentValues.get(t.id);
            m.set(t.id, !v ? 'stale' : v.quality === 0 ? 'fresh' : 'stale');
        }
        return m;
    }, [filteredTags, currentValues]);
    const statusCounts = useMemo(() => {
        const c = { fresh: 0, stale: 0, never: 0, alarm: 0, configured: 0 };
        for (const t of filteredTags) {
            c[statusById.get(t.id) ?? 'never']++;
            if (activeAlarms[t.id]) c.alarm++;
            if (alarmDefsCount[t.id] > 0) c.configured++;
        }
        return c;
    }, [filteredTags, statusById, activeAlarms, alarmDefsCount]);
    // A plain filter, cheap next to the sort above it.
    const tagsList = statusFilter === 'all' ? filteredTags : filteredTags.filter(t =>
        statusFilter === 'alarm' ? !!activeAlarms[t.id]
            : statusFilter === 'configured' ? alarmDefsCount[t.id] > 0
                : statusById.get(t.id) === statusFilter);

    if (isLoading) {
        return <div className="p-8 text-center text-muted-foreground">{tr('tagsPage.loading')}</div>;
    }

    return (
        <div className="space-y-6">
            {/* Title above the toolbar, not beside it: side by side, six
                controls squeezed the description to one word per line. */}
            <div className="flex flex-col gap-3">
                <div>
                    <h2 className="text-2xl font-semibold tracking-tight">{tr('tagsPage.title')}</h2>
                    <p className="text-sm text-muted-foreground">
                        {tr('tagsPage.subtitle')}
                    </p>
                </div>

                <div className="flex flex-wrap items-center gap-2">
                    <div className="relative w-full sm:w-72">
                        <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
                        <Input
                            placeholder={tr('tagsPage.search_placeholder')}
                            value={searchInput}
                            onChange={(e) => setSearchInput(e.target.value)}
                            className="w-full pl-9"
                        />
                    </div>

                    <div className="w-[calc(50%-0.25rem)] sm:w-56">
                        <Select
                            value={selectedGatewayId}
                            onValueChange={setSelectedGatewayId}
                        >
                            <SelectTrigger>
                                <SelectValue placeholder={tr('tagsPage.filter_gateway')} />
                            </SelectTrigger>
                            <SelectContent>
                                <SelectItem value="all">{tr('tagsPage.all_gateways')}</SelectItem>
                                {gateways.map((gw) => (
                                    <SelectItem key={gw.id} value={gw.id.toString()}>
                                        {gw.name}
                                    </SelectItem>
                                ))}
                            </SelectContent>
                        </Select>
                    </div>

                    <div className="w-[calc(50%-0.25rem)] sm:w-44">
                        <Select value={filterDriverType} onValueChange={setFilterDriverType}>
                            <SelectTrigger>
                                <SelectValue placeholder={tr('tagsPage.all_drivers')} />
                            </SelectTrigger>
                            <SelectContent>
                                <SelectItem value="all">{tr('tagsPage.all_drivers')}</SelectItem>
                                <SelectItem value="S7">Siemens S7</SelectItem>
                                <SelectItem value="MODBUS_TCP">Modbus TCP</SelectItem>
                                <SelectItem value="OPC_UA">OPC-UA</SelectItem>
                                <SelectItem value="MQTT">MQTT</SelectItem>
                                <SelectItem value="LORAWAN">LoRaWAN</SelectItem>
                            </SelectContent>
                        </Select>
                    </div>

                    {/* Actions to the right on a wide screen; on a phone they
                        wrap under the filters, labels kept to icons. */}
                    <div className="hidden lg:block flex-1" />
                    {canExport && (
                        <>
                            {/* Export: a small menu of formats */}
                            <Popover>
                                <PopoverTrigger asChild>
                                    <Button
                                        variant="outline"
                                        className="gap-2"
                                        disabled={!selectedGatewayId || selectedGatewayId === 'all'}
                                        title={!selectedGatewayId || selectedGatewayId === 'all' ? tr('tagsPage.pick_gateway_first') : undefined}
                                    >
                                        <Download size={16} /> <span className="hidden sm:inline">{tr('common.export')}</span>
                                    </Button>
                                </PopoverTrigger>
                                <PopoverContent align="end" className="w-56 p-1">
                                    {([
                                        ['xlsx', 'tagsPage.export_xlsx'],
                                        ['csv', 'tagsPage.export_csv'],
                                        ['txt', 'tagsPage.export_txt'],
                                    ] as const).map(([format, key]) => (
                                        <button
                                            key={format}
                                            type="button"
                                            onClick={() => void handleExport(format)}
                                            className="w-full text-left text-sm px-3 py-2 rounded hover:bg-muted"
                                        >
                                            {tr(key)}
                                        </button>
                                    ))}
                                </PopoverContent>
                            </Popover>

                            <Button
                                variant="outline"
                                className="gap-2"
                                onClick={() => setIsImportOpen(true)}
                                disabled={!selectedGatewayId || selectedGatewayId === 'all'}
                                title={!selectedGatewayId || selectedGatewayId === 'all' ? tr('tagsPage.pick_gateway_first') : undefined}
                            >
                                <Upload size={16} /> <span className="hidden sm:inline">{tr('common.import')}</span>
                            </Button>
                            {selectedGatewayId && selectedGatewayId !== 'all' && (
                                <TagImportDialog
                                    open={isImportOpen}
                                    onOpenChange={setIsImportOpen}
                                    gatewayId={parseInt(selectedGatewayId)}
                                    driverType={selectedGatewayDriverType ?? undefined}
                                />
                            )}

                            {/* OPC UA Browse Button */}
                            {selectedGatewayDriverType === 'OPC_UA' && (
                                <Button
                                    variant="outline"
                                    className="gap-2 border-[#c8e600]/50 text-[#4a5500] dark:text-[#c8e600] hover:bg-[#c8e600]/10"
                                    onClick={handleBrowseOpen}
                                    disabled={!selectedGatewayId || selectedGatewayId === 'all'}
                                >
                                    <Database size={16} /> {tr('tagsPage.browse_server')}
                                </Button>
                            )}

                            {/* Add Tag Dialog */}
                            <Dialog open={isOpen} onOpenChange={setIsOpen}>
                                <DialogTrigger asChild disabled={!selectedGatewayId || selectedGatewayId === 'all'}>
                                    <Button className="gap-2" onClick={handleCreateOpen}
                                        title={!selectedGatewayId || selectedGatewayId === 'all' ? tr('tagsPage.pick_gateway_first') : undefined}>
                                        <Plus size={16} /> {tr('tagsPage.add')}
                                    </Button>
                                </DialogTrigger>
                                <DialogContent className="max-w-2xl">
                                    <DialogHeader>
                                        <DialogTitle>{updatingTagId ? tr('tagsPage.edit_title') : tr('tagsPage.create_title')}</DialogTitle>
                                        <DialogDescription>
                                            {updatingTagId ? tr('tagsPage.edit_desc') : tr('tagsPage.create_desc')}
                                        </DialogDescription>
                                    </DialogHeader>
                                    <Tabs defaultValue="general" className="w-full mt-4">
                                        <TabsList className="mb-4">
                                            <TabsTrigger value="general">{tr('tagsPage.tab_general')}</TabsTrigger>
                                            <TabsTrigger value="scaling">{tr('tagsPage.tab_scaling')}</TabsTrigger>
                                            {updatingTagId && <TabsTrigger value="alarms">{tr('tagsPage.tab_alarms')}</TabsTrigger>}
                                        </TabsList>
                                        <TabsContent value="general">
                                            <div className="grid gap-6 py-4">
                                                <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                                                    {/* Modbus Address Builder */}
                                                    {selectedGatewayDriverType === 'MODBUS_TCP' && (
                                                        <div className="col-span-2 space-y-4 p-4 bg-muted/50 rounded-md border">
                                                            <h3 className="text-sm font-medium">{tr('tagsPage.mb_builder')}</h3>
                                                            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                                                                <div className="grid gap-2">
                                                                    <Label htmlFor="mb-type">{tr('tagsPage.mb_register_type')}</Label>
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
                                                                    <Label htmlFor="mb-addr">{tr('tagsPage.mb_address')}</Label>
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
                                                                        <Label htmlFor="mb-bit">{tr('tagsPage.mb_bit')}</Label>
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
                                                            {selectedGatewayDriverType === 'MQTT' ? tr('tagsPage.code_mqtt') : selectedGatewayDriverType === 'LORAWAN' ? tr('tagsPage.code_lorawan') : tr('tagsPage.code')}
                                                        </Label>
                                                        <Input
                                                            id="code"
                                                            value={formData.code}
                                                            onChange={(e) => handleInputChange('code', e.target.value)}
                                                            placeholder={
                                                                selectedGatewayDriverType === 'MQTT' ? tr('tagsPage.code_mqtt_placeholder') :
                                                                selectedGatewayDriverType === 'LORAWAN' ? tr('tagsPage.code_lorawan_placeholder') :
                                                                tr('tagsPage.code_placeholder')
                                                            }
                                                        />
                                                        {selectedGatewayDriverType === 'MODBUS_TCP' && (
                                                            <p className="text-[10px] text-muted-foreground">{tr('tagsPage.code_modbus_help')}</p>
                                                        )}
                                                        {selectedGatewayDriverType === 'MQTT' && (
                                                            <p className="text-[10px] text-emerald-600">{tr('tagsPage.code_mqtt_help')}</p>
                                                        )}
                                                        {selectedGatewayDriverType === 'LORAWAN' && (
                                                            <p className="text-[10px] text-violet-600">{tr('tagsPage.lorawan_help_format')} <code>device_id/field</code> — {tr('tagsPage.eg')} <code>sensor-01/temperature</code>. {tr('tagsPage.lorawan_help_special')} rssi, snr, f_port. {tr('tagsPage.lorawan_help_wildcard')} <code>*/temperature</code></p>
                                                        )}
                                                    </div>
                                                    <div className="grid gap-2">
                                                        <Label htmlFor="alias">{tr('tagsPage.alias_label')}</Label>
                                                        <Input
                                                            id="alias"
                                                            value={formData.alias}
                                                            onChange={(e) => handleInputChange('alias', e.target.value)}
                                                            placeholder={tr('tagsPage.alias_placeholder')}
                                                        />
                                                    </div>
                                                </div>

                                                <div className="grid gap-2">
                                                    <Label htmlFor="type">{tr('tagsPage.data_type')}</Label>
                                                    <Select
                                                        value={formData.data_type}
                                                        onValueChange={(val) => {
                                                            handleInputChange('data_type', val);
                                                            if (val !== 'BOOL') setModbusBit(0);
                                                        }}
                                                    >
                                                        <SelectTrigger>
                                                            <SelectValue placeholder={tr('tagsPage.data_type_placeholder')} />
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

                                                <div className="grid grid-cols-1 sm:grid-cols-2 gap-6 border-t pt-4">
                                                    <div className="space-y-4">
                                                        <div className="flex items-center space-x-2">
                                                            <Switch
                                                                id="historize"
                                                                checked={formData.historize}
                                                                onCheckedChange={(checked) => handleInputChange('historize', checked)}
                                                            />
                                                            <Label htmlFor="historize" className="flex items-center gap-2">
                                                                <Database size={14} /> {tr('tagsPage.historize')}
                                                            </Label>
                                                        </div>

                                                        {formData.historize && (
                                                            <div className="grid gap-2 pl-6 border-l-2">
                                                                <Label htmlFor="deadband">{tr('tagsPage.deadband')}</Label>
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
                                                            JSON path <span className="text-[10px] font-normal text-muted-foreground">{tr('tagsPage.optional')}</span>
                                                        </Label>
                                                        <Input
                                                            id="json_path"
                                                            value={formData.json_path || ''}
                                                            onChange={(e) => handleInputChange('json_path', e.target.value)}
                                                            placeholder={tr('tagsPage.json_path_placeholder')}
                                                            className="font-mono text-sm"
                                                        />
                                                        <p className="text-[10px] text-muted-foreground">
                                                            {tr('tagsPage.json_path_help')}
                                                        </p>
                                                    </div>
                                                    )}
                                                </div>
                                            </div>
                                            <DialogFooter>
                                                <Button onClick={handleSaveWithValidation}>{updatingTagId ? tr('tagsPage.update_submit') : tr('tagsPage.create_title')}</Button>
                                            </DialogFooter>
                                        </TabsContent>

                                        <TabsContent value="scaling">
                                            <div className="space-y-6 py-2">
                                                {/* Enable toggle */}
                                                <div className="flex items-center justify-between p-4 rounded-lg border bg-muted/30">
                                                    <div>
                                                        <p className="font-medium text-sm">{tr('tagsPage.scaling_enabled')}</p>
                                                        <p className="text-xs text-muted-foreground mt-0.5">
                                                            {tr('tagsPage.scaling_enabled_help')}
                                                        </p>
                                                    </div>
                                                    <Switch
                                                        checked={formData.scaling_enabled ?? false}
                                                        onCheckedChange={v => handleInputChange('scaling_enabled', v)}
                                                    />
                                                </div>

                                                {formData.scaling_enabled && (
                                                    <>
                                                        {/* Bool tag: only show invert */}
                                                        {formData.data_type === 'BOOL' ? (
                                                            <div className="space-y-4">
                                                                <div className="flex items-center justify-between p-4 rounded-lg border">
                                                                    <div>
                                                                        <p className="font-medium text-sm">{tr('tagsPage.invert')}</p>
                                                                        <p className="text-xs text-muted-foreground mt-0.5">
                                                                            true → false, false → true
                                                                        </p>
                                                                    </div>
                                                                    <Switch
                                                                        checked={formData.invert ?? false}
                                                                        onCheckedChange={v => handleInputChange('invert', v)}
                                                                    />
                                                                </div>
                                                            </div>
                                                        ) : (
                                                            <div className="space-y-5">
                                                                {/* Raw range */}
                                                                <div className="space-y-3">
                                                                    <h4 className="text-sm font-semibold text-muted-foreground">{tr('tagsPage.raw_range')}</h4>
                                                                    <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                                                                        <div className="grid gap-2">
                                                                            <Label htmlFor="sc-raw-min">{tr('tagsPage.raw_min')}</Label>
                                                                            <Input
                                                                                id="sc-raw-min"
                                                                                type="number"
                                                                                step="any"
                                                                                value={formData.scaling_raw_min ?? 4}
                                                                                onChange={e => handleInputChange('scaling_raw_min', parseFloat(e.target.value) || 0)}
                                                                                placeholder={tr('tagsPage.eg_value', { value: 4 })}
                                                                            />
                                                                            <p className="text-xs text-muted-foreground">{tr('tagsPage.raw_min_help')}</p>
                                                                        </div>
                                                                        <div className="grid gap-2">
                                                                            <Label htmlFor="sc-raw-max">{tr('tagsPage.raw_max')}</Label>
                                                                            <Input
                                                                                id="sc-raw-max"
                                                                                type="number"
                                                                                step="any"
                                                                                value={formData.scaling_raw_max ?? 20}
                                                                                onChange={e => handleInputChange('scaling_raw_max', parseFloat(e.target.value) || 0)}
                                                                                placeholder={tr('tagsPage.eg_value', { value: 20 })}
                                                                            />
                                                                            <p className="text-xs text-muted-foreground">{tr('tagsPage.raw_max_help')}</p>
                                                                        </div>
                                                                    </div>
                                                                </div>

                                                                {/* EU range */}
                                                                <div className="space-y-3">
                                                                    <h4 className="text-sm font-semibold text-muted-foreground">{tr('tagsPage.eu_range')}</h4>
                                                                    <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                                                                        <div className="grid gap-2">
                                                                            <Label htmlFor="sc-eu-min">EU Min</Label>
                                                                            <Input
                                                                                id="sc-eu-min"
                                                                                type="number"
                                                                                step="any"
                                                                                value={formData.scaling_eu_min ?? 0}
                                                                                onChange={e => handleInputChange('scaling_eu_min', parseFloat(e.target.value) || 0)}
                                                                                placeholder={tr('tagsPage.eg_value', { value: 0 })}
                                                                            />
                                                                        </div>
                                                                        <div className="grid gap-2">
                                                                            <Label htmlFor="sc-eu-max">EU Max</Label>
                                                                            <Input
                                                                                id="sc-eu-max"
                                                                                type="number"
                                                                                step="any"
                                                                                value={formData.scaling_eu_max ?? 100}
                                                                                onChange={e => handleInputChange('scaling_eu_max', parseFloat(e.target.value) || 0)}
                                                                                placeholder={tr('tagsPage.eg_value', { value: 100 })}
                                                                            />
                                                                        </div>
                                                                    </div>
                                                                </div>

                                                                {/* Unit + decimals */}
                                                                <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                                                                    <div className="grid gap-2">
                                                                        <Label htmlFor="sc-unit">{tr('tagsPage.eu_unit')}</Label>
                                                                        <Input
                                                                            id="sc-unit"
                                                                            value={formData.eu_unit ?? ''}
                                                                            onChange={e => handleInputChange('eu_unit', e.target.value)}
                                                                            placeholder={tr('tagsPage.eu_unit_placeholder')}
                                                                        />
                                                                    </div>
                                                                    <div className="grid gap-2">
                                                                        <Label htmlFor="sc-dec">{tr('tagsPage.decimals')}</Label>
                                                                        <Input
                                                                            id="sc-dec"
                                                                            type="number"
                                                                            min={0}
                                                                            max={6}
                                                                            value={formData.eu_decimals ?? 2}
                                                                            onChange={e => handleInputChange('eu_decimals', parseInt(e.target.value) || 0)}
                                                                        />
                                                                    </div>
                                                                </div>

                                                                {/* Clamp toggle */}
                                                                <div className="flex items-center justify-between p-3 rounded-lg border">
                                                                    <div>
                                                                        <p className="font-medium text-sm">{tr('tagsPage.clamp')}</p>
                                                                        <p className="text-xs text-muted-foreground mt-0.5">
                                                                            {tr('tagsPage.clamp_help')}
                                                                        </p>
                                                                    </div>
                                                                    <Switch
                                                                        checked={formData.scaling_clamp ?? true}
                                                                        onCheckedChange={v => handleInputChange('scaling_clamp', v)}
                                                                    />
                                                                </div>

                                                                {/* Live formula preview */}
                                                                <div className="p-4 rounded-lg bg-muted/50 border font-mono text-sm space-y-1">
                                                                    <p className="text-muted-foreground text-xs font-sans font-semibold mb-2">{tr('tagsPage.formula_preview')}</p>
                                                                    {(() => {
                                                                        const rawMin = formData.scaling_raw_min ?? 4;
                                                                        const rawMax = formData.scaling_raw_max ?? 20;
                                                                        const euMin = formData.scaling_eu_min ?? 0;
                                                                        const euMax = formData.scaling_eu_max ?? 100;
                                                                        const unit = formData.eu_unit || 'EU';
                                                                        const dec = formData.eu_decimals ?? 2;
                                                                        const span = rawMax - rawMin;
                                                                        const convert = (r: number) =>
                                                                            span === 0 ? euMin : ((r - rawMin) / span) * (euMax - euMin) + euMin;
                                                                        const mid = rawMin + (rawMax - rawMin) / 2;
                                                                        return (
                                                                            <div className="space-y-1 text-xs">
                                                                                <div className="flex justify-between"><span className="text-muted-foreground">Raw {rawMin}</span><span className="text-emerald-400">→ {convert(rawMin).toFixed(dec)} {unit}</span></div>
                                                                                <div className="flex justify-between"><span className="text-muted-foreground">Raw {mid.toFixed(1)}</span><span className="text-emerald-400">→ {convert(mid).toFixed(dec)} {unit}</span></div>
                                                                                <div className="flex justify-between"><span className="text-muted-foreground">Raw {rawMax}</span><span className="text-emerald-400">→ {convert(rawMax).toFixed(dec)} {unit}</span></div>
                                                                            </div>
                                                                        );
                                                                    })()}
                                                                </div>
                                                            </div>
                                                        )}
                                                    </>
                                                )}

                                                {!formData.scaling_enabled && (
                                                    <div className="text-center py-8 text-muted-foreground text-sm">
                                                        <p>{tr('tagsPage.scaling_off')}</p>
                                                        <p className="mt-1 text-xs">{tr('tagsPage.scaling_off_example')}</p>
                                                    </div>
                                                )}
                                            </div>
                                            {/* The conversion is part of the tag: it is saved here too,
                                                not only from the first tab. */}
                                            <DialogFooter>
                                                <Button onClick={handleSaveWithValidation}>{updatingTagId ? tr('tagsPage.update_submit') : tr('tagsPage.create_title')}</Button>
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
                        <span className="text-sm font-medium px-2">{tr('tagsPage.selected', { count: selectedTagIds.length })}</span>
                        <div className="h-4 w-px bg-border mx-2" />
                        <Button variant="ghost" size="sm" onClick={handleBatchDelete} className="gap-1 hover:bg-red-100 text-red-600 hover:text-red-700">
                            <Trash2 size={16} /> {tr('tagsPage.delete_selected')}
                        </Button>
                        <div className="flex-1" />
                        <Button variant="ghost" size="sm" onClick={() => setSelectedTagIds([])} className="gap-1 hover:bg-[#c8e600]/20 text-[#4a5500] dark:text-[#c8e600]">
                            <X size={16} /> {tr('common.cancel')}
                        </Button>
                    </div>
                )
            }

            {/* Status filters: what the current values say, each a chip with its
                count; the one pressed filters the list. */}
            <div className="-mx-4 flex items-center gap-2 overflow-x-auto px-4 pb-1 sm:mx-0 sm:flex-wrap sm:overflow-visible sm:px-0 sm:pb-0 [scrollbar-width:none]">
                {([
                    ['all', tagsList.length === filteredTags.length ? filteredTags.length : filteredTags.length, ''],
                    ['fresh', statusCounts.fresh, 'bg-emerald-500'],
                    ['stale', statusCounts.stale, 'bg-amber-500'],
                    ['alarm', statusCounts.alarm, 'bg-red-500'],
                    ['configured', statusCounts.configured, ''],
                ] as const).map(([key, n, dot]) => {
                    const on = statusFilter === key;
                    return (
                        <button key={key} type="button"
                            onClick={() => setStatusFilter(key === 'all' ? 'all' : (f => (f === key ? 'all' : key)))}
                            title={key === 'all' ? undefined : tr(`tagsPage.stat_${key}_hint`)}
                            aria-pressed={on}
                            className={`inline-flex h-8 shrink-0 items-center gap-2 whitespace-nowrap rounded-full border px-3 text-sm transition-colors ${on
                                ? 'border-foreground/20 bg-foreground text-background'
                                : 'bg-card text-muted-foreground hover:bg-muted hover:text-foreground'}`}>
                            {dot && <span className={`size-2 rounded-full ${dot}`} />}
                            {key === 'all' ? tr('tagsPage.filter_all') : tr(`tagsPage.chip_${key}`)}
                            <span className={`tabular-nums font-semibold ${on ? '' : 'text-foreground'}`}>{n}</span>
                        </button>
                    );
                })}
                {(searchInput || selectedGatewayId !== 'all' || filterDriverType !== 'all' || statusFilter !== 'all') && (
                    <Button variant="ghost" size="sm" className="h-8 text-xs" onClick={() => { setSearchInput(''); setSelectedGatewayId('all'); setFilterDriverType('all'); setStatusFilter('all'); }}>
                        <X size={14} /> {tr('tagsPage.clear_filters')}
                    </Button>
                )}
            </div>
            {!realtimeConnected && (
                // Without this the table kept rendering the last values as good
                // after the socket dropped: a healthy plant on screen while every
                // number was minutes or hours old.
                <div className="flex items-center gap-2 rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-800 dark:border-amber-900 dark:bg-amber-950/50 dark:text-amber-300">
                    <WifiOff size={16} className="shrink-0" />
                    {tr('tagsPage.live_disconnected')}
                </div>
            )}

            <div className="hidden md:block overflow-hidden rounded-xl border bg-card">
                <Table className="table-fixed w-full">
                    <TableHeader className="bg-muted/40">
                        <TableRow className="hover:bg-transparent">
                            {isAdmin() && (
                                <TableHead className="w-[44px] pl-3">
                                    <Button variant="ghost" size="icon" className="size-7" onClick={handleSelectAll} aria-label={tr('tagsPage.select_all')}>
                                        {selectedTagIds.length === tagsList.length && tagsList.length > 0
                                            ? <CheckSquare size={16} className="text-foreground" />
                                            : <Square size={16} className="text-muted-foreground/60" />}
                                    </Button>
                                </TableHead>
                            )}
                            <TableHead className="cursor-pointer select-none" onClick={() => handleSort('alias')}>
                                {tr('tagsPage.col_tag')} <SortIcon field="alias" current={sortField} dir={sortDir} />
                            </TableHead>
                            <TableHead className="w-[140px] text-right">{tr('tagsPage.col_value')}</TableHead>
                            <TableHead className="w-[150px]">{tr('tagsPage.col_state')}</TableHead>
                            <TableHead className="hidden lg:table-cell w-[110px]">{tr('tagsPage.col_updated')}</TableHead>
                            <TableHead className="hidden xl:table-cell w-[120px]">{tr('tagsPage.col_alarms')}</TableHead>
                            <TableHead className={`text-right ${isAdmin() ? 'w-[164px]' : 'w-[64px]'}`}><span className="sr-only">{tr('common.actions')}</span></TableHead>
                        </TableRow>
                    </TableHeader>
                    <TableBody>
                        {tagsList.length === 0 ? (
                            <TableRow className="hover:bg-transparent">
                                <TableCell colSpan={isAdmin() ? 7 : 6}>
                                    <EmptyState
                                        icon={TagsIcon}
                                        title={tr('empty.tags_title')}
                                        description={selectedGatewayId && selectedGatewayId !== 'all' ? tr('empty.tags_desc') : tr('empty.tags_pick_gateway')}
                                    />
                                </TableCell>
                            </TableRow>
                        ) : (
                            tagsList.map((tag) => {
                                const cv = currentValues.get(tag.id);
                                const state = tagState(tag, cv);
                                const selected = selectedTagIds.includes(tag.id);
                                const gwName = gatewayMap.get(tag.gateway_id)?.name;
                                return (
                                    <TableRow key={tag.id} className={`group ${selected ? 'bg-muted/60' : ''} ${state === 'alarm' ? 'shadow-[inset_3px_0_0_0_rgb(239_68_68)]' : ''}`}>
                                        {isAdmin() && (
                                            <TableCell className="pl-3">
                                                <div className="flex items-center gap-0.5">
                                                    <Button variant="ghost" size="icon" className="size-7"
                                                        onClick={(e) => { e.stopPropagation(); toggleSelect(tag.id); }}
                                                        aria-label={tr('tagsPage.select')}>
                                                        {selected ? <CheckSquare size={16} className="text-foreground" /> : <Square size={16} className="text-muted-foreground/60" />}
                                                    </Button>
                                                    {selected && (
                                                        <div className="flex flex-col animate-in fade-in zoom-in duration-200">
                                                            <Button variant="ghost" size="icon" className="h-4 w-5 p-0" onClick={(e) => { e.stopPropagation(); handleMoveSingle(tag.id, 'up'); }} aria-label={tr('tagsPage.move_up')}><ArrowUp size={12} /></Button>
                                                            <Button variant="ghost" size="icon" className="h-4 w-5 p-0" onClick={(e) => { e.stopPropagation(); handleMoveSingle(tag.id, 'down'); }} aria-label={tr('tagsPage.move_down')}><ArrowDown size={12} /></Button>
                                                        </div>
                                                    )}
                                                </div>
                                            </TableCell>
                                        )}
                                        <TableCell>
                                            <div className="min-w-0">
                                                <div className="truncate font-medium" title={tag.code}>{tag.alias || tag.code || '-'}</div>
                                                <div className="flex min-w-0 items-center gap-1.5 text-xs text-muted-foreground">
                                                    {gwName && <span className="truncate">{gwName}</span>}
                                                    {gwName && <span aria-hidden>·</span>}
                                                    <span className="shrink-0">{tag.data_type}</span>
                                                    {tag.historize && (
                                                        <span className="inline-flex shrink-0 items-center" title={(tag.historize_deadband ?? tag.deadband_value) != null
                                                            ? tr('tagsPage.history_on', { deadband: tag.historize_deadband ?? tag.deadband_value })
                                                            : tr('tagsPage.history_on_plain')}>
                                                            <Database size={11} className="ml-0.5" />
                                                        </span>
                                                    )}
                                                </div>
                                            </div>
                                        </TableCell>
                                        <TableCell className="text-right text-[15px]">
                                            <ValueText tag={tag} currentValue={cv} />
                                        </TableCell>
                                        <TableCell>{valuesReady || state === 'alarm' ? <StateBadge state={state} /> : null}</TableCell>
                                        <TableCell className="hidden lg:table-cell text-sm text-muted-foreground">
                                            {cv?.timestamp ? <span title={absoluteTime(cv.timestamp)} className={state === 'offline' || state === 'stale' ? 'text-amber-700 dark:text-amber-400' : ''}>{timeAgo(cv.timestamp, i18n.language)}</span> : '—'}
                                        </TableCell>
                                        <TableCell className="hidden xl:table-cell text-sm">
                                            {alarmDefsCount[tag.id]
                                                ? <span className="inline-flex items-center gap-1 text-muted-foreground"><Bell size={13} />{tr('tagsPage.alarm_rules', { count: alarmDefsCount[tag.id] })}</span>
                                                : <span className="text-muted-foreground/60">—</span>}
                                        </TableCell>
                                        <TableCell className="pr-3">
                                            <div className="flex items-center justify-end gap-0.5 text-muted-foreground">
                                                <Button variant="ghost" size="icon" className="size-8" title={tr('tagsPage.view_history')} aria-label={tr('tagsPage.view_history')}
                                                    onClick={() => setHistoryTag({ id: tag.id, alias: tag.alias || tag.code || `#${tag.id}` })}>
                                                    <History size={16} />
                                                </Button>
                                                {isAdmin() && (
                                                    <>
                                                        <Button variant="ghost" size="icon" className="size-8" title={tr('tagsPage.write_value')} aria-label={tr('tagsPage.write_value')}
                                                            onClick={() => openWrite(tag)}>
                                                            <Pencil size={16} />
                                                        </Button>
                                                        <Button variant="ghost" size="icon" className="size-8" title={tr('tagsPage.configure')} aria-label={tr('tagsPage.configure')}
                                                            onClick={() => handleEdit(tag)}>
                                                            <Settings2 size={16} />
                                                        </Button>
                                                        <Button variant="ghost" size="icon" className="size-8 hover:bg-red-500/10 hover:text-red-600" title={tr('common.delete')} aria-label={tr('common.delete')}
                                                            onClick={(e) => handleDelete(e, tag.id)}>
                                                            <Trash2 size={16} />
                                                        </Button>
                                                    </>
                                                )}
                                            </div>
                                        </TableCell>
                                    </TableRow>
                                );
                            })
                        )}
                    </TableBody>
                </Table>
            </div>

            {/* Phone: one compact card per tag — name and state, the value
                large, when it came, and the actions within reach. */}
            <div className="md:hidden flex flex-col gap-2">
                {tagsList.length === 0 ? (
                    <div className="rounded-xl border bg-card p-4">
                        <EmptyState
                            icon={TagsIcon}
                            title={tr('empty.tags_title')}
                            description={selectedGatewayId && selectedGatewayId !== 'all' ? tr('empty.tags_desc') : tr('empty.tags_pick_gateway')}
                        />
                    </div>
                ) : (
                    tagsList.map((tag) => {
                        const cv = currentValues.get(tag.id);
                        const state = tagState(tag, cv);
                        const selected = selectedTagIds.includes(tag.id);
                        const gwName = gatewayMap.get(tag.gateway_id)?.name;
                        return (
                            <div key={tag.id}
                                className={`rounded-xl border bg-card p-3 ${selected ? 'ring-2 ring-foreground/20' : ''} ${state === 'alarm' ? 'border-l-4 border-l-red-500' : ''}`}>
                                <div className="flex items-start gap-2">
                                    {isAdmin() && (
                                        <Button variant="ghost" size="icon" className="-ml-1 size-8 shrink-0"
                                            onClick={(e) => { e.stopPropagation(); toggleSelect(tag.id); }}
                                            aria-label={tr('tagsPage.select_tag')}>
                                            {selected ? <CheckSquare size={16} /> : <Square size={16} className="text-muted-foreground/60" />}
                                        </Button>
                                    )}
                                    <div className="min-w-0 flex-1">
                                        <div className="truncate font-medium leading-snug" title={tag.code}>{tag.alias || tag.code || '-'}</div>
                                        <div className="truncate text-xs text-muted-foreground">{gwName ?? tag.data_type}</div>
                                    </div>
                                    {(valuesReady || state === 'alarm') && <StateBadge state={state} />}
                                </div>
                                <div className="mt-2 flex items-end justify-between gap-2">
                                    <div className="min-w-0">
                                        <ValueText tag={tag} currentValue={cv} className="text-xl" />
                                        <div className="text-xs text-muted-foreground">
                                            {cv?.timestamp ? <span title={absoluteTime(cv.timestamp)}>{timeAgo(cv.timestamp, i18n.language)}</span> : tr('tagsPage.state_never')}
                                            {alarmDefsCount[tag.id] ? <> · <Bell size={11} className="inline -mt-0.5" /> {alarmDefsCount[tag.id]}</> : null}
                                        </div>
                                    </div>
                                    <div className="flex shrink-0 items-center text-muted-foreground">
                                        <Button variant="ghost" size="icon" className="size-10" aria-label={tr('tagsPage.view_history')}
                                            onClick={() => setHistoryTag({ id: tag.id, alias: tag.alias || tag.code || `#${tag.id}` })}>
                                            <History size={18} />
                                        </Button>
                                        {isAdmin() && (
                                            <>
                                                <Button variant="ghost" size="icon" className="size-10" aria-label={tr('tagsPage.write_value')} onClick={() => openWrite(tag)}>
                                                    <Pencil size={18} />
                                                </Button>
                                                <Button variant="ghost" size="icon" className="size-10" aria-label={tr('tagsPage.configure')} onClick={() => handleEdit(tag)}>
                                                    <Settings2 size={18} />
                                                </Button>
                                            </>
                                        )}
                                    </div>
                                </div>
                            </div>
                        );
                    })
                )}
            </div>

            {/* OPC UA Browse Dialog */}
            <Dialog open={isBrowseOpen} onOpenChange={setIsBrowseOpen}>
                <DialogContent className="max-w-3xl max-h-[80vh] flex flex-col">
                    <DialogHeader>
                        <DialogTitle>{tr('tagsPage.browse_title')}</DialogTitle>
                        <DialogDescription>
                            {tr('tagsPage.browse_desc')}
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
                                <div className="animate-spin rounded-full h-9 sm:h-6 w-9 sm:w-6 border-b-2 border-indigo-600 mr-2"></div>
                                {tr('tagsPage.browsing')}
                            </div>
                        ) : browseNodes.length === 0 ? (
                            <div className="flex items-center justify-center h-full text-muted-foreground">
                                {tr('tagsPage.browse_empty')}
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
                                                        <Plus size={14} className="mr-1" /> {tr('tagsPage.add_as_tag')}
                                                    </Button>
                                                </>
                                            )}
                                            {node.node_class === 'Object' && node.children_count > 0 && (
                                                <span className="text-xs text-muted-foreground">{tr('tagsPage.children', { count: node.children_count })} →</span>
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
                        <DialogTitle>{tr('tagsPage.write_title', { alias: writeDialogTag?.alias })}</DialogTitle>
                        <DialogDescription>
                            {tr('tagsPage.write_desc')}
                        </DialogDescription>
                    </DialogHeader>
                    <div className="space-y-4 py-2">
                        <div className="flex items-center gap-2">
                            <span className="text-sm text-muted-foreground">{tr('tagsPage.write_type')}</span>
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
                                placeholder={tr('tagsPage.write_placeholder')}
                                className="font-mono"
                            />
                        )}
                    </div>
                    <DialogFooter>
                        <Button variant="outline" onClick={() => setWriteDialogTag(null)}>{tr('common.cancel')}</Button>
                        <Button onClick={handleTagWrite} disabled={writeLoading}>
                            {writeLoading ? tr('tagsPage.writing') : tr('tagsPage.write')}
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>
            {/* Shared history chart for the clock button: same view as I3X and the map. */}
            <Dialog open={historyTag !== null} onOpenChange={(open) => { if (!open) setHistoryTag(null); }}>
                <DialogContent className="max-w-2xl w-[calc(100vw-2rem)]">
                    <DialogHeader>
                        <DialogTitle>{historyTag?.alias}</DialogTitle>
                    </DialogHeader>
                    {historyTag && (
                        <TagHistoryChart tagId={historyTag.id} tagName={historyTag.alias} height={220} />
                    )}
                </DialogContent>
            </Dialog>
        </div >
    );
};

// Filter by driver and search text, then sort. A plain function so the page's
// memo has one call to cache, which the React Compiler can preserve.
function filterAndSortTags(
    tags: Tag[], gateways: { id: number; driver_type: string }[], filterDriverType: string,
    searchQuery: string, sortField: string | null, sortDir: 'asc' | 'desc',
): Tag[] {
    let filtered = [...tags];
    if (filterDriverType !== 'all') {
        const gwDriverMap = new Map(gateways.map(g => [g.id, g.driver_type]));
        filtered = filtered.filter(t => gwDriverMap.get(t.gateway_id) === filterDriverType);
    }
    if (searchQuery) {
        const lower = searchQuery.toLowerCase();
        filtered = filtered.filter(t =>
            t.code.toLowerCase().includes(lower) ||
            (t.alias && t.alias.toLowerCase().includes(lower))
        );
    }
    if (sortField) {
        filtered.sort((a, b) => {
            const av = (a as unknown as Record<string, unknown>)[sortField] ?? '';
            const bv = (b as unknown as Record<string, unknown>)[sortField] ?? '';
            const cmp = String(av).localeCompare(String(bv), undefined, { numeric: true });
            return sortDir === 'asc' ? cmp : -cmp;
        });
    }
    return filtered;
}

export default TagsPage;
