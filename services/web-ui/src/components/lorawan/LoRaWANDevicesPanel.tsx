import { useState, useCallback } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { lorawanApi, LoRaWANDevice, ImportField } from '@/api/lorawan';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Switch } from '@/components/ui/switch';
import { Badge } from '@/components/ui/badge';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from '@/components/ui/dialog';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { Radio, Download, Send, RefreshCw, CheckCircle2, Signal, Clock, ChevronDown } from 'lucide-react';
import { formatDistanceToNow } from 'date-fns';
import { it } from 'date-fns/locale';

interface Props {
  gatewayId: number;
  gatewayName: string;
  open: boolean;
  onClose: () => void;
}

interface FieldImportState {
  selected: boolean;
  alias: string;
  data_type: 'REAL' | 'INT' | 'BOOL' | 'STRING';
  historize: boolean;
}

const guessType = (value: string, fieldName: string): 'REAL' | 'INT' | 'BOOL' | 'STRING' => {
  const lc = fieldName.toLowerCase();
  if (lc === 'rssi' || lc === 'snr' || lc === 'f_port') return 'REAL';
  if (lc.includes('bool') || lc.includes('digital') || lc.includes('alarm') || lc.includes('status')) return 'BOOL';
  if (lc.includes('count') || lc.includes('pulse') || lc.includes('index')) return 'INT';
  const num = parseFloat(value);
  if (!isNaN(num)) return Number.isInteger(num) && !lc.includes('temp') && !lc.includes('press') && !lc.includes('hum') ? 'INT' : 'REAL';
  return 'STRING';
};

