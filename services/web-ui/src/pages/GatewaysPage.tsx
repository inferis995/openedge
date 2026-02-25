import { useState } from 'react';
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
import { Plus, Trash2, Wifi, ChevronRight, RefreshCw } from 'lucide-react';
import { useNavigate } from 'react-router-dom';
import { Badge } from '@/components/ui/badge';
import { CreateGatewayDto, Gateway } from '@/types';

// Extended DTO to include UI-specific fields or fields not yet in shared types
interface ExtendedCreateGatewayDto extends Omit<CreateGatewayDto, 'connection_config'> {
    zero_based: boolean;
    connection_config: any;
    auth_mode?: string;
    username?: string;
    password?: string;
    cert_file?: string;
    key_file?: string;
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
            connection_config = {};
        }
        return connection_config;
    };

    const handleCreate = async () => {
        if (!formData.name || !selectedAreaForCreate) return;
        // IP is required for S7 and MODBUS_TCP, not for MQTT or OPC_UA
        if (formData.driver_type !== 'MQTT' && formData.driver_type !== 'OPC_UA' && !formData.ip_address) return;
        // Endpoint is required for OPC_UA
        if (formData.driver_type === 'OPC_UA' && !formData.endpoint) return;

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
            key_file: '',
        });
    };

    const handleEditOpen = (gateway: Gateway) => {
        setUpdatingGatewayId(gateway.id);
        setSelectedAreaForCreate(gateway.area_id.toString());

        // Parse connection config based on driver type
        let rack = 0, slot = 2, port = 502, slave_id = 1, ip_address = '';
        let auth_mode = 'Anonymous', username = '', password = '', cert_file = '', key_file = '';
        let endpoint = '';

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
        return <div className="p-8 text-center text-slate-500">Loading gateways...</div>;
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

                                {formData.driver_type !== 'MQTT' && formData.driver_type !== 'OPC_UA' && (
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
                                    <div className="grid grid-cols-2 gap-4 p-4 bg-slate-50 rounded-md border">
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
                                        <div className="flex items-center justify-between p-3 bg-slate-50 rounded-md border">
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
                                        <div className={`p-3 text-xs rounded-md border ${formData.zero_based ? 'bg-amber-50 text-amber-800 border-amber-200' : 'bg-blue-50 text-blue-800 border-blue-100'}`}>
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
                                    <div className="p-4 bg-emerald-50 rounded-md border border-emerald-200">
                                        <p className="font-semibold text-emerald-800 mb-2">MQTT Native Driver</p>
                                        <p className="text-sm text-emerald-700 mb-2">
                                            The PLC publishes data directly to this system's MQTT broker.
                                        </p>
                                        <ul className="list-disc list-inside text-xs text-emerald-600 space-y-1">
                                            <li>No IP address needed — the PLC connects to your broker</li>
                                            <li>Tag <strong>Code</strong> = the PLC's MQTT topic (e.g. <code>wago/sensori/T1</code>)</li>
                                            <li>Data is automatically bridged to the system format</li>
                                        </ul>
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
                                            <div className="grid gap-4 bg-slate-50 p-3 rounded-md border">
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

                                        <div className="p-4 bg-indigo-50 rounded-md border border-indigo-200 mt-2">
                                            <p className="font-semibold text-indigo-800 mb-2">OPC UA Driver</p>
                                            <p className="text-sm text-indigo-700 mb-2">
                                                Connects to an OPC UA server and reads selected nodes at the configured scan rate.
                                            </p>
                                            <ul className="list-disc list-inside text-xs text-indigo-600 space-y-1">
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

            <div className="rounded-md border bg-white">
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
                        {gateways.length === 0 ? (
                            <TableRow>
                                <TableCell colSpan={7} className="h-24 text-center">
                                    No gateways found. {selectedAreaId ? 'Create one for the selected area.' : 'Select an area to filter or add a new gateway.'}
                                </TableCell>
                            </TableRow>
                        ) : (
                            gateways.map((gw) => (
                                <TableRow
                                    key={gw.id}
                                    className={`cursor-pointer hover:bg-slate-50 ${!gw.enabled ? 'opacity-50' : ''}`}
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
                                            ? `Rack: ${gw.connection_config?.rack || 0}, Slot: ${gw.connection_config?.slot || 0}`
                                            : gw.driver_type === 'MQTT'
                                                ? 'PLC → Broker (event-driven)'
                                                : gw.driver_type === 'OPC_UA'
                                                    ? `${gw.connection_config?.endpoint || 'No endpoint'}`
                                                    : `Port: ${gw.connection_config?.port || 502}, ID: ${gw.connection_config?.slave_id || 1}`
                                        }
                                        <br />
                                        {gw.driver_type !== 'MQTT' && <>{gw.driver_type === 'OPC_UA' ? `Scan: ${gw.scan_rate_ms}ms` : `Scan: ${gw.scan_rate_ms}ms`}</>}
                                    </TableCell>
                                    <TableCell>
                                        <div className="flex items-center gap-2">
                                            <div className={`h-2.5 w-2.5 rounded-full ${gw.connection_status === 'online' ? 'bg-green-500' : 'bg-red-500'}`} />
                                            <span className="text-sm capitalize">{gw.connection_status || 'Unknown'}</span>
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
                                                <Button
                                                    variant="ghost"
                                                    size="icon"
                                                    className="h-8 w-8 text-red-500 hover:text-red-600 hover:bg-red-50"
                                                    onClick={(e) => handleDelete(e, gw.id)}
                                                >
                                                    <Trash2 size={16} />
                                                </Button>
                                                <ChevronRight size={16} className="text-slate-300" />
                                            </div>
                                        )}
                                        {testResult?.id === gw.id && (
                                            <div className={`text-xs mt-1 ${testResult.success ? 'text-green-600' : 'text-red-600'}`}>
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
        </div >
    );
};

export default GatewaysPage;
