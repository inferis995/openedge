import { useDeferredValue, useMemo, useState } from 'react';
import { useGateways } from '@/hooks/useGateways';
import { useAreas } from '@/hooks/useAreas';
import { useNavigationStore } from '@/stores/useNavigationStore';
import { useAuthStore } from '@/stores/useAuthStore';
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
import { Plus, Trash2, Wifi, ChevronRight, RefreshCw, Search, Radio } from 'lucide-react';
import { useNavigate } from 'react-router-dom';
import { Badge } from '@/components/ui/badge';
import { CreateGatewayDto, Gateway } from '@/types';
import { LoRaWANDevicesPanel } from '@/components/lorawan/LoRaWANDevicesPanel';

// Extended DTO to include UI-specific fields or fields not yet in shared types
interface ExtendedCreateGatewayDto extends Omit<CreateGatewayDto, 'connection_config'> {
    zero_based: boolean;
    connection_config: any;
    auth_mode?: string;
    username?: string;
    password?: string;
    cert_file?: string;
    key_file?: string;
    // MQTT external-broker fields (all optional; empty broker_host = use
    // OpenEdge's internal broker, the legacy behaviour).
    broker_host?: string;
    broker_port?: number;
    broker_tls?: boolean;
    broker_username?: string;
    broker_password?: string;
    broker_client_id?: string;
    // LoRaWAN fields
    lora_server_type?: string;
    lora_server_host?: string;
    lora_server_port?: number;
    lora_tls_enabled?: boolean;
    lora_username?: string;
    lora_password?: string;
    lora_application_id?: string;
}

