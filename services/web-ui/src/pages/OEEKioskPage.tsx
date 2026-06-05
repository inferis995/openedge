import { useQuery } from '@tanstack/react-query';
import { useEffect, useState } from 'react';
import { Gauge, ArrowLeft } from 'lucide-react';
import { Link } from 'react-router-dom';

import { dashboardApi, OEEProfileSnapshot, OEESnapshot } from '@/api/dashboard';

// OEEKioskPage — modalità TV/kiosk per monitor 1080p appeso in reparto.
//
// Caratteristiche:
//   - No sidebar / no header / fullscreen
//   - Rollup gigante a sinistra + rotazione profili a destra ogni 10s
//   - Refresh dati ogni 30s (dashboard overview)
//   - High contrast, font enormi, leggibile a distanza
//   - Modalità giorno/notte automatica (basata su orario)
//   - Piccolo link "back" in alto a destra per uscire (admin)

const REFRESH_MS = 30_000;
const ROTATE_MS  = 10_000;

const oeeColor = (v: number): string => {
    if (v >= 85) return 'text-emerald-400';
    if (v >= 65) return 'text-amber-400';
    if (v >= 40) return 'text-orange-400';
    return 'text-red-400';
};

const oeeBg = (v: number): string => {
    if (v >= 85) return 'bg-emerald-500/10 border-emerald-500/40';
    if (v >= 65) return 'bg-amber-500/10 border-amber-500/40';
    if (v >= 40) return 'bg-orange-500/10 border-orange-500/40';
    return 'bg-red-500/10 border-red-500/40';
};

const oeeBand = (v: number): string => {
    if (v >= 85) return 'WORLD-CLASS';
    if (v >= 65) return 'TIPICO';
    if (v >= 40) return 'DA MIGLIORARE';
    return 'CRITICO';
};

// Determina se siamo in "notte" (theme scuro forzato).
const isNightTime = (): boolean => {
    const h = new Date().getHours();
    return h >= 20 || h < 7;
};

const OEEKioskPage = () => {
    const [profileIdx, setProfileIdx] = useState(0);
    const [night, setNight] = useState(isNightTime());

    const { data } = useQuery({
        queryKey: ['kiosk-dashboard'],
        queryFn: dashboardApi.overview,
        refetchInterval: REFRESH_MS,
        placeholderData: (prev) => prev,
    });

    // Rotazione profili: change index ogni ROTATE_MS.
    useEffect(() => {
        const profiles = data?.oee?.profiles ?? [];
        if (profiles.length <= 1) return;
        const id = setInterval(() => {
            setProfileIdx((i) => (i + 1) % profiles.length);
        }, ROTATE_MS);
        return () => clearInterval(id);
    }, [data?.oee?.profiles?.length]);

    // Night/day mode: ricalcola ogni minuto.
    useEffect(() => {
        const id = setInterval(() => setNight(isNightTime()), 60_000);
        return () => clearInterval(id);
    }, []);

    if (!data || !data.oee) {
        return (
            <div className="min-h-screen flex items-center justify-center bg-black text-white text-2xl">
                Caricamento OEE…
            </div>
        );
    }

    const overview = data.oee;
    const profiles = overview.profiles ?? [];
    const rollup = overview.rollup ?? overview.legacy;
    const visibleProfile = profiles.length > 0 ? profiles[profileIdx % profiles.length] : null;

    return (
        <div
            className={`min-h-screen w-full font-sans transition-colors ${
                night ? 'bg-black text-slate-100' : 'bg-slate-50 text-slate-900'
            }`}
        >
            <div className="h-screen flex flex-col p-8">
                {/* Header — small */}
                <div className="flex items-center justify-between mb-6">
                    <div className="flex items-center gap-3">
                        <Gauge size={32} className={night ? 'text-emerald-400' : 'text-emerald-600'} />
                        <span className="text-2xl font-bold tracking-tight">OEE LIVE</span>
                        <span className={`text-sm ${night ? 'text-slate-400' : 'text-slate-500'}`}>
                            {new Date(data.generated_at).toLocaleString('it-IT')}
                        </span>
                    </div>
                    <Link
                        to="/"
                        className={`flex items-center gap-2 text-xs px-3 py-1.5 rounded-md transition-colors ${
                            night ? 'hover:bg-slate-800' : 'hover:bg-slate-200'
                        }`}
                    >
                        <ArrowLeft size={14} /> Esci
                    </Link>
                </div>

                {/* Main grid: 2 columns */}
                <div className="flex-1 grid grid-cols-1 lg:grid-cols-12 gap-8 min-h-0">
                    {/* Rollup gigante a sinistra */}
                    {rollup && (
                        <KioskRollup snapshot={rollup} label={overview.mode === 'profiles' ? 'OVERALL' : 'OEE'} night={night} />
                    )}

                    {/* Profili a rotazione a destra */}
                    {visibleProfile ? (
                        <KioskProfile
                            profile={visibleProfile}
                            night={night}
                            idx={profileIdx % profiles.length}
                            total={profiles.length}
                        />
                    ) : (
                        <div className="lg:col-span-7 flex items-center justify-center text-3xl opacity-50">
                            Nessun profilo configurato
                        </div>
                    )}
                </div>

                {/* Footer: badge profili in coda */}
                {profiles.length > 1 && (
                    <div className="mt-4 flex items-center justify-center gap-2 flex-wrap">
                        {profiles.map((p, i) => (
                            <div
                                key={p.profile_id}
                                className={`px-3 py-1.5 rounded text-sm border transition-all ${
                                    i === profileIdx % profiles.length
                                        ? oeeBg(p.snapshot.oee)
                                        : night
                                            ? 'border-slate-800 text-slate-500'
                                            : 'border-slate-300 text-slate-500'
                                }`}
                            >
                                <span className="font-bold">{p.name}</span>
                                <span className={`ml-2 font-mono ${oeeColor(p.snapshot.oee)}`}>
                                    {p.snapshot.oee.toFixed(0)}%
                                </span>
                            </div>
                        ))}
                    </div>
                )}
            </div>
        </div>
    );
};

