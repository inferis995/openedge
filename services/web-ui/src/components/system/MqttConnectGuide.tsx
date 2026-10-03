import { useTranslation } from 'react-i18next';
import { Link } from 'react-router-dom';
import { BookOpen, Radio } from 'lucide-react';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';

interface Props {
    brokerMode: 'internal' | 'external';
    externalHost: string;
    externalPort: number;
}

const Row = ({ label, children }: { label: string; children: React.ReactNode }) => (
    <div className="grid grid-cols-1 sm:grid-cols-[180px_1fr] gap-1 sm:gap-3 py-2 border-b border-border last:border-0">
        <span className="text-xs font-medium text-muted-foreground">{label}</span>
        <div className="text-sm min-w-0">{children}</div>
    </div>
);

/**
 * How another system — a SCADA, Node-RED, a MES — reads OpenEdge's data.
 *
 * The broker settings said which broker the drivers publish to, and nothing
 * about what they publish there. Someone connecting an external client had to
 * find the topic layout in the source.
 */
export default function MqttConnectGuide({ brokerMode, externalHost, externalPort }: Props) {
    const { t } = useTranslation();
    const host = brokerMode === 'external' && externalHost ? externalHost : window.location.hostname;
    const port = brokerMode === 'external' ? externalPort : 1883;

    return (
        <Card className="border-border shadow-xs bg-card">
            <CardHeader className="pb-3 border-b border-border">
                <div className="flex items-center gap-3">
                    <div className="w-9 h-9 clip-hex bg-primary/10 border border-primary/20 flex items-center justify-center shrink-0">
                        <BookOpen className="h-4 w-4 text-primary" />
                    </div>
                    <div>
                        <CardTitle className="text-base">{t('mqttSettings.guide_title')}</CardTitle>
                        <CardDescription className="text-xs mt-0.5">{t('mqttSettings.guide_desc')}</CardDescription>
                    </div>
                </div>
            </CardHeader>
            <CardContent className="pt-2">
                <Row label={t('mqttSettings.guide_broker')}>
                    <code className="font-mono">{host}:{port}</code>
                    <span className="text-xs text-muted-foreground ml-2">
                        {brokerMode === 'internal' ? t('mqttSettings.guide_broker_internal') : t('mqttSettings.guide_broker_external')}
                    </span>
                </Row>
                <Row label={t('mqttSettings.guide_values')}>
                    <code className="font-mono text-xs break-all">data/&#123;{t('mqttSettings.seg_org')}&#125;/&#123;{t('mqttSettings.seg_site')}&#125;/&#123;{t('mqttSettings.seg_area')}&#125;/&#123;{t('mqttSettings.seg_gateway')}&#125;/&#123;{t('mqttSettings.seg_tag')}&#125;</code>
                    <p className="text-xs text-muted-foreground mt-1">{t('mqttSettings.guide_values_names')}</p>
                    <p className="text-xs text-muted-foreground mt-1">
                        {t('mqttSettings.guide_example')} <code className="font-mono">data/acme/stabilimento-milano/linea-1/plc-forno/temperatura-zona-1</code>
                    </p>
                </Row>
                <Row label={t('mqttSettings.guide_payload')}>
                    <code className="font-mono text-xs">{'{"tag_id": 42, "org_id": 1, "v": 71.3, "ts": 1790000000000, "q": 0}'}</code>
                    <p className="text-xs text-muted-foreground mt-1">{t('mqttSettings.guide_payload_desc')}</p>
                </Row>
                <Row label={t('mqttSettings.guide_sparkplug')}>
                    <code className="font-mono text-xs">spBv1.0/&#123;group&#125;/DDATA/&#123;edge node&#125;/&#123;device&#125;</code>
                    <p className="text-xs text-muted-foreground mt-1">{t('mqttSettings.guide_sparkplug_desc')}</p>
                </Row>
                <Row label={t('mqttSettings.guide_health')}>
                    <code className="font-mono text-xs">sys/health/&#123;org id&#125;/&#123;gateway id&#125;</code>
                    <p className="text-xs text-muted-foreground mt-1">{t('mqttSettings.guide_health_desc')}</p>
                </Row>
                <Row label={t('mqttSettings.guide_access')}>
                    <p className="text-xs text-muted-foreground">{t('mqttSettings.guide_access_desc')}</p>
                </Row>
                <div className="pt-3">
                    <Link to="/mqtt-monitor" className="inline-flex items-center gap-1.5 text-sm text-primary hover:underline">
                        <Radio className="h-3.5 w-3.5" /> {t('mqttSettings.guide_monitor')}
                    </Link>
                </div>
            </CardContent>
        </Card>
    );
}