const GatewaysPage = () => {
    const navigate = useNavigate();
    const { selectedAreaId, selectedSiteId } = useNavigationStore();
    const { gateways, isLoading, create, remove, testConnection, update, isUpdating } = useGateways(selectedAreaId);
    const { areas } = useAreas(selectedSiteId); // Get areas for current site
    const { isAdmin } = useAuthStore();

    const [isOpen, setIsOpen] = useState(false);
    const [testResult, setTestResult] = useState<{ id: number, success: boolean, message: string } | null>(null);
    const [updatingGatewayId, setUpdatingGatewayId] = useState<number | null>(null);
    const [searchInput, setSearchInput] = useState('');
    const searchQuery = useDeferredValue(searchInput);
    const [lorawanPanel, setLorawanPanel] = useState<{ id: number; name: string } | null>(null);

    const filteredGateways = useMemo(() => {
        if (!searchQuery.trim()) return gateways;
        const q = searchQuery.toLowerCase();
        return gateways.filter(gw =>
            gw.name.toLowerCase().includes(q) ||
            gw.driver_type.toLowerCase().includes(q) ||
            (gw.connection_config?.ip_address || '').toLowerCase().includes(q) ||
            (gw.connection_config?.endpoint || '').toLowerCase().includes(q),
        );
    }, [gateways, searchQuery]);

    const onlineCount  = useMemo(() => gateways.filter(g => g.connection_status === 'online').length, [gateways]);
    const offlineCount = useMemo(() => gateways.filter(g => g.connection_status !== 'online' && g.enabled).length, [gateways]);

    // Form State
    const [formData, setFormData] = useState<Partial<ExtendedCreateGatewayDto>>({
        name: '',
        driver_type: 'S7',
        ip_address: '',
        endpoint: '',
        rack: 0,
        slot: 2,
        port: 502,
        slave_id: 1,
        scan_rate_ms: 1000,
        enabled: true,
        zero_based: false, // Default to false (Standard Modbus)
        auth_mode: 'Anonymous', // Default OPC UA auth
        username: '',
        password: '',
        cert_file: '',
        key_file: '',
        // MQTT external broker (empty host → use internal OpenEdge broker)
        broker_host: '',
        broker_port: 1883,
        broker_tls: false,
        broker_username: '',
        broker_password: '',
        broker_client_id: '',
        // LoRaWAN
        lora_server_type: 'ttn_v3',
        lora_server_host: '',
        lora_server_port: 1883,
        lora_tls_enabled: false,
        lora_username: '',
        lora_password: '',
        lora_application_id: '',
    });
    const [selectedAreaForCreate, setSelectedAreaForCreate] = useState<string>(
        selectedAreaId ? selectedAreaId.toString() : ''
    );

    const handleInputChange = (field: keyof ExtendedCreateGatewayDto, value: any) => {
        setFormData(prev => ({ ...prev, [field]: value }));
    };

    const buildConnectionConfig = () => {
        let connection_config: any = {};
        if (formData.driver_type === 'S7') {
            connection_config = { ip_address: formData.ip_address, rack: formData.rack, slot: formData.slot };
        } else if (formData.driver_type === 'MODBUS_TCP') {
            connection_config = { ip_address: formData.ip_address, port: formData.port, slave_id: formData.slave_id };
        } else if (formData.driver_type === 'OPC_UA') {
            connection_config = {
                endpoint: formData.endpoint,
                auth_mode: formData.auth_mode,
                username: formData.username,
                password: formData.password,
                cert_file: formData.cert_file,
                key_file: formData.key_file,
            };
        } else if (formData.driver_type === 'MQTT') {
            connection_config = {
                broker_host: formData.broker_host || '',
                broker_port: formData.broker_port || 1883,
                broker_tls: !!formData.broker_tls,
                broker_username: formData.broker_username || '',
                broker_password: formData.broker_password || '',
                broker_client_id: formData.broker_client_id || '',
            };
        } else if (formData.driver_type === 'LORAWAN') {
            connection_config = {
                server_type: formData.lora_server_type || 'ttn_v3',
                server_host: formData.lora_server_host || '',
                server_port: formData.lora_server_port || 1883,
                tls_enabled: !!formData.lora_tls_enabled,
                username: formData.lora_username || '',
                password: formData.lora_password || '',
                application_id: formData.lora_application_id || '',
            };
        }
        return connection_config;
    };

    const handleCreate = async () => {
        if (!formData.name || !selectedAreaForCreate) return;
        // IP is required for S7 and MODBUS_TCP only
        const noIPDrivers = ['MQTT', 'OPC_UA', 'LORAWAN'];
        if (!noIPDrivers.includes(formData.driver_type!) && !formData.ip_address) return;
        if (formData.driver_type === 'OPC_UA' && !formData.endpoint) return;
        if (formData.driver_type === 'LORAWAN' && (!formData.lora_server_host || !formData.lora_application_id)) return;

        try {
            const connection_config = buildConnectionConfig();

            const payload: ExtendedCreateGatewayDto = {
                ...formData,
                connection_config,
                area_id: parseInt(selectedAreaForCreate),
                org_id: useNavigationStore.getState().selectedOrgId || undefined
            } as ExtendedCreateGatewayDto; // Cast to ExtendedCreateGatewayDto

            if (updatingGatewayId) {
                await update({ id: updatingGatewayId, data: payload });
            } else {
                await create(payload);
            }

            setIsOpen(false);
            resetForm();
        } catch (error) {
            console.error('Failed to save gateway', error);
        }
    };

    const resetForm = () => {
        setUpdatingGatewayId(null);
        setFormData({
            name: '',
            driver_type: 'S7',
            ip_address: '',
            endpoint: '',
            rack: 0,
            slot: 2,
            port: 502,
            slave_id: 1,
            scan_rate_ms: 1000,
            enabled: true,
            zero_based: false, // Default to false (Standard Modbus)
            auth_mode: 'Anonymous',
            username: '',
            password: '',
            cert_file: '',
            // MQTT external broker (reset on Add)
            broker_host: '',
            broker_port: 1883,
            broker_tls: false,
            broker_username: '',
            broker_password: '',
            broker_client_id: '',
            key_file: '',
            // LoRaWAN
            lora_server_type: 'ttn_v3',
            lora_server_host: '',
            lora_server_port: 1883,
            lora_tls_enabled: false,
            lora_username: '',
            lora_password: '',
            lora_application_id: '',
        });
    };

    const handleEditOpen = (gateway: Gateway) => {
        setUpdatingGatewayId(gateway.id);
        setSelectedAreaForCreate(gateway.area_id.toString());

        // Parse connection config based on driver type
        let rack = 0, slot = 2, port = 502, slave_id = 1, ip_address = '';
        let auth_mode = 'Anonymous', username = '', password = '', cert_file = '', key_file = '';
        let endpoint = '';
        let broker_host = '', broker_port = 1883, broker_tls = false;
        let broker_username = '', broker_password = '', broker_client_id = '';
        let lora_server_type = 'ttn_v3', lora_server_host = '', lora_server_port = 1883;
        let lora_tls_enabled = false, lora_username = '', lora_password = '', lora_application_id = '';

        if (gateway.connection_config) {
            const config = gateway.connection_config;
            ip_address = config.ip_address || '';
            if (gateway.driver_type === 'S7') {
                rack = config.rack || 0;
                slot = config.slot || 2;
            } else if (gateway.driver_type === 'MODBUS_TCP') {
                port = config.port || 502;
                slave_id = config.slave_id || 1;
            } else if (gateway.driver_type === 'OPC_UA') {
                endpoint = config.endpoint || '';
                auth_mode = config.auth_mode || 'Anonymous';
                username = config.username || '';
                password = config.password || '';
                cert_file = config.cert_file || '';
                key_file = config.key_file || '';
            } else if (gateway.driver_type === 'MQTT') {
                broker_host = config.broker_host || '';
                broker_port = config.broker_port || 1883;
                broker_tls = !!config.broker_tls;
                broker_username = config.broker_username || '';
                broker_password = config.broker_password || '';
                broker_client_id = config.broker_client_id || '';
            } else if (gateway.driver_type === 'LORAWAN') {
                lora_server_type = config.server_type || 'ttn_v3';
                lora_server_host = config.server_host || '';
                lora_server_port = config.server_port || 1883;
                lora_tls_enabled = !!config.tls_enabled;
                lora_username = config.username || '';
                lora_password = config.password || '';
                lora_application_id = config.application_id || '';
            }
        }

        setFormData({
            name: gateway.name,
            driver_type: gateway.driver_type,
            ip_address: ip_address,
            endpoint,
            rack,
            slot,
            port,
            slave_id,
            scan_rate_ms: gateway.scan_rate_ms,
            enabled: gateway.enabled,
            zero_based: gateway.zero_based !== undefined ? gateway.zero_based : false,
            auth_mode,
            username,
            password,
            cert_file,
            key_file,
            broker_host,
            broker_port,
            broker_tls,
            broker_username,
            broker_password,
            broker_client_id,
            lora_server_type,
            lora_server_host,
            lora_server_port,
            lora_tls_enabled,
            lora_username,
            lora_password,
            lora_application_id,
        });
        setIsOpen(true);
    };

    const handleDelete = async (e: React.MouseEvent, id: number) => {
        e.stopPropagation();
        if (confirm('Are you sure you want to delete this gateway?')) {
            await remove(id);
        }
    };

    const handleTest = async (e: React.MouseEvent, id: number) => {
        e.stopPropagation();
        try {
            const result = await testConnection(id);
            setTestResult({ id, ...result });
            setTimeout(() => setTestResult(null), 3000);
        } catch (error) {
            setTestResult({ id, success: false, message: 'Connection failed' });
        }
    };

    const handleToggleEnabled = async (id: number, currentEnabled: boolean) => {
        setUpdatingGatewayId(id);
        try {
            await update({ id, data: { enabled: !currentEnabled } });
        } catch (error) {
            console.error('Failed to toggle gateway enabled state', error);
        } finally {
            setUpdatingGatewayId(null);
        }
    };

    const handleSelect = (id: number) => {
        navigate(`/tags?gateway_id=${id}`);
    };

    if (isLoading) {
        return <div className="p-8 text-center text-muted-foreground">Loading gateways...</div>;
    }

    return (
        <div className="space-y-6">
            <div className="flex items-center justify-between">
                <div>
                    <h2 className="text-2xl font-bold tracking-tight">Gateways</h2>
                    <p className="text-muted-foreground">
                        Configure PLC connections and communication drivers.
                    </p>
                </div>
                {isAdmin() && (
                    <Dialog open={isOpen} onOpenChange={(open) => {
                        setIsOpen(open);
                        if (!open) resetForm();
                    }}>
                        <DialogTrigger asChild>
                            <Button className="gap-2">
                                <Plus size={16} /> Add Gateway
                            </Button>
                        </DialogTrigger>
                        <DialogContent className="max-w-xl">
                            <DialogHeader>
                                <DialogTitle>{updatingGatewayId ? 'Edit Gateway' : 'Create Gateway'}</DialogTitle>
                            </DialogHeader>
                            <div className="grid gap-4 py-4">
                                <div className="grid grid-cols-2 gap-4">
                                    <div className="grid gap-2">
                                        <Label htmlFor="area">Area</Label>
                                        <Select
                                            value={selectedAreaForCreate}
                                            onValueChange={setSelectedAreaForCreate}
                                        >
                                            <SelectTrigger>
                                                <SelectValue placeholder="Select Area" />
                                            </SelectTrigger>
                                            <SelectContent>
                                                {areas.map((area) => (
                                                    <SelectItem key={area.id} value={area.id.toString()}>
                                                        {area.name}
                                                    </SelectItem>
                                                ))}
                                            </SelectContent>
                                        </Select>
                                    </div>
                                    <div className="grid gap-2">
                                        <Label htmlFor="driver">Driver Type</Label>
                                        <Select
                                            value={formData.driver_type}
                                            onValueChange={(val) => handleInputChange('driver_type', val)}
                                            disabled={!!updatingGatewayId} // Prevent changing driver type on edit
                                        >
                                            <SelectTrigger>
                                                <SelectValue placeholder="Select Driver" />
                                            </SelectTrigger>
                                            <SelectContent>
                                                <SelectItem value="S7">Siemens S7</SelectItem>
                                                <SelectItem value="MODBUS_TCP">Modbus TCP</SelectItem>
                                                <SelectItem value="MQTT">MQTT Native</SelectItem>
                                                <SelectItem value="OPC_UA">OPC UA</SelectItem>
                                                <SelectItem value="LORAWAN">LoRaWAN (TTN / ChirpStack)</SelectItem>
                                            </SelectContent>
                                        </Select>
                                    </div>
                                </div>

                                <div className="grid gap-2">
                                    <Label htmlFor="name">Gateway Name</Label>
                                    <Input
                                        id="name"
                                        value={formData.name}
                                        onChange={(e) => handleInputChange('name', e.target.value)}
                                        placeholder="e.g. PLC Line 1"
                                    />
                                </div>

                                {formData.driver_type !== 'MQTT' && formData.driver_type !== 'OPC_UA' && formData.driver_type !== 'LORAWAN' && (
                                    <div className="grid grid-cols-2 gap-4">
                                        <div className="grid gap-2">
                                            <Label htmlFor="ip">IP Address</Label>
                                            <Input
                                                id="ip"
                                                value={formData.ip_address}
                                                onChange={(e) => handleInputChange('ip_address', e.target.value)}
                                                placeholder="192.168.1.10"
                                            />
                                        </div>
                                        <div className="grid gap-2">
                                            <Label htmlFor="scan">Scan Rate (ms)</Label>
                                            <Input
                                                id="scan"
                                                type="number"
                                                value={formData.scan_rate_ms}
                                                onChange={(e) => handleInputChange('scan_rate_ms', parseInt(e.target.value))}
                                            />
                                        </div>
                                    </div>
                                )}

                                {formData.driver_type === 'S7' && (
                                    <div className="grid grid-cols-2 gap-4 p-4 bg-muted/50 rounded-md border">
                                        <div className="grid gap-2">
                                            <Label htmlFor="rack">Rack</Label>
                                            <Input
                                                id="rack"
                                                type="number"
                                                value={formData.rack}
                                                onChange={(e) => handleInputChange('rack', parseInt(e.target.value))}
                                            />
                                        </div>
                                        <div className="grid gap-2">
                                            <Label htmlFor="slot">Slot</Label>
                                            <Input
                                                id="slot"
                                                type="number"
                                                value={formData.slot}
                                                onChange={(e) => handleInputChange('slot', parseInt(e.target.value))}
                                            />
                                        </div>
                                    </div>
                                )}

                                {formData.driver_type === 'MODBUS_TCP' && (
                                    <>
                                        <div className="grid grid-cols-2 gap-4">
                                            <div className="grid gap-2">
                                                <Label htmlFor="port">Port</Label>
                                                <Input
                                                    id="port"
                                                    type="number"
                                                    value={formData.port}
                                                    onChange={(e) => handleInputChange('port', parseInt(e.target.value))}
                                                />
                                            </div>
                                            <div className="grid gap-2">
                                                <Label htmlFor="slave">Slave ID</Label>
                                                <Input
                                                    id="slave"
                                                    type="number"
                                                    value={formData.slave_id}
                                                    onChange={(e) => handleInputChange('slave_id', parseInt(e.target.value))}
                                                />
                                            </div>
                                        </div>

                                        {/* Zero-Based Addressing Toggle */}
                                        <div className="flex items-center justify-between p-3 bg-muted/50 rounded-md border">
                                            <div>
                                                <Label htmlFor="zero_based" className="text-sm font-semibold">Zero-Based Addressing</Label>
                                                <p className="text-xs text-muted-foreground mt-0.5">
                                                    Enable if your PLC documentation starts addresses from 0
                                                </p>
                                            </div>
                                            <Switch
                                                id="zero_based"
                                                checked={formData.zero_based}
                                                onCheckedChange={(checked) => handleInputChange('zero_based', checked)}
                                            />
                                        </div>

                                        {/* Dynamic Addressing Note */}
                                        <div className={`p-3 text-xs rounded-md border ${formData.zero_based ? 'bg-amber-50 text-amber-800 border-amber-200 dark:bg-amber-950/30 dark:text-amber-300 dark:border-amber-800' : 'bg-primary/5 text-primary border-primary/20'}`}>
                                            <p className="font-semibold mb-1">Modbus Addressing:</p>
                                            {formData.zero_based ? (
                                                <>
                                                    <p>Addresses map <strong>directly</strong> to PLC registers (no offset).</p>
                                                    <ul className="list-disc list-inside mt-1 space-y-0.5 opacity-90">
                                                        <li>Address <strong>40000</strong> → Register <strong>0</strong> (Holding)</li>
                                                        <li>Address <strong>40001</strong> → Register <strong>1</strong> (Holding)</li>
                                                        <li>Address <strong>30000</strong> → Register <strong>0</strong> (Input)</li>
                                                    </ul>
                                                </>
                                            ) : (
                                                <>
                                                    <p>Addresses use standard Modbus convention (1-based).</p>
                                                    <ul className="list-disc list-inside mt-1 space-y-0.5 opacity-90">
                                                        <li>Address <strong>40001</strong> → Register <strong>0</strong> (Holding)</li>
                                                        <li>Address <strong>40002</strong> → Register <strong>1</strong> (Holding)</li>
                                                        <li>Address <strong>30001</strong> → Register <strong>0</strong> (Input)</li>
                                                    </ul>
                                                </>
                                            )}
                                        </div>
                                    </>
                                )}

                                {formData.driver_type === 'MQTT' && (
                                    <div className="space-y-4">
                                        <div className="p-3 bg-emerald-50 dark:bg-emerald-950/30 rounded-md border border-emerald-200 dark:border-emerald-800 text-xs text-emerald-800 dark:text-emerald-300">
                                            <p className="font-semibold mb-1">MQTT Native Driver</p>
                                            <ul className="list-disc list-inside space-y-0.5 text-emerald-700 dark:text-emerald-400">
                                                <li>Tag <strong>Code</strong> = the PLC's MQTT topic (e.g. <code>wago/sensori/T1</code>)</li>
                                                <li>Optionally subscribe to a customer-owned external broker (below).</li>
                                                <li>For JSON payloads use the tag's <code>json_path</code> field to extract a single field.</li>
                                            </ul>
                                        </div>

                                        {/* External broker (optional) */}
                                        <div className="space-y-3 border rounded-md p-3 bg-muted/30">
                                            <div>
                                                <p className="text-sm font-semibold">External broker (optional)</p>
                                                <p className="text-[11px] text-muted-foreground">
                                                    Leave the host empty to use OpenEdge's internal broker (the PLC publishes straight to us). Set host/port when the PLCs publish to <strong>their own</strong> broker and OpenEdge should connect to it.
                                                </p>
                                            </div>
                                            <div className="grid grid-cols-3 gap-3">
                                                <div className="col-span-2 grid gap-1">
                                                    <Label htmlFor="broker_host" className="text-xs">Broker host (IP or hostname)</Label>
                                                    <Input id="broker_host" value={formData.broker_host || ''}
                                                        onChange={(e) => handleInputChange('broker_host', e.target.value)}
                                                        placeholder="es. 192.168.1.40  oppure  mqtt.cliente.local" />
                                                </div>
                                                <div className="grid gap-1">
                                                    <Label htmlFor="broker_port" className="text-xs">Port</Label>
                                                    <Input id="broker_port" type="number" value={formData.broker_port ?? 1883}
                                                        onChange={(e) => handleInputChange('broker_port', parseInt(e.target.value) || 1883)} />
                                                </div>
                                            </div>
                                            <div className="grid grid-cols-2 gap-3">
                                                <div className="grid gap-1">
                                                    <Label htmlFor="broker_username" className="text-xs">Username (optional)</Label>
                                                    <Input id="broker_username" value={formData.broker_username || ''}
                                                        onChange={(e) => handleInputChange('broker_username', e.target.value)} autoComplete="off" />
                                                </div>
                                                <div className="grid gap-1">
                                                    <Label htmlFor="broker_password" className="text-xs">Password (optional)</Label>
                                                    <Input id="broker_password" type="password" value={formData.broker_password || ''}
                                                        onChange={(e) => handleInputChange('broker_password', e.target.value)} autoComplete="new-password" />
                                                </div>
                                            </div>
                                            <div className="grid grid-cols-2 gap-3 items-end">
                                                <div className="grid gap-1">
                                                    <Label htmlFor="broker_client_id" className="text-xs">Client ID (optional)</Label>
                                                    <Input id="broker_client_id" value={formData.broker_client_id || ''}
                                                        onChange={(e) => handleInputChange('broker_client_id', e.target.value)}
                                                        placeholder="auto-generato se vuoto" />
                                                </div>
                                                <label className="flex items-center gap-2 pb-2 cursor-pointer">
                                                    <Switch checked={!!formData.broker_tls}
                                                        onCheckedChange={(v) => handleInputChange('broker_tls', v)} />
                                                    <span className="text-xs">TLS (port 8883 typically)</span>
                                                </label>
                                            </div>
                                        </div>
                                    </div>
                                )}

                                {formData.driver_type === 'LORAWAN' && (
                                    <div className="space-y-4">
                                        {/* Info box */}
                                        <div className="p-3 bg-violet-50 dark:bg-violet-950/30 rounded-md border border-violet-200 dark:border-violet-800 text-xs text-violet-800 dark:text-violet-300">
                                            <p className="font-semibold mb-1">LoRaWAN Network Server Bridge</p>
                                            <ul className="list-disc list-inside space-y-0.5 text-violet-700 dark:text-violet-400">
                                                <li>Supporta <strong>The Things Network v3</strong> e <strong>ChirpStack v4</strong></li>
                                                <li>Tag <strong>Code</strong> = <code>device_id/campo</code> (es. <code>sensor-01/temperature</code>)</li>
                                                <li>Campi speciali: <code>rssi</code>, <code>snr</code>, <code>f_port</code></li>
                                                <li>Il wildcard <code>*/campo</code> riceve da qualsiasi device</li>
                                            </ul>
                                        </div>

                                        {/* Server type */}
                                        <div className="grid gap-2">
                                            <Label className="text-xs">Network Server</Label>
                                            <Select value={formData.lora_server_type || 'ttn_v3'}
                                                onValueChange={v => handleInputChange('lora_server_type', v)}>
                                                <SelectTrigger><SelectValue /></SelectTrigger>
                                                <SelectContent>
                                                    <SelectItem value="ttn_v3">The Things Network v3</SelectItem>
                                                    <SelectItem value="chirpstack">ChirpStack v4</SelectItem>
                                                </SelectContent>
                                            </Select>
                                        </div>

                                        {/* Application ID */}
                                        <div className="grid gap-2">
                                            <Label htmlFor="lora_app" className="text-xs">Application ID</Label>
                                            <Input id="lora_app" value={formData.lora_application_id || ''}
                                                onChange={e => handleInputChange('lora_application_id', e.target.value)}
                                                placeholder={formData.lora_server_type === 'chirpstack' ? 'my-application' : 'my-app (TTN Application ID)'}
                                            />
                                        </div>

                                        {/* Server host + port */}
                                        <div className="grid grid-cols-3 gap-3">
                                            <div className="col-span-2 grid gap-1">
                                                <Label htmlFor="lora_host" className="text-xs">Server Host (MQTT)</Label>
                                                <Input id="lora_host" value={formData.lora_server_host || ''}
                                                    onChange={e => handleInputChange('lora_server_host', e.target.value)}
                                                    placeholder={formData.lora_server_type === 'chirpstack'
                                                        ? 'localhost'
                                                        : 'eu1.cloud.thethings.network'}
                                                />
                                            </div>
                                            <div className="grid gap-1">
                                                <Label htmlFor="lora_port" className="text-xs">Port</Label>
                                                <Input id="lora_port" type="number" value={formData.lora_server_port ?? 1883}
                                                    onChange={e => handleInputChange('lora_server_port', parseInt(e.target.value) || 1883)} />
                                            </div>
                                        </div>

                                        {/* Credentials */}
                                        <div className="grid grid-cols-2 gap-3">
                                            <div className="grid gap-1">
                                                <Label htmlFor="lora_user" className="text-xs">
                                                    {formData.lora_server_type === 'chirpstack' ? 'MQTT Username' : 'Username (app-id@ttn)'}
                                                </Label>
                                                <Input id="lora_user" value={formData.lora_username || ''}
                                                    onChange={e => handleInputChange('lora_username', e.target.value)}
                                                    placeholder={formData.lora_server_type === 'chirpstack' ? 'chirpstack' : 'my-app@ttn'}
                                                    autoComplete="off" />
                                            </div>
                                            <div className="grid gap-1">
                                                <Label htmlFor="lora_pass" className="text-xs">
                                                    {formData.lora_server_type === 'chirpstack' ? 'MQTT Password' : 'API Key'}
                                                </Label>
                                                <Input id="lora_pass" type="password" value={formData.lora_password || ''}
                                                    onChange={e => handleInputChange('lora_password', e.target.value)}
                                                    placeholder={formData.lora_server_type === 'chirpstack' ? '••••••••' : 'NNSXS.XXXXXX'}
                                                    autoComplete="new-password" />
                                            </div>
                                        </div>

                                        <label className="flex items-center gap-2 cursor-pointer">
                                            <Switch checked={!!formData.lora_tls_enabled}
                                                onCheckedChange={v => handleInputChange('lora_tls_enabled', v)} />
                                            <span className="text-xs">TLS/SSL (porta 8883 tipicamente)</span>
                                        </label>
                                    </div>
                                )}

                                {formData.driver_type === 'OPC_UA' && (
                                    <div className="space-y-4">
                                        <div className="grid gap-2">
                                            <Label htmlFor="endpoint">Endpoint URL</Label>
                                            <Input
                                                id="endpoint"
                                                value={formData.endpoint}
                                                onChange={(e) => handleInputChange('endpoint', e.target.value)}
                                                placeholder="opc.tcp://192.168.1.10:4840"
                                            />
                                        </div>
                                        <div className="grid gap-2">
                                            <Label htmlFor="scan">Scan Rate (ms)</Label>
                                            <Input
                                                id="scan"
                                                type="number"
                                                value={formData.scan_rate_ms}
                                                onChange={(e) => handleInputChange('scan_rate_ms', parseInt(e.target.value))}
                                            />
                                        </div>

                                        <div className="grid gap-2 border-t pt-4">
                                            <Label htmlFor="auth_mode">Authentication Mode</Label>
                                            <Select
                                                value={formData.auth_mode || 'Anonymous'}
                                                onValueChange={(val) => handleInputChange('auth_mode', val)}
                                            >
                                                <SelectTrigger>
                                                    <SelectValue placeholder="Select Auth Mode" />
                                                </SelectTrigger>
                                                <SelectContent>
                                                    <SelectItem value="Anonymous">Anonymous</SelectItem>
                                                    <SelectItem value="Username">Username / Password</SelectItem>
                                                    <SelectItem value="Certificate">Certificate</SelectItem>
                                                </SelectContent>
                                            </Select>
                                        </div>

                                        {formData.auth_mode === 'Username' && (
                                            <div className="grid grid-cols-2 gap-4">
                                                <div className="grid gap-2">
                                                    <Label htmlFor="username">Username</Label>
                                                    <Input
                                                        id="username"
                                                        value={formData.username}
                                                        onChange={(e) => handleInputChange('username', e.target.value)}
                                                        placeholder="admin"
                                                    />
                                                </div>
                                                <div className="grid gap-2">
                                                    <Label htmlFor="password">Password</Label>
                                                    <Input
                                                        id="password"
                                                        type="password"
                                                        value={formData.password}
                                                        onChange={(e) => handleInputChange('password', e.target.value)}
                                                        placeholder="••••••••"
                                                    />
                                                </div>
                                            </div>
                                        )}

                                        {formData.auth_mode === 'Certificate' && (
                                            <div className="grid gap-4 bg-muted/50 p-3 rounded-md border">
                                                <div className="grid gap-2">
                                                    <Label htmlFor="cert_file">Certificate File Path (Server-side)</Label>
                                                    <Input
                                                        id="cert_file"
                                                        value={formData.cert_file}
                                                        onChange={(e) => handleInputChange('cert_file', e.target.value)}
                                                        placeholder="/app/certs/client.pem"
                                                    />
                                                </div>
                                                <div className="grid gap-2">
                                                    <Label htmlFor="key_file">Private Key File Path (Server-side)</Label>
                                                    <Input
                                                        id="key_file"
                                                        value={formData.key_file}
                                                        onChange={(e) => handleInputChange('key_file', e.target.value)}
                                                        placeholder="/app/certs/client.key"
                                                    />
                                                </div>
                                                <div className="text-xs text-muted-foreground mt-1">
                                                    Paths must be valid inside the <span className="font-semibold">driver-opcua</span> container.
                                                </div>
                                            </div>
                                        )}

                                        <div className="p-4 bg-indigo-50 dark:bg-indigo-950/30 rounded-md border border-indigo-200 dark:border-indigo-800 mt-2">
                                            <p className="font-semibold text-indigo-800 dark:text-indigo-300 mb-2">OPC UA Driver</p>
                                            <p className="text-sm text-indigo-700 dark:text-indigo-400 mb-2">
                                                Connects to an OPC UA server and reads selected nodes at the configured scan rate.
                                            </p>
                                            <ul className="list-disc list-inside text-xs text-indigo-600 dark:text-indigo-400 space-y-1">
                                                <li>Enter the server's OPC UA endpoint URL</li>
                                                <li>After creating the gateway, use <strong>Browse Server</strong> in the Tags page to discover and add nodes</li>
                                                <li>Tag <strong>Code</strong> = OPC UA Node ID (e.g. <code>ns=2;s=Temperature</code>)</li>
                                            </ul>
                                        </div>
                                    </div>
                                )}

                                <div className="flex items-center space-x-2">
                                    <Switch
                                        id="enabled"
                                        checked={formData.enabled}
                                        onCheckedChange={(checked) => handleInputChange('enabled', checked)}
                                    />
                                    <Label htmlFor="enabled">Enabled</Label>
                                </div>
                            </div>
                            <DialogFooter>
                                <Button onClick={handleCreate}>{updatingGatewayId ? 'Update Gateway' : 'Create Gateway'}</Button>
                            </DialogFooter>
                        </DialogContent>
                    </Dialog>
                )}
            </div>

            {/* Stats bar */}
            <div className="flex flex-wrap items-center gap-6 text-sm text-muted-foreground px-1">
                <span>
                    <strong className="text-foreground">{gateways.length}</strong> gateway
                    {filteredGateways.length !== gateways.length && ` (${filteredGateways.length} filtrati)`}
                </span>
                <span className="flex items-center gap-1.5">
                    <span className="w-2 h-2 rounded-full bg-emerald-500" />
                    <strong className="text-emerald-600">{onlineCount}</strong> online
                </span>
                {offlineCount > 0 && (
                    <span className="flex items-center gap-1.5">
                        <span className="w-2 h-2 rounded-full bg-red-500 animate-pulse" />
                        <strong className="text-red-500">{offlineCount}</strong> offline
                    </span>
                )}
                <div className="ml-auto relative w-64">
                    <Search size={14} className="absolute left-2.5 top-1/2 -translate-y-1/2 text-muted-foreground pointer-events-none" />
                    <Input
                        className="pl-8 h-8 text-sm"
                        placeholder="Cerca per nome, driver, IP..."
                        value={searchInput}
                        onChange={e => setSearchInput(e.target.value)}
                    />
                </div>
            </div>

            <div className="clip-chamfer border bg-card">
                <Table>
                    <TableHeader>
                        <TableRow>
                            <TableHead className="w-[60px]">ID</TableHead>
                            <TableHead>Name</TableHead>
                            <TableHead>Type</TableHead>
                            <TableHead>Config</TableHead>
                            <TableHead>Status</TableHead>
                            <TableHead>Enabled</TableHead>
                            {isAdmin() && <TableHead className="text-right">Actions</TableHead>}
                        </TableRow>
                    </TableHeader>
                    <TableBody>
                        {filteredGateways.length === 0 ? (
                            <TableRow>
                                <TableCell colSpan={7} className="h-24 text-center">
                                    {gateways.length === 0
                                        ? (selectedAreaId ? 'Create one for the selected area.' : 'Select an area to filter or add a new gateway.')
                                        : 'Nessun gateway corrisponde alla ricerca.'}
                                </TableCell>
                            </TableRow>
                        ) : (
                            filteredGateways.map((gw) => (
                                <TableRow
                                    key={gw.id}
                                    className={`cursor-pointer hover:bg-muted/50 ${!gw.enabled ? 'opacity-50' : ''}`}
                                    onClick={() => handleSelect(gw.id)}
                                >
                                    <TableCell>{gw.id}</TableCell>
                                    <TableCell className="font-medium">
                                        <div className="flex flex-col">
                                            <span>{gw.name}</span>
                                            <span className="text-xs text-muted-foreground">{gw.connection_config?.ip_address || ''}</span>
                                        </div>
                                    </TableCell>
                                    <TableCell>
                                        <Badge variant="outline">{gw.driver_type}</Badge>
                                    </TableCell>
                                    <TableCell className="text-xs text-muted-foreground">
                                        {gw.driver_type === 'S7'
                                            ? `Rack: ${gw.connection_config?.rack ?? 0}, Slot: ${gw.connection_config?.slot ?? 0}`
                                            : gw.driver_type === 'MODBUS_TCP'
                                                ? `Port: ${gw.connection_config?.port ?? 502}, Slave: ${gw.connection_config?.slave_id ?? 1}`
                                                : gw.driver_type === 'OPC_UA'
                                                    ? (gw.connection_config?.endpoint || 'No endpoint')
                                                    : gw.driver_type === 'MQTT'
                                                        ? (gw.connection_config?.broker_host ? `${gw.connection_config.broker_host}:${gw.connection_config.broker_port ?? 1883}` : 'Internal broker')
                                                        : gw.driver_type === 'LORAWAN'
                                                            ? `${gw.connection_config?.server_type ?? 'ttn_v3'} · ${gw.connection_config?.server_host || '—'}`
                                                            : '—'
                                        }
                                        {gw.driver_type !== 'MQTT' && gw.driver_type !== 'LORAWAN' && (
                                            <><br />Scan: {gw.scan_rate_ms >= 1000 ? `${gw.scan_rate_ms / 1000}s` : `${gw.scan_rate_ms}ms`}</>
                                        )}
                                    </TableCell>
                                    <TableCell>
                                        <div className="flex items-center gap-2">
                                            <div className={`h-2 w-2 clip-hex ${gw.connection_status === 'online' ? 'bg-[#10B981] animate-pulse' : 'bg-destructive'}`} />
                                            <span className="text-[10px] font-bold tracking-widest uppercase text-muted-foreground">{gw.connection_status || 'Unknown'}</span>
                                        </div>
                                    </TableCell>
                                    <TableCell>
                                        <div className="flex items-center gap-2" onClick={(e) => e.stopPropagation()}>
                                            <Switch
                                                checked={gw.enabled}
                                                onCheckedChange={() => handleToggleEnabled(gw.id, gw.enabled)}
                                                disabled={updatingGatewayId === gw.id || isUpdating}
                                            />
                                            {updatingGatewayId === gw.id && (
                                                <RefreshCw size={14} className="animate-spin text-muted-foreground" />
                                            )}
                                        </div>
                                    </TableCell>
                                    <TableCell className="text-right">
                                        {isAdmin() && (
                                            <div className="flex items-center justify-end gap-2">
                                                <Button
                                                    variant="outline"
                                                    size="sm"
                                                    className="h-8 text-xs gap-1"
                                                    onClick={(e) => {
                                                        e.stopPropagation();
                                                        handleEditOpen(gw);
                                                    }}
                                                >
                                                    Edit
                                                </Button>

                                                <Button
                                                    variant="secondary"
                                                    size="sm"
                                                    className="h-8 text-xs gap-1"
                                                    onClick={(e) => handleTest(e, gw.id)}
                                                >
                                                    <Wifi size={12} />
                                                    Test
                                                </Button>
                                                {gw.driver_type === 'LORAWAN' && (
                                                    <Button
                                                        variant="outline"
                                                        size="sm"
                                                        className="h-8 text-xs gap-1 text-violet-600 border-violet-300 hover:bg-violet-50 dark:hover:bg-violet-950/30"
                                                        onClick={(e) => {
                                                            e.stopPropagation();
                                                            setLorawanPanel({ id: gw.id, name: gw.name });
                                                        }}
                                                    >
                                                        <Radio size={12} />
                                                        Dispositivi
                                                    </Button>
                                                )}
                                                <Button
                                                    variant="ghost"
                                                    size="icon"
                                                    className="h-8 w-8 text-destructive hover:text-destructive hover:bg-destructive/10"
                                                    onClick={(e) => handleDelete(e, gw.id)}
                                                >
                                                    <Trash2 size={16} />
                                                </Button>
                                                <ChevronRight size={16} className="text-muted-foreground" />
                                            </div>
                                        )}
                                        {testResult?.id === gw.id && (
                                            <div className={`text-[10px] uppercase tracking-widest font-bold mt-1 ${testResult.success ? 'text-[#10B981]' : 'text-destructive'}`}>
                                                {testResult.message}
                                            </div>
                                        )}
                                    </TableCell>
                                </TableRow>
                            ))
                        )}
                    </TableBody>
                </Table>
            </div>

            {lorawanPanel && (
                <LoRaWANDevicesPanel
                    gatewayId={lorawanPanel.id}
                    gatewayName={lorawanPanel.name}
                    open={true}
                    onClose={() => setLorawanPanel(null)}
                />
            )}
        </div>
    );
};

export default GatewaysPage;