const KioskRollup = ({
    snapshot, label, night,
}: { snapshot: OEESnapshot; label: string; night: boolean }) => {
    const color = oeeColor(snapshot.oee);
    const bg = oeeBg(snapshot.oee);
    return (
        <div className={`lg:col-span-5 rounded-3xl border-4 p-12 flex flex-col justify-center ${bg}`}>
            <div className="text-2xl font-bold tracking-widest opacity-70 mb-2">{label}</div>
            <div className={`flex items-baseline gap-3 ${color}`}>
                <span className="text-[12rem] leading-none font-black tracking-tighter">
                    {snapshot.oee.toFixed(0)}
                </span>
                <span className="text-6xl font-bold opacity-70">%</span>
            </div>
            <div className={`text-3xl font-bold tracking-widest mt-4 ${color}`}>
                {oeeBand(snapshot.oee)}
            </div>

            {/* Mini A/P/Q in bottom */}
            <div className="grid grid-cols-3 gap-4 mt-12">
                {[
                    { label: 'A', value: snapshot.availability },
                    { label: 'P', value: snapshot.performance },
                    { label: 'Q', value: snapshot.quality },
                ].map((m) => (
                    <div key={m.label} className="text-center">
                        <div className={`text-xs uppercase tracking-widest ${night ? 'text-slate-400' : 'text-slate-500'}`}>
                            {m.label === 'A' ? 'Availability' : m.label === 'P' ? 'Performance' : 'Quality'}
                        </div>
                        <div className={`text-5xl font-bold ${oeeColor(m.value)}`}>
                            {m.value.toFixed(0)}<span className="text-2xl opacity-70">%</span>
                        </div>
                    </div>
                ))}
            </div>
        </div>
    );
};

const KioskProfile = ({
    profile, night, idx, total,
}: { profile: OEEProfileSnapshot; night: boolean; idx: number; total: number }) => {
    const color = oeeColor(profile.snapshot.oee);
    const bg = oeeBg(profile.snapshot.oee);
    return (
        <div className={`lg:col-span-7 rounded-3xl border-4 p-10 flex flex-col ${bg}`}>
            <div className="flex items-baseline justify-between mb-2">
                <div>
                    <div className="text-2xl font-bold tracking-wide opacity-70">PROFILO</div>
                    <div className={`text-5xl font-black tracking-tight ${night ? 'text-white' : 'text-slate-900'}`}>
                        {profile.name}
                    </div>
                    {profile.area_name && (
                        <div className={`text-xl mt-1 ${night ? 'text-slate-400' : 'text-slate-500'}`}>
                            {profile.area_name}
                        </div>
                    )}
                </div>
                <div className={`text-sm opacity-50`}>{idx + 1} / {total}</div>
            </div>

            <div className={`flex items-baseline gap-3 mt-4 ${color}`}>
                <span className="text-[10rem] leading-none font-black tracking-tighter">
                    {profile.snapshot.oee.toFixed(0)}
                </span>
                <span className="text-5xl font-bold opacity-70">%</span>
            </div>

            {/* A/P/Q a barre orizzontali */}
            <div className="space-y-6 mt-10">
                {[
                    { label: 'AVAILABILITY', value: profile.snapshot.availability },
                    { label: 'PERFORMANCE',  value: profile.snapshot.performance },
                    { label: 'QUALITY',      value: profile.snapshot.quality },
                ].map((m) => (
                    <div key={m.label}>
                        <div className="flex items-baseline justify-between mb-1">
                            <span className={`text-lg font-bold tracking-widest ${night ? 'text-slate-400' : 'text-slate-600'}`}>
                                {m.label}
                            </span>
                            <span className={`text-3xl font-bold ${oeeColor(m.value)}`}>
                                {m.value.toFixed(0)}%
                            </span>
                        </div>
                        <div className={`w-full h-4 rounded ${night ? 'bg-slate-800' : 'bg-slate-200'} overflow-hidden`}>
                            <div
                                className={`h-4 rounded ${
                                    m.value >= 85 ? 'bg-emerald-500'
                                    : m.value >= 65 ? 'bg-amber-500'
                                    : m.value >= 40 ? 'bg-orange-500'
                                    : 'bg-red-500'
                                }`}
                                style={{ width: `${Math.min(100, Math.max(0, m.value))}%` }}
                            />
                        </div>
                    </div>
                ))}
            </div>
        </div>
    );
};

export default OEEKioskPage;
