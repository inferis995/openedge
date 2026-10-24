import { useState } from 'react';
import { Link } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { ArrowRight, CheckCircle2, Circle, CircleDashed, Hourglass, ListChecks } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { showApiError } from '@/lib/api-error-handler';
import { setupProgress, type SetupStep } from '@/hooks/useForecastSetup';

const ICON = {
    done: <CheckCircle2 className="size-5 text-emerald-600 dark:text-emerald-400" />,
    todo: <Circle className="size-5 text-amber-500" />,
    wait: <Hourglass className="size-5 text-sky-500" />,
    optional: <CircleDashed className="size-5 text-muted-foreground" />,
};

/** What forecasts already have to work with, and what to set up next, each with a way to do it. */
export function SetupGuide({ steps, onTab }: { steps: SetupStep[]; onTab: (tab: string) => void }) {
    const { t } = useTranslation();
    const [busy, setBusy] = useState<string | null>(null);
    if (steps.length === 0) return null;
    const { done, total } = setupProgress(steps);
    return (
        <section className="rounded-lg border bg-card">
            <div className="flex flex-col gap-3 border-b p-4 sm:flex-row sm:items-center sm:justify-between">
                <div className="flex items-start gap-3">
                    <ListChecks className="mt-0.5 size-5 shrink-0 text-muted-foreground" />
                    <div>
                        <h2 className="text-sm font-semibold">{t('forecast.setup_title')}</h2>
                        <p className="text-xs text-muted-foreground">{t('forecast.setup_intro')}</p>
                    </div>
                </div>
                <div className="flex items-center gap-3 sm:w-56">
                    <div className="h-2 flex-1 overflow-hidden rounded-full bg-muted">
                        <div className="h-full rounded-full bg-emerald-500 transition-all" style={{ width: `${total ? done / total * 100 : 0}%` }} />
                    </div>
                    <span className="shrink-0 text-xs tabular-nums text-muted-foreground">{t('forecast.setup_progress', { done, total })}</span>
                </div>
            </div>
            <ol className="divide-y">
                {steps.map(s => (
                    <li key={s.key} className="flex flex-col gap-3 p-4 sm:flex-row sm:items-center">
                        <div className="flex min-w-0 flex-1 items-start gap-3">
                            <span className="mt-0.5 shrink-0" title={t(`forecast.setup_state_${s.state}`)}>{ICON[s.state]}</span>
                            <div className="min-w-0">
                                <div className="flex flex-wrap items-center gap-x-2 text-sm font-medium">
                                    {s.title}
                                    {s.state === 'optional' && <span className="text-xs font-normal text-muted-foreground">{t('forecast.setup_state_optional')}</span>}
                                </div>
                                <p className="text-sm text-muted-foreground">{s.detail}</p>
                            </div>
                        </div>
                        {s.action && (
                            <div className="shrink-0 pl-8 sm:pl-0">
                                {s.action.to ? (
                                    <Button asChild variant="outline" size="sm" className="gap-1.5">
                                        <Link to={s.action.to}>{s.action.label}<ArrowRight className="size-3.5" /></Link>
                                    </Button>
                                ) : (
                                    <Button variant={s.state === 'todo' ? 'default' : 'outline'} size="sm" className="gap-1.5" disabled={busy === s.key}
                                        onClick={async () => {
                                            if (s.action?.tab) { onTab(s.action.tab); return; }
                                            setBusy(s.key);
                                            try { await s.action?.run?.(); } catch (e) { showApiError(e, t('forecast.failed')); } finally { setBusy(null); }
                                        }}>
                                        {s.action.label}{s.action.tab && <ArrowRight className="size-3.5" />}
                                    </Button>
                                )}
                            </div>
                        )}
                    </li>
                ))}
            </ol>
        </section>
    );
}