export function LoRaWANDevicesPanel({ gatewayId, gatewayName, open, onClose }: Props) {
  const qc = useQueryClient();
  const [importState, setImportState] = useState<Record<string, Record<string, FieldImportState>>>({});
  const [downlinkDevice, setDownlinkDevice] = useState<LoRaWANDevice | null>(null);
  const [dlPayload, setDlPayload] = useState('');
  const [dlFPort, setDlFPort] = useState(1);
  const [dlConfirmed, setDlConfirmed] = useState(false);
  const [importResult, setImportResult] = useState<string | null>(null);
  const [expanded, setExpanded] = useState<Record<string, boolean>>({});

  const { data: devices = [], isLoading, refetch } = useQuery({
    queryKey: ['lorawan-devices', gatewayId],
    queryFn: () => lorawanApi.listDevices(gatewayId),
    enabled: open,
    refetchInterval: open ? 15000 : false,
  });

  const initDeviceState = useCallback((dev: LoRaWANDevice) => {
    if (importState[dev.device_id]) return;
    const fields: Record<string, FieldImportState> = {};
    for (const [name, value] of Object.entries(dev.available_fields)) {
      const alreadyImported = dev.existing_tags.includes(`${dev.device_id}/${name}`);
      fields[name] = {
        selected: !alreadyImported,
        alias: `${dev.device_id} ${name}`,
        data_type: guessType(value, name),
        historize: true,
      };
    }
    setImportState(prev => ({ ...prev, [dev.device_id]: fields }));
  }, [importState]);

  const toggleField = (devId: string, field: string, checked: boolean) => {
    setImportState(prev => ({
      ...prev,
      [devId]: { ...prev[devId], [field]: { ...prev[devId][field], selected: checked } },
    }));
  };

  const updateField = (devId: string, field: string, key: keyof FieldImportState, value: unknown) => {
    setImportState(prev => ({
      ...prev,
      [devId]: { ...prev[devId], [field]: { ...prev[devId][field], [key]: value } },
    }));
  };

  const importMutation = useMutation({
    mutationFn: () => {
      const devicesPayload = devices
        .filter(d => importState[d.device_id])
        .map(d => ({
          device_id: d.device_id,
          fields: Object.entries(importState[d.device_id] || {})
            .filter(([, s]) => s.selected)
            .map(([name, s]): ImportField => ({
              name,
              alias: s.alias,
              data_type: s.data_type,
              historize: s.historize,
            })),
        }))
        .filter(d => d.fields.length > 0);
      return lorawanApi.importTags(gatewayId, devicesPayload);
    },
    onSuccess: (res) => {
      setImportResult(res.message);
      qc.invalidateQueries({ queryKey: ['tags'] });
      qc.invalidateQueries({ queryKey: ['lorawan-devices', gatewayId] });
      refetch();
    },
  });

  const downlinkMutation = useMutation({
    mutationFn: () => lorawanApi.sendDownlink(gatewayId, {
      device_id: downlinkDevice!.device_id,
      dev_eui: downlinkDevice!.dev_eui,
      f_port: dlFPort,
      payload_hex: dlPayload,
      confirmed: dlConfirmed,
    }),
    onSuccess: () => {
      setDownlinkDevice(null);
      setDlPayload('');
    },
  });

  const totalSelected = Object.values(importState).reduce((sum, fields) =>
    sum + Object.values(fields).filter(f => f.selected).length, 0);

  const signalColor = (rssi: number | null) => {
    if (rssi === null) return 'text-muted-foreground';
    if (rssi > -85) return 'text-emerald-500';
    if (rssi > -100) return 'text-yellow-500';
    return 'text-red-500';
  };

  return (
    <>
      <Dialog open={open} onOpenChange={v => !v && onClose()}>
        <DialogContent className="max-w-3xl max-h-[85vh] flex flex-col gap-0 p-0">
          <DialogHeader className="px-6 pt-6 pb-4 border-b">
            <DialogTitle className="flex items-center gap-2">
              <Radio size={18} className="text-violet-500" />
              Dispositivi LoRaWAN — {gatewayName}
            </DialogTitle>
            <p className="text-xs text-muted-foreground mt-1">
              I device vengono scoperti automaticamente quando inviano il primo uplink. Seleziona i campi da importare come tag OpenEdge.
            </p>
          </DialogHeader>

          <div className="flex-1 overflow-y-auto px-6 py-4">
            {isLoading ? (
              <div className="flex items-center justify-center py-16 text-muted-foreground">
                <RefreshCw size={20} className="animate-spin mr-2" />
                Caricamento dispositivi...
              </div>
            ) : devices.length === 0 ? (
              <div className="text-center py-16 space-y-3">
                <Radio size={40} className="mx-auto text-muted-foreground opacity-30" />
                <p className="text-muted-foreground">Nessun dispositivo rilevato ancora.</p>
                <p className="text-xs text-muted-foreground">Il driver raccoglie automaticamente i device non appena inviano il primo uplink al network server.</p>
              </div>
            ) : (
              <div className="space-y-2">
                {devices.map(dev => {
                  if (!importState[dev.device_id]) initDeviceState(dev);
                  const devState = importState[dev.device_id] || {};
                  const selectedCount = Object.values(devState).filter(f => f.selected).length;
                  const alreadyImported = dev.existing_tags.length;
                  const isExpanded = expanded[dev.device_id] ?? true;

                  return (
                    <div key={dev.device_id} className="border rounded-lg bg-card overflow-hidden">
                      {/* Header row */}
                      <button
                        className="w-full px-4 py-3 hover:bg-muted/30 text-left transition-colors"
                        onClick={() => setExpanded(p => ({ ...p, [dev.device_id]: !isExpanded }))}>
                        <div className="flex items-center gap-3">
                          <ChevronDown size={14} className={`shrink-0 transition-transform text-muted-foreground ${isExpanded ? '' : '-rotate-90'}`} />
                          <div className="flex-1 min-w-0">
                            <div className="flex items-center gap-2">
                              <span className="font-mono text-sm font-semibold truncate">{dev.device_id}</span>
                              {dev.dev_eui && (
                                <span className="text-[10px] text-muted-foreground font-mono uppercase">{dev.dev_eui}</span>
                              )}
                            </div>
                            <div className="flex items-center gap-3 mt-0.5 text-xs text-muted-foreground">
                              <span className="flex items-center gap-1">
                                <Clock size={10} />
                                {formatDistanceToNow(new Date(dev.last_seen), { addSuffix: true, locale: it })}
                              </span>
                              <span className={`flex items-center gap-1 ${signalColor(dev.last_rssi)}`}>
                                <Signal size={10} />
                                {dev.last_rssi !== null ? `${dev.last_rssi} dBm` : '—'}
                              </span>
                              <span>{dev.uplink_count} uplink</span>
                            </div>
                          </div>
                          <div className="flex items-center gap-2 shrink-0">
                            {alreadyImported > 0 && (
                              <Badge variant="secondary" className="text-[10px]">
                                <CheckCircle2 size={9} className="mr-1 text-emerald-500" />
                                {alreadyImported} importati
                              </Badge>
                            )}
                            {selectedCount > 0 && (
                              <Badge className="text-[10px] bg-violet-600">{selectedCount} selezionati</Badge>
                            )}
                            <Badge variant="outline" className="text-[10px]">
                              {Object.keys(dev.available_fields).length} campi
                            </Badge>
                          </div>
                        </div>
                      </button>

                      {/* Expanded content */}
                      {isExpanded && (
                        <div className="px-4 pb-4 border-t">
                          {/* Fields table */}
                          <div className="space-y-1.5 mt-3 mb-3">
                            <div className="grid grid-cols-[auto_1fr_120px_100px_auto] gap-2 text-[10px] uppercase tracking-widest text-muted-foreground px-1 pb-1 border-b">
                              <span className="w-5" />
                              <span>Campo / Alias</span>
                              <span>Tipo</span>
                              <span>Ultimo valore</span>
                              <span>Hist.</span>
                            </div>
                            {Object.entries(dev.available_fields).map(([field, exampleVal]) => {
                              const fs = devState[field];
                              if (!fs) return null;
                              const alreadyTag = dev.existing_tags.includes(`${dev.device_id}/${field}`);

                              return (
                                <div key={field}
                                  className={`grid grid-cols-[auto_1fr_120px_100px_auto] gap-2 items-center rounded px-1 py-1.5 ${alreadyTag ? 'opacity-50' : 'hover:bg-muted/30'}`}>
                                  <input
                                    type="checkbox"
                                    checked={fs.selected}
                                    disabled={alreadyTag}
                                    onChange={e => toggleField(dev.device_id, field, e.target.checked)}
                                    className="w-4 h-4 accent-violet-600"
                                  />
                                  <div className="min-w-0">
                                    <div className="text-xs font-mono text-muted-foreground">{dev.device_id}/{field}</div>
                                    <Input
                                      value={fs.alias}
                                      onChange={e => updateField(dev.device_id, field, 'alias', e.target.value)}
                                      className="h-6 text-xs mt-0.5"
                                      disabled={!fs.selected || alreadyTag}
                                    />
                                  </div>
                                  <Select
                                    value={fs.data_type}
                                    onValueChange={v => updateField(dev.device_id, field, 'data_type', v as 'REAL')}
                                    disabled={!fs.selected || alreadyTag}>
                                    <SelectTrigger className="h-7 text-xs"><SelectValue /></SelectTrigger>
                                    <SelectContent>
                                      <SelectItem value="REAL">REAL</SelectItem>
                                      <SelectItem value="INT">INT</SelectItem>
                                      <SelectItem value="BOOL">BOOL</SelectItem>
                                      <SelectItem value="STRING">STRING</SelectItem>
                                    </SelectContent>
                                  </Select>
                                  <span className="text-xs font-mono truncate text-muted-foreground">
                                    {alreadyTag ? <span className="text-emerald-500">✓ importato</span> : exampleVal}
                                  </span>
                                  <Switch
                                    checked={fs.historize}
                                    onCheckedChange={v => updateField(dev.device_id, field, 'historize', v)}
                                    disabled={!fs.selected || alreadyTag}
                                    className="scale-75"
                                  />
                                </div>
                              );
                            })}
                          </div>

                          {/* Downlink button */}
                          <div className="flex justify-between items-center mt-3 pt-3 border-t">
                            <div className="text-xs text-muted-foreground">
                              SNR: {dev.last_snr !== null ? `${dev.last_snr} dB` : '—'} · FPort: {dev.last_f_port ?? '—'}
                            </div>
                            <Button
                              variant="outline"
                              size="sm"
                              className="h-7 text-xs gap-1"
                              onClick={() => setDownlinkDevice(dev)}>
                              <Send size={11} />
                              Invia Downlink
                            </Button>
                          </div>
                        </div>
                      )}
                    </div>
                  );
                })}
              </div>
            )}
          </div>

          {importResult && (
            <div className="mx-6 mb-2 p-2 rounded bg-emerald-50 dark:bg-emerald-950/30 border border-emerald-200 dark:border-emerald-800 text-xs text-emerald-700 dark:text-emerald-300 flex items-center gap-2">
              <CheckCircle2 size={14} />
              {importResult}
            </div>
          )}

          <DialogFooter className="px-6 py-4 border-t gap-2">
            <Button variant="ghost" size="sm" onClick={() => refetch()} className="gap-1 mr-auto">
              <RefreshCw size={13} />
              Aggiorna
            </Button>
            <Button variant="outline" onClick={onClose}>Chiudi</Button>
            <Button
              onClick={() => { setImportResult(null); importMutation.mutate(); }}
              disabled={totalSelected === 0 || importMutation.isPending}
              className="gap-1 bg-violet-600 hover:bg-violet-700 text-white">
              {importMutation.isPending ? <RefreshCw size={13} className="animate-spin" /> : <Download size={13} />}
              Importa {totalSelected > 0 ? `${totalSelected} tag` : 'tag selezionati'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Downlink dialog */}
      <Dialog open={!!downlinkDevice} onOpenChange={v => !v && setDownlinkDevice(null)}>
        <DialogContent className="max-w-sm">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <Send size={16} className="text-violet-500" />
              Downlink → {downlinkDevice?.device_id}
            </DialogTitle>
          </DialogHeader>
          <div className="space-y-4">
            <div className="space-y-1">
              <Label className="text-xs">Payload (HEX)</Label>
              <Input
                value={dlPayload}
                onChange={e => setDlPayload(e.target.value.replace(/[^0-9a-fA-F\s]/g, ''))}
                placeholder="01 02 FF (senza 0x)"
                className="font-mono text-sm"
              />
              <p className="text-[10px] text-muted-foreground">Byte in formato esadecimale, es: <code>0101</code> = 2 byte</p>
            </div>
            <div className="grid grid-cols-2 gap-3">
              <div className="space-y-1">
                <Label className="text-xs">FPort</Label>
                <Input
                  type="number"
                  min={1}
                  max={223}
                  value={dlFPort}
                  onChange={e => setDlFPort(parseInt(e.target.value) || 1)}
                />
              </div>
              <div className="space-y-1">
                <Label className="text-xs">Confirmed</Label>
                <div className="flex items-center gap-2 h-9">
                  <Switch checked={dlConfirmed} onCheckedChange={setDlConfirmed} />
                  <span className="text-xs text-muted-foreground">{dlConfirmed ? 'ACK richiesto' : 'Unconfirmed'}</span>
                </div>
              </div>
            </div>
            <div className="p-2 rounded bg-amber-50 dark:bg-amber-950/30 border border-amber-200 dark:border-amber-800 text-xs text-amber-700 dark:text-amber-300">
              Il downlink viene accodato sul network server (TTN/ChirpStack) e consegnato al device al prossimo uplink.
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDownlinkDevice(null)}>Annulla</Button>
            <Button
              disabled={!dlPayload.trim() || downlinkMutation.isPending}
              onClick={() => downlinkMutation.mutate()}
              className="gap-1 bg-violet-600 hover:bg-violet-700 text-white">
              {downlinkMutation.isPending ? <RefreshCw size={13} className="animate-spin" /> : <Send size={13} />}
              Invia
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
