import { useState, useEffect } from 'react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Switch } from '@/components/ui/switch';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Trash2, Plus } from 'lucide-react';
import { tagsApi } from '@/api/tags';
import { toast } from 'sonner';

export interface AlarmDefinition {
    id?: number;
    tag_id: number;
    alarm_type: string;
    threshold: number | null;
    deadband: number;
    delay_seconds: number;
    severity: string;
    message: string;
    enabled: boolean;
}

interface Props {
    tagId: number;
    dataType: string;
}

export function TagAlarmsTab({ tagId, dataType }: Props) {
    const [alarms, setAlarms] = useState<AlarmDefinition[]>([]);
    const [isLoading, setIsLoading] = useState(true);
    const [isSaving, setIsSaving] = useState(false);

    useEffect(() => {
        const fetchAlarms = async () => {
            if (!tagId) return;
            try {
                const data = await tagsApi.getTagAlarms(tagId);
                setAlarms(data || []);
            } catch (err) {
                console.error("Failed to fetch alarms", err);
            } finally {
                setIsLoading(false);
            }
        };
        fetchAlarms();
    }, [tagId]);

    const handleSave = async () => {
        setIsSaving(true);
        try {
            await tagsApi.saveTagAlarms(tagId, alarms);
            toast.success("Allarmi salvati con successo!");
        } catch (err) {
            console.error("Failed to save alarms", err);
            toast.error("Errore nel salvataggio degli allarmi.");
        } finally {
            setIsSaving(false);
        }
    };

    const addAlarm = () => {
        const defType = dataType === 'BOOL' ? 'bool_true' : 'high';
        setAlarms([...alarms, {
            tag_id: tagId,
            alarm_type: defType,
            threshold: dataType === 'BOOL' ? null : 100,
            deadband: 0,
            delay_seconds: 5,
            severity: 'warning',
            message: `Allarme ${defType}`,
            enabled: true
        }]);
    };

    const removeAlarm = (index: number) => {
        setAlarms(alarms.filter((_, i) => i !== index));
    };

    const updateAlarm = (index: number, field: keyof AlarmDefinition, value: any) => {
        const newAlarms = [...alarms];
        newAlarms[index] = { ...newAlarms[index], [field]: value };
        setAlarms(newAlarms);
    };

    if (isLoading) return <div className="p-4 text-center text-sm text-muted-foreground">Caricamento allarmi...</div>;

    return (
        <div className="space-y-4 py-4">
            <div className="flex justify-between items-center">
                <p className="text-sm text-muted-foreground">Configura le regole di allarme per questo tag.</p>
                <Button onClick={addAlarm} size="sm" variant="outline" className="gap-2">
                    <Plus size={16} /> Nuovo Allarme
                </Button>
            </div>

            {alarms.length === 0 ? (
                <div className="text-center p-8 border border-dashed rounded-md bg-muted/20 text-muted-foreground">
                    Nessun allarme configurato.
                </div>
            ) : (
                <div className="space-y-4 max-h-[400px] overflow-y-auto pr-2">
                    {alarms.map((alarm, idx) => (
                        <div key={idx} className="p-4 border rounded-md shadow-sm space-y-4 bg-card relative">
                            <Button
                                variant="ghost"
                                size="icon"
                                className="absolute top-2 right-2 h-6 w-6 text-red-500 hover:bg-red-50"
                                onClick={() => removeAlarm(idx)}
                            >
                                <Trash2 size={14} />
                            </Button>

                            <div className="grid grid-cols-2 gap-4">
                                <div className="space-y-1">
                                    <Label>Condizione</Label>
                                    <Select
                                        value={alarm.alarm_type}
                                        onValueChange={(v) => updateAlarm(idx, 'alarm_type', v)}
                                    >
                                        <SelectTrigger>
                                            <SelectValue />
                                        </SelectTrigger>
                                        <SelectContent>
                                            {dataType === 'BOOL' ? (
                                                <>
                                                    <SelectItem value="bool_true">Se diventa Vero</SelectItem>
                                                    <SelectItem value="bool_false">Se diventa Falso</SelectItem>
                                                </>
                                            ) : (
                                                <>
                                                    <SelectItem value="high">Alto ( \u003e Soglia )</SelectItem>
                                                    <SelectItem value="low">Basso ( \u003c Soglia )</SelectItem>
                                                    <SelectItem value="high_high">Molto Alto (Critico)</SelectItem>
                                                    <SelectItem value="low_low">Molto Basso (Critico)</SelectItem>
                                                </>
                                            )}
                                        </SelectContent>
                                    </Select>
                                </div>

                                {dataType !== 'BOOL' && (
                                    <div className="space-y-1">
                                        <Label>Valore Soglia</Label>
                                        <Input
                                            type="number"
                                            value={alarm.threshold || 0}
                                            onChange={(e) => updateAlarm(idx, 'threshold', parseFloat(e.target.value))}
                                        />
                                    </div>
                                )}

                                <div className="space-y-1">
                                    <Label>Ritardo (Secondi)</Label>
                                    <Input
                                        type="number"
                                        min="0"
                                        value={alarm.delay_seconds}
                                        onChange={(e) => updateAlarm(idx, 'delay_seconds', parseInt(e.target.value))}
                                    />
                                    <p className="text-[10px] text-muted-foreground">Deve resistere per {alarm.delay_seconds}s</p>
                                </div>

                                {dataType !== 'BOOL' && (
                                    <div className="space-y-1">
                                        <Label>Deadband (Isteresi)</Label>
                                        <Input
                                            type="number"
                                            min="0"
                                            value={alarm.deadband}
                                            onChange={(e) => updateAlarm(idx, 'deadband', parseFloat(e.target.value))}
                                        />
                                    </div>
                                )}

                                <div className="space-y-1">
                                    <Label>Gravità (Severity)</Label>
                                    <Select
                                        value={alarm.severity}
                                        onValueChange={(v) => updateAlarm(idx, 'severity', v)}
                                    >
                                        <SelectTrigger>
                                            <SelectValue />
                                        </SelectTrigger>
                                        <SelectContent>
                                            <SelectItem value="info">Info</SelectItem>
                                            <SelectItem value="warning">Warning / Allarme</SelectItem>
                                            <SelectItem value="critical">Critico / Urgente</SelectItem>
                                        </SelectContent>
                                    </Select>
                                </div>

                                <div className="space-y-1 col-span-2">
                                    <Label>Messaggio di allarme</Label>
                                    <Input
                                        value={alarm.message}
                                        placeholder="es. Allarme: Pompa in blocco"
                                        onChange={(e) => updateAlarm(idx, 'message', e.target.value)}
                                    />
                                </div>

                                <div className="space-y-1 col-span-2 flex items-center justify-between pt-2 border-t mt-2">
                                    <div className="flex items-center gap-2">
                                        <Switch
                                            checked={alarm.enabled}
                                            onCheckedChange={(v) => updateAlarm(idx, 'enabled', v)}
                                        />
                                        <Label>Abilita Allarme</Label>
                                    </div>
                                </div>

                            </div>
                        </div>
                    ))}
                </div>
            )}

            <div className="flex justify-end pt-4 border-t">
                <Button onClick={handleSave} disabled={isSaving}>
                    {isSaving ? 'Salvataggio...' : 'Salva Allarmi'}
                </Button>
            </div>
        </div>
    );
}
